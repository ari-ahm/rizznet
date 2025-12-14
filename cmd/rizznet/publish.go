package main

import (
	"strconv"

	"rizznet/internal/config"
	"rizznet/internal/db"
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/publishers"
	"rizznet/internal/xray"

	"github.com/spf13/cobra"
)

var publishParams map[string]string

var publishCmd = &cobra.Command{
	Use:   "publish [publisher_names...]",
	Short: "Publish populated categories",
	Long:  `Run all publishers or specific ones. Use --param to override publisher configuration.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			logger.Log.Fatalf("Error loading config: %v", err)
		}

		// 1. Filter Publishers based on args
		if len(args) > 0 {
			cfg.FilterPublishers(args)
		}

		if len(cfg.Publishers) == 0 {
			logger.Log.Warn("No publishers matched.")
			return
		}

		// 2. Apply CLI Params Overrides
		for i := range cfg.Publishers {
			if cfg.Publishers[i].Params == nil {
				cfg.Publishers[i].Params = make(map[string]interface{})
			}
			for k, v := range publishParams {
				if intVal, err := strconv.Atoi(v); err == nil {
					cfg.Publishers[i].Params[k] = intVal
				} else {
					cfg.Publishers[i].Params[k] = v
				}
			}
		}

		database, err := db.Connect(cfg.Database.Path)
		if err != nil {
			logger.Log.Fatalf("Error connecting to DB: %v", err)
		}

		var activeProxy string
		if cfg.SystemProxy.Enabled && !noProxy {
			logger.Log.Info("🛡️  Initializing internal proxy manager for publishing...")
			// Use EchoURL instead of HealthURL
			pm := xray.NewManager(database, cfg.SystemProxy.Category, cfg.SystemProxy.Fallback, cfg.Tester.EchoURL)

			proxyAddr, err := pm.GetProxy()
			if err != nil {
				logger.Log.Warnf("Failed to get proxy: %v", err)
			} else {
				activeProxy = proxyAddr
				defer pm.Stop()
			}
		}

		for _, pubCfg := range cfg.Publishers {
			logger.Log.Infof("📨 Running Publisher: %s (%s)...", pubCfg.Name, pubCfg.Type)

			plugin, err := publishers.Get(pubCfg.Type)
			if err != nil {
				logger.Log.Warnf("Plugin not found: %v", err)
				continue
			}

			if activeProxy != "" {
				pubCfg.Params["_proxy_url"] = activeProxy
			}

			var categoriesToPublish []model.Category

			if len(pubCfg.Categories) > 0 {
				database.Preload("Proxies").Where("name IN ?", pubCfg.Categories).Find(&categoriesToPublish)
			} else {
				database.Preload("Proxies").Find(&categoriesToPublish)
			}

			err = plugin.Publish(categoriesToPublish, pubCfg.Params)
			if err != nil {
				logger.Log.Errorf("Publish failed: %v", err)
			} else {
				logger.Log.Info("✅ Published successfully.")
			}
		}
	},
}

func init() {
	publishCmd.Flags().StringToStringVarP(&publishParams, "param", "p", nil, "Override publisher params (e.g. -p output=sub.txt)")
	rootCmd.AddCommand(publishCmd)
}
