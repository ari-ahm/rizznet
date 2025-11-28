package engine

import (
	"rizznet/internal/categories"
	"rizznet/internal/config"
	"rizznet/internal/model"
)

// CalculateGlobalPriority computes the potential value of a proxy based on
// its history and how many categories it satisfies.
func CalculateGlobalPriority(
	p model.Proxy,
	hist *HistoryEngine,
	envISP string,
	catConfigs []config.CategoryConfig,
) float64 {
	// 1. Get Predictive Speed Score (History)
	predScore := hist.GetPredictiveScore(p.ID, envISP)

	// 2. Calculate Weight based on Categories it fits into
	totalWeight := 0.0
	matchesAny := false

	for _, catCfg := range catConfigs {
		// We instantiate the strategy temporarily to check candidacy.
		strat, err := categories.Get(catCfg.Strategy)
		if err != nil {
			continue
		}

		if strat.IsCandidate(p, catCfg.Params) {
			totalWeight += float64(catCfg.Weight)
			matchesAny = true
		}
	}

	if !matchesAny {
		return 0 // Useless proxy if it fits no categories
	}

	// Priority = LikelySpeed * Importance
	return predScore * totalWeight
}
