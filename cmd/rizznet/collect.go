package main

import (
	"strconv"
	"time"

	"rizznet/internal/collectors"
	_ "rizznet/internal/collectors/http"
	_ "rizznet/internal/collectors/telegram"
	"rizznet/internal/config"
	"rizznet/internal/db"
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/xray"
	"rizznet/internal/xray/parser"

	"github.com/spf13/cobra"
	"gorm.io/gorm/clause"
)

var collectParams map[string]string

var collectCmd = &cobra.Command{
	Use:   "collect [collector_names...]",
	Short: "Run collectors to fetch proxies",
	Long:  `Run all collectors defined in config, or specify specific ones by name. Use --param to override configuration parameters.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			logger.Log.Fatalf("Error loading config: %v", err)
		}

		if len(args) > 0 {
			cfg.FilterCollectors(args)
		}

		if len(cfg.Collectors) == 0 {
			logger.Log.Warn("No collectors matched the provided names.")
			return
		}

		for i := range cfg.Collectors {
			if cfg.Collectors[i].Params == nil {
				cfg.Collectors[i].Params = make(map[string]interface{})
			}
			for k, v := range collectParams {
				if intVal, err := strconv.Atoi(v); err == nil {
					cfg.Collectors[i].Params[k] = intVal
				} else {
					cfg.Collectors[i].Params[k] = v
				}
			}
		}

		database, err := db.Connect(cfg.Database.Path)
		if err != nil {
			logger.Log.Fatalf("Error connecting to DB: %v", err)
		}
		db.Migrate(database)

		var activeProxy string
		if cfg.SystemProxy.Enabled && !noProxy {
			logger.Log.Info("🛡️  Initializing internal proxy manager...")
			// Use EchoURL instead of HealthURL
			pm := xray.NewManager(database, cfg.SystemProxy.Category, cfg.SystemProxy.Fallback, cfg.Tester.EchoURL)

			proxyAddr, err := pm.GetProxy()
			if err != nil {
				logger.Log.Warnf("Failed to get proxy: %v. Proceeding without one.", err)
			} else {
				activeProxy = proxyAddr
				logger.Log.Infof("🚀 Using Proxy: %s", proxyAddr)
				defer pm.Stop()
			}
		}

		for _, cCfg := range cfg.Collectors {
			logger.Log.Infof("🏃 Running collector: %s (%s)...", cCfg.Name, cCfg.Type)

			collector, err := collectors.Get(cCfg.Type)
			if err != nil {
				logger.Log.Warnf("Skipping: %v", err)
				continue
			}

			if activeProxy != "" {
				cCfg.Params["_proxy_url"] = activeProxy
			}

			rawLinks, err := collector.Collect(cCfg.Params)
			if err != nil {
				logger.Log.Errorf("Error running collector: %v", err)
				continue
			}

			savedCount := 0
			for _, raw := range rawLinks {
				profile, err := parser.Parse(raw)
				if err != nil {
					continue
				}

				hash := profile.CalculateHash()

				proxy := model.Proxy{
					Raw:       raw,
					Hash:      hash,
					Source:    cCfg.Name,
					CreatedAt: time.Now(),
					Address:   profile.Address,
					Port:      profile.Port,
				}

				result := database.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "hash"}},
					DoNothing: true,
				}).Create(&proxy)

				if result.RowsAffected > 0 {
					savedCount++
				}
			}
			logger.Log.Infof("✅ Collector %s finished. Saved %d new unique proxies.", cCfg.Name, savedCount)
		}
	},
}

func init() {
	collectCmd.Flags().StringToStringVarP(&collectParams, "param", "p", nil, "Override collector params")
	rootCmd.AddCommand(collectCmd)
}
