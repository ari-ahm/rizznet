package engine

import (
	"fmt"
	"math/rand"
	"sort"

	"rizznet/internal/categories"
	"rizznet/internal/config"
	"rizznet/internal/environment"
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/tester"
	"rizznet/internal/xray"

	"github.com/schollz/progressbar/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Annealer struct {
	db                *gorm.DB
	history           *HistoryEngine
	tester            *tester.Tester
	testerCfg         config.TesterConfig
	env               *environment.Env
	categories        []CategoryContext
	candidates        []Candidate
	strictHealthCheck bool
}

type CategoryContext struct {
	Index    int
	Config   config.CategoryConfig
	Strategy categories.Strategy
	Bucket   *Bucket
}

type Candidate struct {
	Proxy              model.Proxy
	PredictedScore     float64
	GlobalPriority     float64
	MatchingCatIndices []int
}

func NewAnnealer(db *gorm.DB, cfg config.Config, env *environment.Env, aliveProxies []model.Proxy, strictHealthCheck bool) (*Annealer, error) {
	hist := NewHistoryEngine(db)
	tst := tester.New(cfg.Tester)

	var catContexts []CategoryContext
	for i, catCfg := range cfg.Categories {
		strat, err := categories.Get(catCfg.Strategy)
		if err != nil {
			logger.Log.Warnf("Skipping category %s: %v", catCfg.Name, err)
			continue
		}
		catContexts = append(catContexts, CategoryContext{
			Index:    i,
			Config:   catCfg,
			Strategy: strat,
			Bucket:   NewBucket(catCfg.BucketSize),
		})
	}

	var candidates []Candidate
	for _, p := range aliveProxies {
		priority := CalculateGlobalPriority(p, hist, env.ISP, cfg.Categories)

		var matchingIndices []int
		for i, ctx := range catContexts {
			if ctx.Strategy.IsCandidate(p, ctx.Config.Params) {
				matchingIndices = append(matchingIndices, i)
			}
		}

		if len(matchingIndices) > 0 {
			candidates = append(candidates, Candidate{
				Proxy:              p,
				PredictedScore:     hist.GetPredictiveScore(p.ID, env.ISP),
				GlobalPriority:     priority,
				MatchingCatIndices: matchingIndices,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].GlobalPriority > candidates[j].GlobalPriority
	})

	return &Annealer{
		db:                db,
		history:           hist,
		tester:            tst,
		testerCfg:         cfg.Tester,
		env:               env,
		categories:        catContexts,
		candidates:        candidates,
		strictHealthCheck: strictHealthCheck,
	}, nil
}

func (a *Annealer) Run(maxDataMB int) {
	logger.Log.Infof("🔥 Starting Annealing (Budget: %d MB, Candidates: %d)", maxDataMB, len(a.candidates))

	dataUsed := 0.0
	limit := float64(maxDataMB)
	testedProxies := make(map[uint]bool)
	survivorsCount := 0

	bar := progressbar.NewOptions(1000,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[yellow]Annealing...[reset]"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[red]=[reset]",
			SaucerHead:    "[red]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	for dataUsed < limit {
		if len(testedProxies) >= len(a.candidates) {
			break
		}

		temperature := 1.0 - (dataUsed / limit)
		rangeSize := int(float64(len(a.candidates)) * temperature)
		if rangeSize < 1 {
			rangeSize = 1
		} else if rangeSize > len(a.candidates) {
			rangeSize = len(a.candidates)
		}

		var candidate *Candidate
		attemptLimit := 20
		for k := 0; k < attemptLimit; k++ {
			idx := rand.Intn(rangeSize)
			c := &a.candidates[idx]
			if !testedProxies[c.Proxy.ID] {
				candidate = c
				break
			}
		}
		if candidate == nil {
			for i := 0; i < len(a.candidates); i++ {
				if !testedProxies[a.candidates[i].Proxy.ID] {
					candidate = &a.candidates[i]
					break
				}
			}
		}
		if candidate == nil {
			break
		}

		testedProxies[candidate.Proxy.ID] = true

		shortLink := candidate.Proxy.Raw
		if len(shortLink) > 15 {
			shortLink = shortLink[:12] + "..."
		}
		bar.Describe(fmt.Sprintf("[yellow]Found: %d | Testing: %s[reset]", survivorsCount, shortLink))

		port, instance, err := xray.StartEphemeral(candidate.Proxy.Raw)
		if err != nil {
			continue
		}

		if a.strictHealthCheck {
			analyzeClient := a.tester.MakeClient(port, a.testerCfg.HealthTimeout)
			res, err := a.tester.Analyze(analyzeClient)

			if err != nil {
				instance.Close()
				a.history.UpdateHistory(candidate.Proxy.ID, a.env.ISP, 0.0, a.env.BaselineSpeed)
				continue
			}

			isRotating := candidate.Proxy.IsRotating
			if !isRotating {
				if candidate.Proxy.ISP != "" && candidate.Proxy.Country != "" {
					if res.ISP != candidate.Proxy.ISP || res.Country != candidate.Proxy.Country {
						isRotating = true
						logger.Log.Debugf("🔄 Detected Rotation for Proxy %d", candidate.Proxy.ID)
					}
				}
			}

			candidate.Proxy.IP = res.IP
			candidate.Proxy.ISP = res.ISP
			candidate.Proxy.Country = res.Country
			candidate.Proxy.IsDirty = res.IsDirty
			candidate.Proxy.IsRotating = isRotating

			go func(p model.Proxy) {
				a.db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "id"}},
					DoUpdates: clause.AssignmentColumns([]string{"ip", "isp", "country", "is_dirty", "is_rotating"}),
				}).Create(&p)
			}(candidate.Proxy)
		}

		speedClient := a.tester.MakeClient(port, a.testerCfg.SpeedTimeout)
		mbps, bytesDownloaded, err := a.tester.SpeedCheck(speedClient)

		instance.Close()

		mbDownloaded := float64(bytesDownloaded) / (1024 * 1024)
		dataUsed += mbDownloaded

		if err != nil {
			dataUsed += 0.2
			mbps = 0
		}

		currentPercent := (dataUsed / limit) * 1000
		bar.Set(int(currentPercent))

		a.history.UpdateHistory(candidate.Proxy.ID, a.env.ISP, mbps, a.env.BaselineSpeed)

		if mbps > 0 {
			currentNormalized := 0.0
			if a.env.BaselineSpeed > 0 {
				currentNormalized = mbps / a.env.BaselineSpeed
			}
			for _, catIdx := range candidate.MatchingCatIndices {
				ctx := &a.categories[catIdx]
				if ctx.Strategy.IsCandidate(candidate.Proxy, ctx.Config.Params) {
					finalScore := ctx.Strategy.Score(currentNormalized, candidate.Proxy, ctx.Config.Params)
					added := ctx.Bucket.Offer(candidate.Proxy, finalScore)
					if added {
						survivorsCount++
					}
				}
			}
		}
	}

	bar.Finish()
	fmt.Print("\n")
	logger.Log.Info("🏁 Optimization Finished.")
	a.saveResults()
}

func (a *Annealer) saveResults() {
	logger.Log.Info("💾 Saving Categories to Database...")
	for _, ctx := range a.categories {
		survivors := ctx.Bucket.GetProxies()
		var dbCat model.Category
		a.db.FirstOrCreate(&dbCat, model.Category{Name: ctx.Config.Name})
		a.db.Model(&dbCat).Association("Proxies").Replace(survivors)
		logger.Log.Infof("   -> %s: Saved %d proxies.", ctx.Config.Name, len(survivors))
	}
}