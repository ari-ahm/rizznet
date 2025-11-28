package tester

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"rizznet/internal/config"
	"rizznet/internal/xray"
)

type Tester struct {
	cfg config.TesterConfig
}

type MetadataResult struct {
	IP      string
	ISP     string
	Country string
	IsDirty bool
}

func New(cfg config.TesterConfig) *Tester {
	return &Tester{cfg: cfg}
}

func (t *Tester) HealthCheck(client *http.Client) (time.Duration, error) {
	start := time.Now()
	resp, err := client.Get(t.cfg.HealthURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return 0, fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}
	return time.Since(start), nil
}

func (t *Tester) HealthCheckFromLink(link string) (time.Duration, error) {
	port, instance, err := xray.StartEphemeral(link)
	if err != nil {
		return 0, err
	}
	defer instance.Close()
	return t.HealthCheck(t.MakeClient(port, t.cfg.HealthTimeout))
}

func (t *Tester) MetadataCheck(client *http.Client) (*MetadataResult, error) {
	resp, err := client.Get("http://ip-api.com/json")
	if err != nil {
		return nil, fmt.Errorf("metadata fetch failed: %w", err)
	}
	defer resp.Body.Close()

	var apiData struct {
		Query       string `json:"query"`
		ISP         string `json:"isp"`
		CountryCode string `json:"countryCode"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiData); err != nil {
		return nil, err
	}
	if apiData.Status != "success" {
		return nil, fmt.Errorf("ip-api returned error status")
	}

	res := &MetadataResult{
		IP:      apiData.Query,
		ISP:     apiData.ISP,
		Country: apiData.CountryCode,
	}

	dirtyResp, err := client.Get(t.cfg.DirtyCheckURL)
	if err == nil && dirtyResp.StatusCode == 200 {
		res.IsDirty = false
		dirtyResp.Body.Close()
	} else {
		res.IsDirty = true
	}
	return res, nil
}

func (t *Tester) MetadataCheckFromLink(link string) (*MetadataResult, error) {
	port, instance, err := xray.StartEphemeral(link)
	if err != nil {
		return nil, err
	}
	defer instance.Close()
	return t.MetadataCheck(t.MakeClient(port, t.cfg.HealthTimeout))
}

func (t *Tester) SpeedCheck(client *http.Client) (float64, int64, error) {
	req, _ := http.NewRequest("GET", t.cfg.SpeedTestURL, nil)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("speedtest download failed: %d", resp.StatusCode)
	}

	buf := make([]byte, 32*1024)
	written, err := io.CopyBuffer(io.Discard, resp.Body, buf)

	duration := time.Since(start)
	if duration.Seconds() == 0 {
		return 0, written, fmt.Errorf("download too fast")
	}

	bits := float64(written) * 8
	mbps := (bits / duration.Seconds()) / 1_000_000
	if err != nil && err != io.EOF {
		return mbps, written, err
	}
	return mbps, written, nil
}

func (t *Tester) SpeedCheckFromLink(link string) (float64, int64, error) {
	port, instance, err := xray.StartEphemeral(link)
	if err != nil {
		return 0, 0, err
	}
	defer instance.Close()
	return t.SpeedCheck(t.MakeClient(port, t.cfg.SpeedTimeout))
}

func (t *Tester) MakeClient(port int, timeout time.Duration) *http.Client {
	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   timeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
		Timeout: timeout + (2 * time.Second),
	}
}

// FetchHostMetadata resolves the target host (IP or Domain) using a DIRECT connection.
// This tells us the ISP of the proxy server itself (Entry Point).
func (t *Tester) FetchHostMetadata(client *http.Client, host string) (*MetadataResult, error) {
	// ip-api.com/json/{query} allows querying a specific IP/Domain
	url := fmt.Sprintf("http://ip-api.com/json/%s", host)
	
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("inbound fetch failed: %w", err)
	}
	defer resp.Body.Close()

	var apiData struct {
		Query       string `json:"query"` // The resolved IP
		ISP         string `json:"isp"`
		CountryCode string `json:"countryCode"`
		Status      string `json:"status"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&apiData); err != nil {
		return nil, err
	}
	
	if apiData.Status != "success" {
		return nil, fmt.Errorf("ip-api error for host %s", host)
	}

	return &MetadataResult{
		IP:      apiData.Query,
		ISP:     apiData.ISP,
		Country: apiData.CountryCode,
		IsDirty: false, // Not applicable for inbound
	}, nil
}
