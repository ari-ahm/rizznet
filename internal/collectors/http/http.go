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

	// Determine Timeout & Retries (Injected from CMD)
	timeout := 120 * time.Second 
	if tVal, ok := config["_timeout"]; ok {
		if t, ok := tVal.(time.Duration); ok {
			timeout = t
		}
	}

	retries := 0
	if rVal, ok := config["_retries"]; ok {
		if r, ok := rVal.(int); ok {
			retries = r
		}
	}

	// 2. Setup Client
	client := &http.Client{
		Timeout: timeout,
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

	// 4. Fetch with Retries
	var resp *http.Response
	var err error

	for i := 0; i <= retries; i++ {
		logger.Log.Debugf("Fetching URL (Attempt %d/%d): %s", i+1, retries+1, targetURL)
		
		resp, err = client.Get(targetURL)
		if err == nil && resp.StatusCode == 200 {
			break
		}
		
		if err == nil {
			// Non-200, close body and set error
			resp.Body.Close()
			err = fmt.Errorf("status code %d", resp.StatusCode)
		}

		if i < retries {
			time.Sleep(1 * time.Second)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch url after retries: %w", err)
	}
	defer resp.Body.Close()

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
