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
	"rizznet/internal/metrics" 
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

// Analyze performs a full check (IP, ISP, Country, Dirty) and records metrics.
func (t *Tester) Analyze(client *http.Client, mc *metrics.Collector) (*AnalysisResult, error) {
	var lastErr error
	
	// Retry Loop
	for i := 0; i <= t.cfg.Retries; i++ {
		// Fix: We calculate maxReqDuration inside doAnalyze.
		// This represents the slowest single request in the chain.
		// We use this for metrics because 'health_timeout' config applies per-request.
		res, maxReqDuration, err := t.doAnalyze(client)
		
		if err == nil {
			// Success
			if mc != nil {
				mc.RecordSuccess(i, maxReqDuration)
			}
			return res, nil
		}

		// Failure
		lastErr = err
		
		if mc != nil {
			mc.RecordFailure(err)
		}

		// Brief backoff
		if i < t.cfg.Retries {
			time.Sleep(200 * time.Millisecond)
		}
	}
	return nil, lastErr
}

// doAnalyze returns the result and the duration of the slowest individual request.
func (t *Tester) doAnalyze(client *http.Client) (*AnalysisResult, time.Duration, error) {
	var maxDuration time.Duration

	// 1. Echo Request
	start1 := time.Now()
	resp, err := client.Get(t.cfg.EchoURL)
	dur1 := time.Since(start1)
	if dur1 > maxDuration {
		maxDuration = dur1
	}

	if err != nil {
		return nil, maxDuration, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, maxDuration, fmt.Errorf("echo failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, maxDuration, err
	}

	ipStr := strings.TrimSpace(string(body))
	if ipStr == "" {
		return nil, maxDuration, fmt.Errorf("empty response from echo service")
	}

	geo, err := geoip.Lookup(ipStr)
	if err != nil {
		geo = &geoip.GeoResult{ISP: "Unknown", Country: "XX"}
	}

	res := &AnalysisResult{
		IP:      ipStr,
		ISP:     geo.ISP,
		Country: geo.Country,
		IsDirty: false,
	}

	// 2. Dirty Check Request (Optional)
	if t.cfg.DirtyCheckURL != "" {
		start2 := time.Now()
		dirtyResp, err := client.Get(t.cfg.DirtyCheckURL)
		dur2 := time.Since(start2)
		if dur2 > maxDuration {
			maxDuration = dur2
		}

		if err == nil && dirtyResp.StatusCode == 200 {
			res.IsDirty = false
			dirtyResp.Body.Close()
		} else {
			res.IsDirty = true
			// Note: A network failure here marks it as "Dirty" rather than failing the whole test,
			// preserving original logic.
		}
	}

	return res, maxDuration, nil
}

// AnalyzeFromLink starts a temporary Xray instance and runs Analyze.
// Metrics are passed as nil since this is usually an ad-hoc check.
func (t *Tester) AnalyzeFromLink(link string) (*AnalysisResult, error) {
	port, instance, err := xray.StartEphemeral(link)
	if err != nil {
		return nil, err
	}
	defer instance.Close()
	
	// pass nil for metrics
	return t.Analyze(t.MakeClient(port, t.cfg.HealthTimeout), nil)
}

func (t *Tester) SpeedCheck(client *http.Client) (float64, int64, error) {
	var mbps float64
	var written int64
	var lastErr error

	for i := 0; i <= t.cfg.Retries; i++ {
		m, w, err := t.doSpeedCheck(client)
		if err == nil {
			return m, w, nil
		}
		lastErr = err
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

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("speedtest download failed: %d", resp.StatusCode)
	}

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
	
	connectTimeout := t.cfg.HealthTimeout

	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   connectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: connectTimeout,
		},
		Timeout: totalTimeout,
	}
}

func (t *Tester) ResolveHost(host string) (*AnalysisResult, error) {
	if net.ParseIP(host) != nil {
		geo, _ := geoip.Lookup(host)
		return &AnalysisResult{IP: host, ISP: geo.ISP, Country: geo.Country}, nil
	}

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
