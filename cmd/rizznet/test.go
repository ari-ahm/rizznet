package main

import (
	"fmt"
	"os"
	"sync"

	_ "rizznet/internal/categories/strategies"

	"rizznet/internal/config"
	"rizznet/internal/db"
	"rizznet/internal/engine"
	"rizznet/internal/environment"
	"rizznet/internal/geoip"
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/tester"
	"rizznet/internal/xray"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	flagWorkers         int
	flagBudget          int
	flagFast            bool
	flagOnlyCategorized bool
	flagTopK            int
)

var testCmd = &cobra.Command{
	Use:   "test [category_names...]",
	Short: "Optimize proxies using Simulated Annealing",
	Long:  `Run the optimization engine. Use --fast to skip the initial health check. Use --top-k or --only-categorized to filter selection.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			logger.Log.Fatalf("Error loading config: %v", err)
		}

		if err := geoip.Init(cfg.Tester.GeoIPASNPath, cfg.Tester.GeoIPCountryPath); err != nil {
			logger.Log.Fatalf("Failed to init GeoIP: %v", err)
		}
		defer geoip.Close()

		if flagWorkers > 0 {
			cfg.Tester.WorkerCount = flagWorkers
		}
		if flagBudget > 0 {
			cfg.Tester.AnnealBudgetMB = flagBudget
		}

		if len(args) > 0 {
			cfg.FilterCategories(args)
		}

		database, err := db.Connect(cfg.Database.Path)
		if err != nil {
			logger.Log.Fatalf("Error connecting to DB: %v", err)
		}
		defer db.Close(database)
		db.Migrate(database)

		env, err := environment.Detect(cfg.Tester, true)
		if err != nil {
			logger.Log.Errorf("Environment check failed. Using fallbacks: %v", err)
			env = &environment.Env{
				ISP:           "ip-api error, not detected(i sure hope this isn't a real isp)",
				BaselineSpeed: 1.,
			}
		}

		// --- 1. Candidate Selection Logic ---
		logger.Log.Info("🔍 Selecting candidates for testing...")
		query := database.Model(&model.Proxy{})

		if flagOnlyCategorized {
			query = query.Where("id IN (?)", database.Table("proxy_categories").Select("proxy_id"))
			logger.Log.Info("   -> Filter: Only proxies already in a category")
		}

		if flagTopK > 0 {
			query = query.Select("proxies.*").
				Joins("LEFT JOIN proxy_performances pp ON pp.proxy_id = proxies.id AND pp.user_isp = ?", env.ISP).
				Order("COALESCE(pp.score, 0) DESC").
				Limit(flagTopK)
			logger.Log.Infof("   -> Filter: Top %d proxies by historical score", flagTopK)
		}

		var candidates []model.Proxy
		if err := query.Find(&candidates).Error; err != nil {
			logger.Log.Fatalf("Failed to fetch candidates: %v", err)
		}

		if len(candidates) == 0 {
			logger.Log.Error("❌ No candidates found matching criteria. Exiting.")
			return
		}
		logger.Log.Infof("   -> Target Count: %d", len(candidates))

		// --- 2. Health Check Layer ---
		historyEngine := engine.NewHistoryEngine(database)

		var survivors []model.Proxy
		if flagFast {
			logger.Log.Info("⏩ Fast mode enabled: Skipping global health check.")
			survivors = candidates
		} else {
			survivors = runHealthCheckLayer(database, historyEngine, cfg.Tester, env, candidates)
		}

		if len(survivors) == 0 {
			logger.Log.Error("❌ No survivors after health check. Exiting.")
			return
		}

		// --- 3. Annealing ---
		annealer, err := engine.NewAnnealer(database, *cfg, env, survivors, flagFast)
		if err != nil {
			logger.Log.Fatalf("Failed to init annealer: %v", err)
		}

		annealer.Run(cfg.Tester.AnnealBudgetMB)

		logger.Log.Info("✅ Testing and optimization complete.")
	},
}

func runHealthCheckLayer(
	database *gorm.DB,
	hist *engine.HistoryEngine,
	testCfg config.TesterConfig,
	env *environment.Env,
	inputProxies []model.Proxy,
) []model.Proxy {
	batchSize := testCfg.WorkerCount
	if batchSize <= 0 {
		batchSize = 20
	}

	totalCount := len(inputProxies)
	if totalCount == 0 {
		return []model.Proxy{}
	}

	logger.Log.Infof("🔎 Running Health Check on %d proxies...", totalCount)

	poolPorts, err := xray.GetFreePorts(batchSize)
	if err != nil {
		logger.Log.Fatalf("Failed to allocate port pool: %v", err)
	}
	logger.Log.Debugf("Allocated port pool: %v", poolPorts)

	bar := progressbar.NewOptions(totalCount,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(false),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetDescription("[cyan]Checking...[reset]"),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	var survivors []model.Proxy
	var survivorsLock sync.Mutex

	t := tester.New(testCfg)

	for i := 0; i < totalCount; i += batchSize {
		end := i + batchSize
		if end > totalCount {
			end = totalCount
		}
		batch := inputProxies[i:end]

		var links []string
		for _, p := range batch {
			links = append(links, p.Raw)
		}

		currentPorts := poolPorts[:len(batch)]

		portMap, instance, err := xray.StartOnPorts(links, currentPorts)
		if err != nil {
			logger.Log.Warnf("Batch failed Xray start: %v", err)
			bar.Add(len(batch))
			continue
		}

		var wg sync.WaitGroup

		for _, p := range batch {
			port, ok := portMap[p.Raw]
			if !ok {
				bar.Add(1)
				continue
			}

			wg.Add(1)
			go func(proxy model.Proxy, localPort int) {
				defer wg.Done()
				defer bar.Add(1)

				analyzeClient := t.MakeClient(localPort, testCfg.HealthTimeout)
				res, err := t.Analyze(analyzeClient)

				if err != nil {
					hist.UpdateHistory(proxy.ID, env.ISP, 0.0, env.BaselineSpeed)
				} else {
					proxy.IP = res.IP
					proxy.ISP = res.ISP
					proxy.Country = res.Country
					proxy.IsDirty = res.IsDirty

					if !proxy.IsRotating && proxy.ISP != "" && (proxy.ISP != res.ISP || proxy.Country != res.Country) {
						proxy.IsRotating = true
					}

					if proxy.EntryISP == "" && proxy.Address != "" {
						if inMeta, err := t.ResolveHost(proxy.Address); err == nil {
							proxy.EntryIP = inMeta.IP
							proxy.EntryISP = inMeta.ISP
							proxy.EntryCountry = inMeta.Country
						}
					}

					database.Clauses(clause.OnConflict{
						Columns:   []clause.Column{{Name: "id"}},
						DoUpdates: clause.AssignmentColumns([]string{
							"ip", "isp", "country", "is_dirty", "is_rotating",
							"entry_ip", "entry_isp", "entry_country",
						}),
					}).Create(&proxy)

					survivorsLock.Lock()
					survivors = append(survivors, proxy)
					bar.Describe(fmt.Sprintf("[cyan]Alive: %d[reset]", len(survivors)))
					survivorsLock.Unlock()
				}
			}(p, port)
		}
		wg.Wait()
		instance.Close()
	}

	bar.Finish()
	fmt.Print("\n")
	logger.Log.Infof("✅ Health Check Complete. Survivors: %d/%d", len(survivors), totalCount)
	return survivors
}

func init() {
	testCmd.Flags().IntVar(&flagWorkers, "workers", 0, "Override worker count")
	testCmd.Flags().IntVar(&flagBudget, "budget", 0, "Override data budget (MB)")
	testCmd.Flags().BoolVar(&flagFast, "fast", false, "Skip initial health check")

	testCmd.Flags().BoolVar(&flagOnlyCategorized, "only-categorized", false, "Only test proxies that are already in a category")
	testCmd.Flags().IntVar(&flagTopK, "top-k", 0, "Only test the top K proxies based on historical score")

	rootCmd.AddCommand(testCmd)
}
