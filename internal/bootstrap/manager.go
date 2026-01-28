package bootstrap

import (
	"fmt"

	"rizznet/internal/config"
	"rizznet/internal/logger"
	"rizznet/internal/model"
	"rizznet/internal/tester"

	"github.com/xtls/xray-core/core"
	"gorm.io/gorm"
)

type Manager struct {
	db              *gorm.DB
	t               *tester.Tester
	category        string
	fallback        string
	healthURL       string
	currentInstance *core.Instance
}

// NewManager creates a bootstrap manager. 
// Note: We pass the full TesterConfig to create a Tester internally.
func NewManager(database *gorm.DB, cfg config.TesterConfig, category, fallback string) *Manager {
	return &Manager{
		db:        database,
		t:         tester.New(cfg),
		category:  category,
		fallback:  fallback,
		healthURL: cfg.EchoURL, // Default to EchoURL if not specified, or use a specific one
	}
}

func (m *Manager) GetProxy() (string, error) {
	var category model.Category

	targetCat := m.category
	if targetCat == "" {
		targetCat = "speed"
	}

	// 1. Fetch Candidates
	result := m.db.Preload("Proxies").Where("name = ?", targetCat).Limit(1).Find(&category)
	if result.Error != nil || result.RowsAffected == 0 || len(category.Proxies) == 0 {
		return m.fallback, nil
	}

	candidates := category.Proxies

	var rawLinks []string
	for _, p := range candidates {
		rawLinks = append(rawLinks, p.Raw)
	}

	// 2. Delegate Race to Tester
	// This uses the shared concurrent logic, retries, and timeouts.
	winnerPort, instance, err := m.t.FindFirstAlive(rawLinks, m.healthURL)
	if err != nil {
		logger.Log.Warnf("System Proxy: Bootstrap failed (%v). Using fallback.", err)
		return m.fallback, nil
	}

	// 3. Success
	m.currentInstance = instance
	logger.Log.Debugf("System Proxy: Bootstrap successful on port %d", winnerPort)
	return fmt.Sprintf("socks5://127.0.0.1:%d", winnerPort), nil
}

func (m *Manager) Stop() {
	if m.currentInstance != nil {
		m.currentInstance.Close()
	}
}