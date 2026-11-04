package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestLeakFetchPricesReusesConnections: if a response body is not closed
// and drained, net/http cannot put the connection back in the pool and
// every scrape dials a new socket (plus two goroutines) that live until
// the server gives up on them. Success and error replies are both covered.
func TestLeakFetchPricesReusesConnections(t *testing.T) {
	b := &binance{prices: map[string]float64{"BTCUSDT": 1}}
	_, cc := setup(t, b)

	for i := 0; i < 300; i++ {
		switch i % 3 {
		case 0:
			b.status, b.body = 0, ""
		case 1:
			b.status, b.body = http.StatusTooManyRequests, `{"code":-1003,"msg":"Too many requests"}`
		case 2:
			b.status, b.body = http.StatusInternalServerError, "boom"
		}
		_, _ = fetchPrices([]string{"BTCUSDT"})
	}

	if opened, _ := cc.counts(); opened > 2 {
		t.Errorf("300 requests opened %d connections, want <= 2 (bodies are not being closed/drained)", opened)
	}
}

// TestLeakFetchPricesCapsResponseBody: a misbehaving proxy or captive
// portal returning a huge body must not be read into memory in full.
func TestLeakFetchPricesCapsResponseBody(t *testing.T) {
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
	_, err := fetchPrices([]string{"BTCUSDT"})
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("expected a parse error for a 64 MiB non-JSON body")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Errorf("fetchPrices allocated %d MiB for one oversized response, want < 16 MiB (body is not capped)", allocated>>20)
	}
}

// TestLeakScrapeCyclesKeepHeapAndSeriesStable runs many full scrapes
// (fetch -> gauges -> /metrics) and checks nothing is retained per scrape
// and the number of series equals the symbol count.
func TestLeakScrapeCyclesKeepHeapAndSeriesStable(t *testing.T) {
	symbols := "BTCUSDT,ETHUSDT,BNBUSDT,SOLUSDT,XRPUSDT"
	b := &binance{prices: map[string]float64{"BTCUSDT": 1, "ETHUSDT": 2, "BNBUSDT": 3, "SOLUSDT": 4, "XRPUSDT": 5}}
	_, cc := setup(t, b)
	t.Setenv("SYMBOLS", symbols)

	cycle := func() {
		rec := httptest.NewRecorder()
		metricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /metrics = %d", rec.Code)
		}
	}
	assertNoHeapGrowth(t, 50, 1000, 512<<10, cycle)

	if n := testutil.CollectAndCount(priceGauge); n != 5 {
		t.Errorf("binance_crypto_price has %d series, want 5", n)
	}
	if opened, _ := cc.counts(); opened > 2 {
		t.Errorf("%d scrapes opened %d connections, want <= 2", 1050, opened)
	}
}

// TestLeakHungUpstreamDoesNotPinGoroutines: with the client timeout, a
// stuck Binance request ends and its goroutines are released (goleak in
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
		if _, err := fetchPrices([]string{"BTCUSDT"}); err == nil {
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
