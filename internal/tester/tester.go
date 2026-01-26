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

func (t *Tester) Analyze(client *http.Client) (*AnalysisResult, error) {
	var lastErr error
	
	// Retry Loop
	for i := 0; i <= t.cfg.Retries; i++ {
		res, err := t.doAnalyze(client)
		if err == nil {
			return res, nil
		}
		lastErr = err
		// Brief backoff
		if i < t.cfg.Retries {
			time.Sleep(200 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func (t *Tester) doAnalyze(client *http.Client) (*AnalysisResult, error) {
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
	var mbps float64
	var written int64
	var lastErr error

	// Retry Loop
	for i := 0; i <= t.cfg.Retries; i++ {
		m, w, err := t.doSpeedCheck(client)
		if err == nil {
			return m, w, nil
		}
		lastErr = err
		// Keep best effort data count
		if w > written {
			written = w 
		}
		
		if i < t.cfg.Retries {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return mbps, written, lastErr
}

func (t *Tester) doSpeedCheck(client *http.Client) (float64, int64, error) {
	req, _ := http.NewRequest("GET", t.cfg.SpeedTestURL, nil)

	// Note: We do NOT start the timer here. We exclude TTFB.
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("speedtest download failed: %d", resp.StatusCode)
	}

	// Start timer ONLY after headers are received and body is ready to stream
	start := time.Now()

	buf := make([]byte, 32*1024)
	var written int64
	var readErr error

	for {
		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			written += int64(nr)
		}
		if er != nil {
			if er != io.EOF {
				readErr = er
			}
			break
		}
	}

	duration := time.Since(start)

	if readErr != nil && written == 0 {
		return 0, written, fmt.Errorf("download failed (0 bytes): %w", readErr)
	}

	if duration.Seconds() == 0 {
		return 0, written, fmt.Errorf("download too fast or duration zero")
	}

	bits := float64(written) * 8
	mbps := (bits / duration.Seconds()) / 1_000_000

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

func (t *Tester) MakeClient(port int, totalTimeout time.Duration) *http.Client {
	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	
	// Requirement: Connection and Headers must happen within HealthTimeout
	// even if the total operation (like SpeedTest) is allowed to take longer.
	connectTimeout := t.cfg.HealthTimeout

	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   connectTimeout, // Enforce TCP Handshake limit
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: connectTimeout, // Enforce TTFB limit
		},
		Timeout: totalTimeout, // Enforce Total Duration limit
	}
}

// ResolveHost resolves the target host to an IP and looks up metadata locally.
func (t *Tester) ResolveHost(host string) (*AnalysisResult, error) {
	// If host is already an IP
	if net.ParseIP(host) != nil {
		geo, _ := geoip.Lookup(host)
		return &AnalysisResult{IP: host, ISP: geo.ISP, Country: geo.Country}, nil
	}

	// DNS Lookup with Retries
	var ips []net.IP
	var err error

	for i := 0; i <= t.cfg.Retries; i++ {
		ips, err = net.LookupIP(host)
		if err == nil && len(ips) > 0 {
			break
		}
		if i < t.cfg.Retries {
			time.Sleep(200 * time.Millisecond)
		}
	}

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
