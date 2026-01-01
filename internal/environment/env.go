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

func Detect(cfg config.TesterConfig, performSpeedTest bool) (*Env, error) {
	logger.Log.Info("🌍 Detecting Environment...")

	t := tester.New(cfg)
	directClient := &http.Client{Timeout: cfg.HealthTimeout}

	meta, err := t.Analyze(directClient)
	if err != nil {
		return nil, fmt.Errorf("failed to detect ISP: %w", err)
	}
	logger.Log.Infof("   -> Current ISP: %s (%s) [IP: %s]", meta.ISP, meta.Country, meta.IP)

	speed := 0.0
	
	if performSpeedTest {
		logger.Log.Info("   -> Measuring Baseline Speed...") 
		directClientSpeed := &http.Client{Timeout: cfg.SpeedTimeout}

		s, _, err := t.SpeedCheck(directClientSpeed)
		if err != nil {
			logger.Log.Warn("   -> Baseline Speed Test Failed!")
			return nil, fmt.Errorf("failed to measure baseline speed: %w", err)
		}
		speed = s
		logger.Log.Infof("   -> Baseline Speed: %.2f Mbps", speed)
	} else {
		logger.Log.Info("   -> Skipped Baseline Speed Test.")
	}

	return &Env{
		ISP:           meta.ISP,
		BaselineSpeed: speed,
	}, nil
}
