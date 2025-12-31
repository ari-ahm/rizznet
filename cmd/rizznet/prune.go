package main

import (
	"strconv"

	"rizznet/internal/config"
	"rizznet/internal/db"
	"rizznet/internal/engine"
	"rizznet/internal/logger"

	"github.com/spf13/cobra"
)

var noEnvDetect bool

var pruneCmd = &cobra.Command{
	Use:   "prune [limit]",
	Short: "Shrink the database to a specific size",
	Long: `Removes the lowest-scoring proxies until the total count matches the target limit.
If no limit is provided, the 'max_proxies' value from config.yaml is used.

The pruning logic calculates a score for every proxy based on the current environment's 
ISP history and category weights, then deletes the worst performers.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Load Config
		cfg, err := config.Load(cfgFile)
		if err != nil {
			logger.Log.Fatalf("Error loading config: %v", err)
		}

		// 2. Parse Argument (Optional Override)
		targetLimit := 0
		if len(args) > 0 {
			val, err := strconv.Atoi(args[0])
			if err != nil {
				logger.Log.Fatalf("Invalid limit argument: %v", err)
			}
			targetLimit = val
			logger.Log.Infof("🎯 Pruning target manually set to: %d", targetLimit)
		}

		// 3. Connect DB
		database, err := db.Connect(cfg.Database.Path)
		if err != nil {
			logger.Log.Fatalf("Error connecting to DB: %v", err)
		}
		defer db.Close(database)

		// 4. Run Pruner
		if err := engine.PruneDatabase(database, cfg, targetLimit, noEnvDetect); err != nil {
			logger.Log.Errorf("Pruning failed: %v", err)
		} else {
			logger.Log.Info("✅ Database maintenance complete.")
		}
	},
}

func init() {
	pruneCmd.Flags().BoolVar(&noEnvDetect, "no-env", false, "Disable environment detection entirely (assumes 'Unknown' ISP)")
	rootCmd.AddCommand(pruneCmd)
}
