package tester

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rizznet/internal/config"
	"rizznet/internal/geoip"
	"rizznet/internal/xray"
)

type Tester struct {
	cfg config.TesterConfig
}

type AnalysisResult struct {
	IP      string
	ISP     string
	Country string
	IsDirty bool
}

func New(cfg config.TesterConfig) *Tester {
	return &Tester{cfg: cfg}
}

// Analyze replaces HealthCheck and MetadataCheck.
// It connects via proxy, hits the EchoURL to get IP, then looks up metadata locally.
func (t *Tester) Analyze(client *http.Client) (*AnalysisResult, error) {
	// 1. Fetch IP (Echo / Health)
	resp, err := client.Get(t.cfg.EchoURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("echo failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Clean up IP string (trim whitespace)
	ipStr := strings.TrimSpace(string(body))
	if ipStr == "" {
		return nil, fmt.Errorf("empty response from echo service")
	}

	// 2. Local GeoIP Lookup
	geo, err := geoip.Lookup(ipStr)
	if err != nil {
		// If GeoIP fails, we still have a working proxy, return bare minimum
		geo = &geoip.GeoResult{ISP: "Unknown", Country: "XX"}
	}

	res := &AnalysisResult{
		IP:      ipStr,
		ISP:     geo.ISP,
		Country: geo.Country,
		IsDirty: false,
	}

	// 3. Dirty Check (Optional)
	if t.cfg.DirtyCheckURL != "" {
		dirtyResp, err := client.Get(t.cfg.DirtyCheckURL)
		if err == nil && dirtyResp.StatusCode == 200 {
			res.IsDirty = false
			dirtyResp.Body.Close()
		} else {
			res.IsDirty = true
		}
	}

	return res, nil
}

func (t *Tester) AnalyzeFromLink(link string) (*AnalysisResult, error) {
	port, instance, err := xray.StartEphemeral(link)
	if err != nil {
		return nil, err
	}
	defer instance.Close()
	return t.Analyze(t.MakeClient(port, t.cfg.HealthTimeout))
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

// ResolveHost resolves the target host to an IP and looks up metadata locally.
func (t *Tester) ResolveHost(host string) (*AnalysisResult, error) {
	// If host is already an IP
	if net.ParseIP(host) != nil {
		geo, _ := geoip.Lookup(host)
		return &AnalysisResult{IP: host, ISP: geo.ISP, Country: geo.Country}, nil
	}

	// DNS Lookup
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("dns lookup failed for %s", host)
	}
	
	ipStr := ips[0].String()
	geo, _ := geoip.Lookup(ipStr)
	
	return &AnalysisResult{
		IP:      ipStr,
		ISP:     geo.ISP,
		Country: geo.Country,
	}, nil
}
