package tester

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"rizznet/internal/xray"

	"github.com/xtls/xray-core/core"
)

// FindFirstAlive spins up all links and races them to fetch the targetURL.
// It returns the local port of the winner and the Xray instance (which MUST be closed by the caller eventually).
func (t *Tester) FindFirstAlive(links []string, targetURL string) (int, *core.Instance, error) {
	// 1. Start Xray for all candidates
	portMap, instance, err := xray.StartMultiEphemeral(links)
	if err != nil {
		return 0, nil, err
	}

	// 2. Setup Race Channels
	winChan := make(chan int, 1)
	doneChan := make(chan struct{})
	var once sync.Once

	var wg sync.WaitGroup

	// 3. Launch Workers
	for _, link := range links {
		port, ok := portMap[link]
		if !ok {
			continue
		}

		wg.Add(1)
		go func(p int) {
			defer wg.Done()

			// Check if race is already over
			select {
			case <-doneChan:
				return
			default:
			}

			// Perform Check using standard Tester logic (Retries/Timeouts)
			if t.checkSimple(p, targetURL) {
				// Try to declare victory
				select {
				case winChan <- p:
					once.Do(func() { close(doneChan) })
				default:
				}
			}
		}(port)
	}

	// 4. Closer routine
	go func() {
		wg.Wait()
		close(winChan)
	}()

	// 5. Await Result
	winnerPort, ok := <-winChan
	if ok {
		return winnerPort, instance, nil
	}

	// No winner
	instance.Close()
	return 0, nil, fmt.Errorf("no alive proxies found in batch")
}

// checkSimple is a lightweight boolean check using the standardized MakeClient
func (t *Tester) checkSimple(port int, url string) bool {
	client := t.MakeClient(port, t.cfg.HealthTimeout)
	
	// Retry loop is implicit if we use t.Analyze, but Analyze does heavy GeoIP parsing.
	// We just want a simple 200 OK check here with the Configured Retries.
	
	for i := 0; i <= t.cfg.Retries; i++ {
		// Create a context for the request
		ctx, cancel := context.WithTimeout(context.Background(), t.cfg.HealthTimeout)
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				cancel()
				return true
			}
		}
		cancel() // Clean up context

		// Backoff
		if i < t.cfg.Retries {
			// Fast backoff for race
			// We don't sleep here to block the thread, but since we are in a goroutine it's fine.
		}
	}
	
	return false
}