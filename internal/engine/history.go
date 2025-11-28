package engine

import (
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"time"

	"gorm.io/gorm"
)

// Alpha determines how much weight the LATEST test has (0.0 - 1.0).
// 0.3 means the new test changes the score by 20% immediately.
const HistoryAlpha = 0.2

// FailurePenalty is the multiplier applied when a proxy fails completely.
// 0.5 means we cut the score in HALF every time it fails.
const FailurePenalty = 0.6

type HistoryEngine struct {
	db *gorm.DB
}

func NewHistoryEngine(db *gorm.DB) *HistoryEngine {
	return &HistoryEngine{db: db}
}

func (h *HistoryEngine) GetPredictiveScore(proxyID uint, currentUserISP string) float64 {
	var perf model.ProxyPerformance
	h.db.Where("proxy_id = ? AND user_isp = ?", proxyID, currentUserISP).Limit(1).Find(&perf)

	if perf.ProxyID != 0 {
		return perf.Score
	}

	// Cold Start: Use average of other ISPs, or a default low score
	var result struct {
		AvgScore float64
	}
	err := h.db.Model(&model.ProxyPerformance{}).
		Select("AVG(score) as avg_score").
		Where("proxy_id = ? AND user_isp != ?", proxyID, currentUserISP).
		Scan(&result).Error

	if err == nil && result.AvgScore > 0 {
		return result.AvgScore * 0.8 // Conservative guess
	}
	return 0.2 // Default start for completely unknown proxies
}

// UpdateHistory uses Exponential Moving Average (EMA) to favor recent data.
func (h *HistoryEngine) UpdateHistory(proxyID uint, userISP string, rawSpeed float64, baselineSpeed float64) float64 {
	// 1. Normalize Speed
	currentNormalized := 0.0
	if baselineSpeed > 0 && rawSpeed > 0 {
		currentNormalized = rawSpeed / baselineSpeed
	}

	var perf model.ProxyPerformance
	err := h.db.Where("proxy_id = ? AND user_isp = ?", proxyID, userISP).Limit(1).Find(&perf).Error

	if err != nil || perf.ProxyID == 0 {
		// New Record
		perf = model.ProxyPerformance{
			ProxyID:     proxyID,
			UserISP:     userISP,
			Score:       currentNormalized, // First score is just the result
			SampleCount: 1,
			LastTested:  time.Now(),
		}
	} else {
		// Existing Record: Update Logic
		if currentNormalized > 0 {
			// SUCCESS: Apply Exponential Moving Average
			// Keeps history but adapts quickly to degradation
			perf.Score = (perf.Score * (1 - HistoryAlpha)) + (currentNormalized * HistoryAlpha)
		} else {
			// FAILURE: Apply Hash Penalty
			// If it fails, we don't care how fast it WAS. It's useless NOW.
			perf.Score = perf.Score * FailurePenalty
		}
		
		perf.SampleCount++
		perf.LastTested = time.Now()
	}

	// Save
	if err := h.db.Save(&perf).Error; err != nil {
		logger.Log.Errorf("Failed to update history for Proxy %d: %v", proxyID, err)
	}
	return perf.Score
}
