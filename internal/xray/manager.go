package xray

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"rizznet/internal/model"

	"github.com/xtls/xray-core/core"
	"gorm.io/gorm"
)

type Manager struct {
	db              *gorm.DB
	category        string
	fallback        string
	healthURL       string
	currentInstance *core.Instance
	currentPort     int
}

func NewManager(database *gorm.DB, category string, fallback string, healthURL string) *Manager {
	return &Manager{
		db:        database,
		category:  category,
		fallback:  fallback,
		healthURL: healthURL,
	}
}

func (m *Manager) GetProxy() (string, error) {
	var category model.Category

	targetCat := m.category
	if targetCat == "" {
		targetCat = "speed"
	}

	result := m.db.Preload("Proxies").Where("name = ?", targetCat).Limit(1).Find(&category)
	if result.Error != nil || result.RowsAffected == 0 || len(category.Proxies) == 0 {
		return m.fallback, nil
	}

	for _, proxy := range category.Proxies {
		port, instance, err := StartEphemeral(proxy.Raw)
		if err != nil {
			continue
		}

		if m.checkConnection(port) {
			if m.currentInstance != nil {
				m.currentInstance.Close()
			}
			m.currentInstance = instance
			m.currentPort = port
			return fmt.Sprintf("socks5://127.0.0.1:%d", port), nil
		}

		instance.Close()
	}

	return m.fallback, nil
}

func (m *Manager) Stop() {
	if m.currentInstance != nil {
		m.currentInstance.Close()
	}
}

func (m *Manager) checkConnection(port int) bool {
	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout: 3 * time.Second,
			}).DialContext,
		},
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get(m.healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
