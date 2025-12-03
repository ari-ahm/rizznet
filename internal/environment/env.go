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
	directClient := &http.Client{Timeout: cfg.HealthTimeout}

	meta, err := t.Analyze(directClient)
	if err != nil {
		return nil, fmt.Errorf("failed to detect ISP: %w", err)
	}
	logger.Log.Infof("   -> Current ISP: %s (%s) [IP: %s]", meta.ISP, meta.Country, meta.IP)

	fmt.Print("   -> Measuring Baseline Speed... ") 
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
