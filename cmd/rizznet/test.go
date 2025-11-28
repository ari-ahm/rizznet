package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	_ "rizznet/internal/categories/strategies"

	"rizznet/internal/config"
	"rizznet/internal/db"
	"rizznet/internal/engine"
	"rizznet/internal/environment"
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/tester"
	"rizznet/internal/xray"

	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	flagWorkers int
	flagBudget  int
	flagFast    bool
)

var testCmd = &cobra.Command{
	Use:   "test [category_names...]",
	Short: "Optimize proxies using Simulated Annealing",
	Long:  `Run the optimization engine. Use --fast to skip the initial health check.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			logger.Log.Fatalf("Error loading config: %v", err)
		}

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
		db.Migrate(database)

		env, err := environment.Detect(cfg.Tester)
		if err != nil {
			logger.Log.Fatalf("Environment check failed: %v", err)
		}

		var candidates []model.Proxy
		historyEngine := engine.NewHistoryEngine(database)

		if flagFast {
			logger.Log.Info("⏩ Fast mode enabled: Skipping global health check.")
			database.Find(&candidates)
		} else {
			candidates = runHealthCheckLayer(database, historyEngine, cfg.Tester, env)
		}

		if len(candidates) == 0 {
			logger.Log.Error("❌ No candidates found. Exiting.")
			return
		}

		annealer, err := engine.NewAnnealer(database, *cfg, env, candidates, flagFast)
		if err != nil {
			logger.Log.Fatalf("Failed to init annealer: %v", err)
		}

		annealer.Run(cfg.Tester.AnnealBudgetMB)

		// --- AGGRESSIVE PRUNING ---
		// Only run this if we did a FULL test (not fast mode).
		// We cut the DB size to 70% of max to make room for fresh proxies next cycle.
		if !flagFast {
			aggressiveLimit := int(float64(cfg.Database.MaxProxies) * 0.7)
			logger.Log.Infof("🧹 Running Aggressive Post-Test Pruning (Target: %d)...", aggressiveLimit)
			
			if err := engine.PruneDatabase(database, cfg, aggressiveLimit); err != nil {
				logger.Log.Errorf("Aggressive pruning failed: %v", err)
			} else {
				logger.Log.Info("✨ Aggressive pruning complete.")
			}
		}
	},
}

func runHealthCheckLayer(
	database *gorm.DB,
	hist *engine.HistoryEngine,
	testCfg config.TesterConfig,
	env *environment.Env,
) []model.Proxy {
	batchSize := testCfg.WorkerCount
	if batchSize <= 0 {
		batchSize = 20
	}

	var allProxies []model.Proxy
	database.Find(&allProxies)
	totalCount := len(allProxies)
	if totalCount == 0 {
		return []model.Proxy{}
	}

	logger.Log.Infof("🔎 Running Batch Health Check (Batch Size: %d, Total: %d)...", batchSize, totalCount)

	var survivors []model.Proxy
	var survivorsLock sync.Mutex
	var processedCount int32

	directClient := &http.Client{
		Timeout: testCfg.HealthTimeout,
	}

	for i := 0; i < totalCount; i += batchSize {
		end := i + batchSize
		if end > totalCount {
			end = totalCount
		}
		batch := allProxies[i:end]

		var links []string
		for _, p := range batch {
			links = append(links, p.Raw)
		}

		portMap, instance, err := xray.StartMultiEphemeral(links)
		if err != nil {
			logger.Log.Warnf("Batch failed Xray start: %v. Skipping %d proxies.", err, len(batch))
			atomic.AddInt32(&processedCount, int32(len(batch)))
			continue
		}

		var wg sync.WaitGroup
		t := tester.New(testCfg)

		for _, p := range batch {
			port, ok := portMap[p.Raw]
			if !ok {
				atomic.AddInt32(&processedCount, 1)
				continue
			}

			wg.Add(1)
			go func(proxy model.Proxy, localPort int) {
				defer wg.Done()
				
				proxiedClient := t.MakeClient(localPort, testCfg.HealthTimeout)

				_, err := t.HealthCheck(proxiedClient)
				if err != nil {
					hist.UpdateHistory(proxy.ID, env.ISP, 0.0, env.BaselineSpeed)
				} else {
					if proxy.ISP == "" || proxy.Country == "" {
						if meta, err := t.MetadataCheck(proxiedClient); err == nil {
							proxy.IP = meta.IP
							proxy.ISP = meta.ISP
							proxy.Country = meta.Country
							proxy.IsDirty = meta.IsDirty
						}
					}

					if proxy.EntryISP == "" && proxy.Address != "" {
						if inMeta, err := t.FetchHostMetadata(directClient, proxy.Address); err == nil {
							proxy.EntryIP = inMeta.IP
							proxy.EntryISP = inMeta.ISP
							proxy.EntryCountry = inMeta.Country
						}
					}

					database.Clauses(clause.OnConflict{
						Columns:   []clause.Column{{Name: "id"}},
						DoUpdates: clause.AssignmentColumns([]string{
							"ip", "isp", "country", "is_dirty", 
							"entry_ip", "entry_isp", "entry_country",
						}),
					}).Create(&proxy)

					survivorsLock.Lock()
					survivors = append(survivors, proxy)
					survivorsLock.Unlock()
				}

				curr := atomic.AddInt32(&processedCount, 1)
				printHealthProgress(int(curr), totalCount, len(survivors))

			}(p, port)
		}
		wg.Wait()
		instance.Close()
	}
	fmt.Print("\n") 
	logger.Log.Infof("✅ Health Check Complete. Survivors: %d/%d", len(survivors), totalCount)
	return survivors
}

func printHealthProgress(curr, total, survivors int) {
	percent := (curr * 100) / total
	barLen := 20
	filled := (percent * barLen) / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)

	fmt.Printf("\r🔎 Health: [%s] %d%% | %d/%d Checked | Alive: %d    ",
		bar, percent, curr, total, survivors)
}

func init() {
	testCmd.Flags().IntVar(&flagWorkers, "workers", 0, "Override worker count")
	testCmd.Flags().IntVar(&flagBudget, "budget", 0, "Override data budget (MB)")
	testCmd.Flags().BoolVar(&flagFast, "fast", false, "Skip initial health check")
	rootCmd.AddCommand(testCmd)
}
