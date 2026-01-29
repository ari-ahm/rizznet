package engine

import (
	"fmt"
	"math/rand"
	"os"
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
)

type Annealer struct {
	db         *gorm.DB
	history    *HistoryEngine
	tester     *tester.Tester
	testerCfg  config.TesterConfig
	env        *environment.Env
	categories []CategoryContext
	candidates []Candidate
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

func NewAnnealer(db *gorm.DB, cfg config.Config, env *environment.Env, aliveProxies []model.Proxy) (*Annealer, error) {
	hist := NewHistoryEngine(db)
	tst := tester.New(cfg.Tester)

	catContexts := setupCategoryContexts(cfg)

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
		db:         db,
		history:    hist,
		tester:     tst,
		testerCfg:  cfg.Tester,
		env:        env,
		categories: catContexts,
		candidates: candidates,
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
		progressbar.OptionSetWriter(os.Stderr),
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

		logger.Log.Debugf("Now testing : " + candidate.Proxy.Raw)
		shortLink := candidate.Proxy.Raw
		if len(shortLink) > 15 {
			shortLink = shortLink[:12] + "..."
		}
		bar.Describe(fmt.Sprintf("[yellow]Found: %d(%d/%d) | Testing: %s[reset]", survivorsCount, len(testedProxies), len(a.candidates), shortLink))

		port, instance, err := xray.StartEphemeral(candidate.Proxy.Raw)
		if err != nil {
			continue
		}

		// Perform Speed Test
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
	saveCategories(a.db, a.categories)
}

// RunFast handles the --fast logic: categorize based on Health Check survivors and History scores, without new speed tests.
func RunFast(db *gorm.DB, cfg config.Config, env *environment.Env, survivors []model.Proxy) {
	hist := NewHistoryEngine(db)
	catContexts := setupCategoryContexts(cfg)

	logger.Log.Info("⚡ Distributing survivors to categories based on historical performance...")

	count := 0
	for _, p := range survivors {
		// Get existing score from DB
		score := hist.GetPredictiveScore(p.ID, env.ISP)

		// If proxy has no score (0.2 default from cold start), it might still be categorized if strategy allows
		// Usually we want at least some track record, but cold start is fine for "clean" category.

		// Check against all categories
		for _, ctx := range catContexts {
			if ctx.Strategy.IsCandidate(p, ctx.Config.Params) {
				// Strategy might re-weight the score (e.g. boost low latency if metadata available)
				finalScore := ctx.Strategy.Score(score, p, ctx.Config.Params)
				

				shortLink := p.Raw
				if len(shortLink) > 15 {
					shortLink = shortLink[:12] + "..."
				}
				logger.Log.Debugf("Offering %s with score %f", shortLink, finalScore)

				// Offer to bucket
				if ctx.Bucket.Offer(p, finalScore) {
					count++
				}
			}
		}
	}

	saveCategories(db, catContexts)
}

// Helper to init buckets
func setupCategoryContexts(cfg config.Config) []CategoryContext {
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
	return catContexts
}

// Helper to save results to DB
func saveCategories(db *gorm.DB, contexts []CategoryContext) {
	logger.Log.Info("💾 Saving Categories to Database...")
	for _, ctx := range contexts {
		survivors := ctx.Bucket.GetProxies()
		var dbCat model.Category
		db.FirstOrCreate(&dbCat, model.Category{Name: ctx.Config.Name})
		db.Model(&dbCat).Association("Proxies").Replace(survivors)
		logger.Log.Infof("   -> %s: Saved %d proxies.", ctx.Config.Name, len(survivors))
	}
}
