package metrics

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

type Collector struct {
	mu sync.Mutex

	// Latency Tracking (Successes only)
	latencies []time.Duration

	// Retry Tracking
	successByAttempt map[int]int
	totalSuccess     int

	// Error Tracking
	errorCounts map[string]int
	totalErrors int

	// Network Saturation Heuristic
	timeoutErrors int
}

func New() *Collector {
	return &Collector{
		successByAttempt: make(map[int]int),
		errorCounts:      make(map[string]int),
	}
}

func (c *Collector) RecordSuccess(attempt int, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.latencies = append(c.latencies, duration)
	c.successByAttempt[attempt]++
	c.totalSuccess++
}

func (c *Collector) RecordFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalErrors++
	
	msg := err.Error()
	errType := "Unknown"

	// Categorize errors for the saturation heuristic
	if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout") {
		errType = "Timeout (Slow)"
		c.timeoutErrors++
	} else if strings.Contains(msg, "refused") {
		errType = "Conn Refused (Fast)"
	} else if strings.Contains(msg, "reset") {
		errType = "Conn Reset (Fast)"
	} else if strings.Contains(msg, "EOF") {
		errType = "EOF / Empty"
	} else if strings.Contains(msg, "no such host") {
		errType = "DNS Error"
	}

	c.errorCounts[errType]++
}

func (c *Collector) PrintReport(currentTimeout time.Duration, currentRetries int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Println("\n📊 \033[1mTUNING & METRICS REPORT\033[0m")
	fmt.Println("────────────────────────────────────────")

	// 1. Latency / Config Tuning
	if len(c.latencies) > 0 {
		sort.Slice(c.latencies, func(i, j int) bool { return c.latencies[i] < c.latencies[j] })
		
		p50 := c.latencies[len(c.latencies)/2]
		p90 := c.latencies[int(float64(len(c.latencies))*0.9)]
		
		fmt.Fprintln(w, "\033[1;36m[ LATENCY (Successful Proxies) ]\033[0m")
		fmt.Fprintf(w, "  Avg Duration:\t%v\n", average(c.latencies))
		fmt.Fprintf(w, "  p50 (Median):\t%v\n", p50)
		fmt.Fprintf(w, "  p90 (Slowest 10%%):\t%v\n", p90)
		
		recTimeout := p90 + (500 * time.Millisecond)
		fmt.Fprintf(w, "  💡 Recommendation:\tSet 'health_timeout' to ~%s (Current: %s)\n", recTimeout.Round(time.Second), currentTimeout)
		fmt.Fprintln(w, "")
	}

	// 2. Retry Efficiency
	fmt.Fprintln(w, "\033[1;36m[ RETRY EFFICIENCY ]\033[0m")
	if c.totalSuccess > 0 {
		fmt.Fprintf(w, "  Total Survivors:\t%d\n", c.totalSuccess)
		for i := 0; i <= currentRetries; i++ {
			count := c.successByAttempt[i]
			pct := float64(count) / float64(c.totalSuccess) * 100
			fmt.Fprintf(w, "  Succeeded on Try %d:\t%d (%.1f%%)\n", i+1, count, pct)
		}

		// Recommendation Logic
		neededRetries := 0
		accumulated := 0.0
		for i := 0; i <= currentRetries; i++ {
			accumulated += float64(c.successByAttempt[i]) / float64(c.totalSuccess)
			if accumulated > 0.98 { // If 98% caught, stop here
				neededRetries = i
				break
			}
		}
		fmt.Fprintf(w, "  💡 Recommendation:\tSet 'retries' to %d (Current: %d)\n", neededRetries, currentRetries)
	} else {
		fmt.Fprintln(w, "  No survivors to analyze.")
	}
	fmt.Fprintln(w, "")

	// 3. Network Saturation (The Limit Check)
	fmt.Fprintln(w, "\033[1;36m[ NETWORK HEALTH / ERRORS ]\033[0m")
	fmt.Fprintf(w, "  Total Failures:\t%d\n", c.totalErrors)
	
	if c.totalErrors > 0 {
		timeoutPct := float64(c.timeoutErrors) / float64(c.totalErrors) * 100
		fmt.Fprintf(w, "  Timeouts (Potential Congestion):\t%d (%.1f%%)\n", c.timeoutErrors, timeoutPct)
		
		for k, v := range c.errorCounts {
			if k != "Timeout (Slow)" {
				fmt.Fprintf(w, "  %s:\t%d\n", k, v)
			}
		}

		fmt.Fprintln(w, "  --------------------------------")
		if timeoutPct > 70 {
			fmt.Fprintln(w, "  ⚠️  \033[1;31mHIGH SATURATION DETECTED\033[0m")
			fmt.Fprintln(w, "  >70% of failures are Timeouts. This suggests your network bandwidth")
			fmt.Fprintln(w, "  or NAT table is choked, or packets are being dropped silently.")
			fmt.Fprintln(w, "  💡 Recommendation: \033[1mDECREASE worker_count\033[0m")
		} else {
			fmt.Fprintln(w, "  ✅ Network seems stable (Failures are mostly active rejections).")
		}
	}
	
	w.Flush()
	fmt.Println("")
}

func average(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	var sum time.Duration
	for _, v := range d {
		sum += v
	}
	return time.Duration(int64(sum) / int64(len(d)))
}