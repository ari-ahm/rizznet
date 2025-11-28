package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"rizznet/internal/logger"
)

var cfgFile string
var noProxy bool
var verbose bool
var logFile string // New variable

var rootCmd = &cobra.Command{
	Use:   "rizznet",
	Short: "A Xray proxy scraping/testing/publishing pipeline",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Pass logFile to Init
		logger.Init(verbose, logFile)
	},
	PostRun: func(cmd *cobra.Command, args []string) {
		logger.Sync()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&noProxy, "no-proxy", false, "Disable system proxy manager")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	
	// New Flag
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Write logs to file instead of stdout (overwrites file)")
}
