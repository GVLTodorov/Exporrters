package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestLeakFetchPriceReusesConnections: if a response body is not closed,
// net/http cannot put the connection back in the pool and every request
// dials a new socket (plus two goroutines) that live until the server
// gives up on them. Success and error replies are both covered.
func TestLeakFetchPriceReusesConnections(t *testing.T) {
	p := &polygon{prices: map[string]float64{"A": 1}}
	_, cc := setup(t, p)

	for i := 0; i < 300; i++ {
		switch i % 3 {
		case 0:
			p.status, p.body = 0, ""
		case 1:
			p.status, p.body = http.StatusTooManyRequests, "slow down"
		case 2:
			p.status, p.body = http.StatusInternalServerError, "boom"
		}
		_, _ = fetchPrice("A", "k")
	}

	if opened, _ := cc.counts(); opened > 2 {
		t.Errorf("300 requests opened %d connections, want <= 2 (bodies are not being closed/drained)", opened)
	}
}

// TestLeakFetchPriceCapsResponseBody: a misbehaving proxy or captive portal
// returning a huge body must not be read into memory in full.
func TestLeakFetchPriceCapsResponseBody(t *testing.T) {
	const huge = 64 << 20 // 64 MiB
	setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 32<<10)
		for sent := 0; sent < huge; sent += len(chunk) {
			if _, err := io.WriteString(w, chunk); err != nil {
				return // client hung up, as it should
			}
		}
	}))

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := fetchPrice("A", "k")
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("expected a parse error for a 64 MiB non-JSON body")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Errorf("fetchPrice allocated %d MiB for one oversized response, want < 16 MiB (body is not capped)", allocated>>20)
	}
}

// TestLeakRefreshCyclesKeepHeapAndSeriesStable runs many full refresh
// cycles (fetch -> cache -> gauges -> /metrics) and checks nothing is
// retained per cycle and the number of series equals the symbol count.
func TestLeakRefreshCyclesKeepHeapAndSeriesStable(t *testing.T) {
	symbols := []string{"QCOM", "NVDA", "INTC", "MU", "GSL"}
	p := &polygon{prices: map[string]float64{"QCOM": 1, "NVDA": 2, "INTC": 3, "MU": 4, "GSL": 5}}
	_, cc := setup(t, p)

	cycle := func() {
		updateMetrics(fetchPrices(symbols, "k"))
		rec := httptest.NewRecorder()
		metricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	}
	assertNoHeapGrowth(t, 50, 1000, 512<<10, cycle)

	if n := testutil.CollectAndCount(priceGauge); n != len(symbols) {
		t.Errorf("stock_price has %d series, want %d", n, len(symbols))
	}
	if opened, _ := cc.counts(); opened > 2 {
		t.Errorf("%d refresh cycles opened %d connections, want <= 2", 1050, opened)
	}
}

// TestLeakRateLimiterHistoryIsBounded: the limiter keeps a timestamp per
// request; it must prune old ones instead of growing with uptime.
func TestLeakRateLimiterHistoryIsBounded(t *testing.T) {
	// Saturated: every 5th call has to wait (window + the limiter's 100ms
	// safety buffer), so keep the count small.
	rl := &rateLimiter{maxRequests: 5, window: 5 * time.Millisecond}
	for i := 0; i < 20; i++ {
		rl.waitIfNeeded()
		if n := len(rl.requests); n > rl.maxRequests {
			t.Fatalf("after %d calls the limiter holds %d timestamps, want <= %d", i+1, n, rl.maxRequests)
		}
	}
	// Idle: after the window passes, old timestamps are pruned.
	time.Sleep(10 * time.Millisecond)
	rl.waitIfNeeded()
	if n := len(rl.requests); n != 1 {
		t.Errorf("after an idle window the limiter holds %d timestamps, want 1", n)
	}
}

// TestLeakHungUpstreamDoesNotPinGoroutines: with the client timeout, a
// stuck Polygon request ends and its goroutines are released (goleak in
// TestMain verifies nothing is left once the test finishes).
func TestLeakHungUpstreamDoesNotPinGoroutines(t *testing.T) {
	setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	old := httpClient.Timeout
	httpClient.Timeout = 50 * time.Millisecond
	t.Cleanup(func() { httpClient.Timeout = old })

	base := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		if _, err := fetchPrice(fmt.Sprintf("S%d", i), "k"); err == nil {
			t.Fatal("expected timeout")
		}
	}
	httpClient.CloseIdleConnections()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if g := runtime.NumGoroutine(); g > base+2 {
		t.Errorf("goroutines grew from %d to %d after 20 timed-out requests", base, g)
	}
}
