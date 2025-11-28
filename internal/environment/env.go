package environment

import (
	"fmt"
	"net/http"
	"rizznet/internal/config"
	"rizznet/internal/logger"
	"rizznet/internal/tester"
)

type Env struct {
	ISP           string
	BaselineSpeed float64
}

func Detect(cfg config.TesterConfig) (*Env, error) {
	logger.Log.Info("🌍 Detecting Environment...")

	t := tester.New(cfg)

	directClientHealth := &http.Client{Timeout: cfg.HealthTimeout}

	meta, err := t.MetadataCheck(directClientHealth)
	if err != nil {
		return nil, fmt.Errorf("failed to detect ISP: %w", err)
	}
	logger.Log.Infof("   -> Current ISP: %s (%s)", meta.ISP, meta.Country)

	fmt.Print("   -> Measuring Baseline Speed... ") // Keep fmt for single line update if needed, or use Log
	directClientSpeed := &http.Client{Timeout: cfg.SpeedTimeout}

	speed, _, err := t.SpeedCheck(directClientSpeed)
	if err != nil {
		fmt.Println("Failed!")
		return nil, fmt.Errorf("failed to measure baseline speed: %w", err)
	}
	fmt.Printf("%.2f Mbps\n", speed)

	return &Env{
		ISP:           meta.ISP,
		BaselineSpeed: speed,
	}, nil
}
