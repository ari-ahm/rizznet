package categories

import (
	"fmt"
	"rizznet/internal/model"
)

// Strategy defines how a category evaluates proxies.
type Strategy interface {
	// Name returns the strategy identifier
	Name() string

	// IsCandidate checks purely static properties (Protocol, Regex, etc).
	// Returns true if this proxy is allowed to enter the Battle Royale.
	IsCandidate(proxy model.Proxy, config map[string]interface{}) bool

	// Score calculates the value used for the Min-Heap comparison.
	//
	// perfScore: The normalized history score (0.0 - 1.0+) from HistoryEngine.
	// metadata:  The result of the MetadataCheck (Latency, Country, etc).
	//
	// A strategy might boost the score if Latency is low, or penalize if IsDirty is true.
	Score(perfScore float64, proxy model.Proxy, config map[string]interface{}) float64
}

type Factory func() Strategy

var registry = make(map[string]Factory)

func Register(name string, factory Factory) {
	registry[name] = factory
}

func Get(name string) (Strategy, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("strategy '%s' not found", name)
	}
	return factory(), nil
}
