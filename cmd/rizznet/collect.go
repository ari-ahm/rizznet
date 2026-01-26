package main

import (
	"io"
	"os"
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
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var collectParams map[string]string
var flagStdin bool

var collectCmd = &cobra.Command{
	Use:   "collect [collector_names...]",
	Short: "Run collectors to fetch proxies",
	Long:  `Run all collectors defined in config, or specify specific ones by name. Use --param to override configuration parameters. Use --stdin to pipe links directly.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			logger.Log.Fatalf("Error loading config: %v", err)
		}

		database, err := db.Connect(cfg.Database.Path)
		if err != nil {
			logger.Log.Fatalf("Error connecting to DB: %v", err)
		}
		defer db.Close(database)
		db.Migrate(database)

		// --- STDIN FLOW ---
		if flagStdin {
			logger.Log.Info("📥 Reading proxies from Stdin...")
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				logger.Log.Fatalf("Failed to read from stdin: %v", err)
			}

			// ExtractLinks handles deduplication and base64 decoding automatically
			links := xray.ExtractLinks(string(data))
			if len(links) == 0 {
				logger.Log.Warn("No valid links found in stdin.")
				return
			}

			count := saveProxies(database, links, "stdin")
			logger.Log.Infof("✅ Stdin Import finished. Processed %d links.", count)
			return
		}

		// --- NORMAL COLLECTOR FLOW ---

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
			// INJECT: Speed Timeout for Collectors
			cfg.Collectors[i].Params["_timeout"] = cfg.Tester.SpeedTimeout

			for k, v := range collectParams {
				if intVal, err := strconv.Atoi(v); err == nil {
					cfg.Collectors[i].Params[k] = intVal
				} else {
					cfg.Collectors[i].Params[k] = v
				}
			}
		}

		var activeProxy string
		if cfg.SystemProxy.Enabled && !noProxy {
			logger.Log.Info("🛡️  Initializing internal proxy manager...")
			// Use EchoURL instead of HealthURL for proxy checks
			// INJECT: Health Timeout for Manager (Bootstrap) & Retries
			pm := xray.NewManager(database, cfg.SystemProxy.Category, cfg.SystemProxy.Fallback, cfg.Tester.EchoURL, cfg.Tester.HealthTimeout, cfg.Tester.Retries)

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

			count := saveProxies(database, rawLinks, cCfg.Name)
			logger.Log.Infof("✅ Collector %s finished. Processed %d links.", cCfg.Name, count)
		}
	},
}

// saveProxies parses raw links, hashes them, and performs a batch insert into the DB.
func saveProxies(db *gorm.DB, rawLinks []string, source string) int64 {
	var batch []model.Proxy

	for _, raw := range rawLinks {
		profile, err := parser.Parse(raw)
		if err != nil {
			continue
		}

		hash := profile.CalculateHash()

		batch = append(batch, model.Proxy{
			Raw:       raw,
			Hash:      hash,
			Source:    source,
			CreatedAt: time.Now(),
			Address:   profile.Address,
			Port:      profile.Port,
		})
	}

	if len(batch) == 0 {
		return 0
	}

	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "hash"}},
		DoNothing: true,
	}).CreateInBatches(batch, 500)

	return result.RowsAffected
}

func init() {
	collectCmd.Flags().StringToStringVarP(&collectParams, "param", "p", nil, "Override collector params")
	collectCmd.Flags().BoolVar(&flagStdin, "stdin", false, "Read proxies from standard input")
	rootCmd.AddCommand(collectCmd)
}
