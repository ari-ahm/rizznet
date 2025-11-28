package stdout

import (
	"fmt"
	"rizznet/internal/model"
	"rizznet/internal/publishers"
)

type Publisher struct{}

func (p *Publisher) Publish(categories []model.Category, config map[string]interface{}) error {
	payload, err := publishers.GenerateSubscriptionPayload(categories, config)
	if err != nil {
		return err
	}
	
	fmt.Println("========== PUBLISHED SUBSCRIPTION ==========")
	fmt.Println(payload)
	fmt.Println("============================================")
	return nil
}

func init() {
	publishers.Register("stdout", func() publishers.Publisher { return &Publisher{} })
}
