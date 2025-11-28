package publishers

import (
	"fmt"
	"rizznet/internal/model"
)

type Publisher interface {
	Publish(categories []model.Category, config map[string]interface{}) error
}

type Factory func() Publisher

var registry = make(map[string]Factory)

func Register(name string, factory Factory) {
	registry[name] = factory
}

func Get(name string) (Publisher, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("publisher plugin '%s' not found", name)
	}
	return factory(), nil
}
