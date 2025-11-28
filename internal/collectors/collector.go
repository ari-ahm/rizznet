package collectors

import "fmt"

type Collector interface {
	Collect(config map[string]interface{}) ([]string, error)
}

type Factory func() Collector

var registry = make(map[string]Factory)

func Register(name string, factory Factory) {
	registry[name] = factory
}

func Get(name string) (Collector, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("collector plugin '%s' not found", name)
	}
	return factory(), nil
}
