package strategies

import (
	"strings"

	"rizznet/internal/categories"
	"rizznet/internal/model"
)

type StandardStrategy struct{}

func (s *StandardStrategy) Name() string {
	return "standard"
}

// IsCandidate checks protocol, clean status, and Entry ISP constraints.
func (s *StandardStrategy) IsCandidate(proxy model.Proxy, config map[string]interface{}) bool {
	// 1. Protocol Filter
	if requiredProto, ok := config["protocol"].(string); ok && requiredProto != "" {
		if !strings.HasPrefix(proxy.Raw, requiredProto+"://") {
			return false
		}
	}

	// 2. Dirty Check Filter
	if requireClean, ok := config["require_clean"].(bool); ok && requireClean {
		if proxy.IsDirty {
			return false
		}
	}

	// 3. Entry ISP Filter (Inbound ISP)
	// Performs a case-insensitive substring match.
	if targetISP, ok := config["entry_isp"].(string); ok && targetISP != "" {
		if proxy.EntryISP == "" {
			return false
		}
		
		if !strings.Contains(strings.ToLower(proxy.EntryISP), strings.ToLower(targetISP)) {
			return false
		}
	}

	return true
}

func (s *StandardStrategy) Score(perfScore float64, proxy model.Proxy, config map[string]interface{}) float64 {
	return perfScore
}

func init() {
	categories.Register("standard", func() categories.Strategy { return &StandardStrategy{} })
}
