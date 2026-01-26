package http

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"rizznet/internal/collectors"
	"rizznet/internal/logger"
	"rizznet/internal/xray"
)

type URLCollector struct{}

func (c *URLCollector) Collect(config map[string]interface{}) ([]string, error) {
	// 1. Get URL
	urlVal, ok := config["url"]
	if !ok {
		return nil, fmt.Errorf("missing 'url' in collector config")
	}
	targetURL := urlVal.(string)

	// 2. Setup Client
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	// 3. Check for Internal Proxy Injection
	if proxyVal, ok := config["_proxy_url"]; ok {
		if proxyStr, ok := proxyVal.(string); ok && proxyStr != "" {
			pURL, err := url.Parse(proxyStr)
			if err == nil {
				client.Transport = &http.Transport{
					Proxy: http.ProxyURL(pURL),
				}
				logger.Log.Debugf("HTTP Collector using proxy: %s", proxyStr)
			}
		}
	}

	// 4. Fetch
	logger.Log.Debugf("Fetching URL: %s", targetURL)
	resp, err := client.Get(targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("non-200 status code: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	links := xray.ExtractLinks(string(bodyBytes))
	return links, nil
}

func init() {
	collectors.Register("http", func() collectors.Collector {
		return &URLCollector{}
	})
}
