package xray

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"rizznet/internal/logger"
	"rizznet/internal/model"

	"github.com/xtls/xray-core/core"
	"gorm.io/gorm"
)

type Manager struct {
	db              *gorm.DB
	category        string
	fallback        string
	healthURL       string
	timeout         time.Duration
	retries         int
	currentInstance *core.Instance
	currentPort     int
}

func NewManager(database *gorm.DB, category string, fallback string, healthURL string, timeout time.Duration, retries int) *Manager {
	return &Manager{
		db:        database,
		category:  category,
		fallback:  fallback,
		healthURL: healthURL,
		timeout:   timeout,
		retries:   retries,
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

	// Limit candidate pool to avoid port exhaustion during bootstrap
	candidates := category.Proxies
	if len(candidates) > 20 {
		candidates = candidates[:20]
	}

	var rawLinks []string
	for _, p := range candidates {
		rawLinks = append(rawLinks, p.Raw)
	}

	// Start ALL candidates in one Xray instance
	portMap, instance, err := StartMultiEphemeral(rawLinks)
	if err != nil {
		logger.Log.Warnf("System Proxy: Failed to start batch: %v", err)
		return m.fallback, nil
	}

	// Buffered channel to capture the first success
	winChan := make(chan int, 1)
	doneChan := make(chan struct{})

	var wg sync.WaitGroup
	
	// Launch concurrent checks
	for _, link := range rawLinks {
		port, ok := portMap[link]
		if !ok {
			continue
		}

		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			
			// Stop checking if a winner is already found
			select {
			case <-doneChan:
				return
			default:
			}

			if m.checkConnection(p) {
				select {
				case winChan <- p:
					close(doneChan) // Signal others to stop
				default:
				}
			}
		}(port)
	}

	// Wait for all to finish OR one to succeed
	go func() {
		wg.Wait()
		close(winChan) // Close winChan if everyone finishes
	}()

	winnerPort, ok := <-winChan
	if ok {
		// Found a winner!
		m.currentInstance = instance
		m.currentPort = winnerPort
		logger.Log.Debugf("System Proxy: Found working proxy on port %d", winnerPort)
		return fmt.Sprintf("socks5://127.0.0.1:%d", winnerPort), nil
	}

	// No winner found
	instance.Close()
	logger.Log.Warn("System Proxy: No working proxies found in category. Using fallback.")
	return m.fallback, nil
}

func (m *Manager) Stop() {
	if m.currentInstance != nil {
		m.currentInstance.Close()
	}
}

func (m *Manager) checkConnection(port int) bool {
	// Retry Loop
	for i := 0; i <= m.retries; i++ {
		if m.doCheckConnection(port) {
			return true
		}
		if i < m.retries {
			time.Sleep(200 * time.Millisecond)
		}
	}
	return false
}

func (m *Manager) doCheckConnection(port int) bool {
	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout: m.timeout, // Use configured health timeout
			}).DialContext,
			ResponseHeaderTimeout: m.timeout, // Enforce TTFB limit
		},
		Timeout: m.timeout, // Use configured health timeout
	}

	// Context for stricter control
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", m.healthURL, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
