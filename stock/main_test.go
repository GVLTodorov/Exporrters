package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// Every test must leave no goroutine behind (HTTP keep-alive readers,
	// rate-limiter sleeps, refresh loops).
	goleak.VerifyTestMain(m)
}

// polygon is a fake Polygon.io /v2/aggs/ticker/<SYM>/prev endpoint.
type polygon struct {
	prices map[string]float64
	status int // 0 = 200
	body   string
	calls  atomic.Int64
}

func (p *polygon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.calls.Add(1)
	if p.status != 0 {
		w.WriteHeader(p.status)
		_, _ = io.WriteString(w, p.body)
		return
	}
	if p.body != "" {
		_, _ = io.WriteString(w, p.body)
		return
	}
	sym := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/aggs/ticker/"), "/prev")
	price, ok := p.prices[sym]
	if !ok {
		_, _ = io.WriteString(w, `{"status":"OK","ticker":"`+sym+`","resultsCount":0,"results":[]}`)
		return
	}
	fmt.Fprintf(w, `{"status":"OK","ticker":%q,"resultsCount":1,"results":[{"c":%v}]}`, sym, price)
}

// setup points the exporter at srv, gives it a fresh cache and a rate
// limiter that never blocks, and restores everything afterwards.
func setup(t *testing.T, h http.Handler) (*httptest.Server, *connCounter) {
	t.Helper()
	srv, cc := newCountingServer(t, h)

	oldURL, oldCache, oldLimiter := polygonBaseURL, cache, limiter
	polygonBaseURL = srv.URL
	cache = &priceCache{prices: map[string]float64{}, ttl: time.Minute}
	// Never blocks (coarse clocks on Windows can stamp several requests
	// with the same instant, which a tiny maxRequests would turn into waits).
	limiter = &rateLimiter{maxRequests: 1 << 30, window: time.Nanosecond}
	priceGauge.Reset()
	t.Cleanup(func() {
		polygonBaseURL, cache, limiter = oldURL, oldCache, oldLimiter
		httpClient.CloseIdleConnections()
	})
	return srv, cc
}

func TestGetSymbols(t *testing.T) {
	t.Setenv("SYMBOLS", "")
	if _, err := getSymbols(); err == nil {
		t.Error("expected an error when SYMBOLS is empty")
	}
	t.Setenv("SYMBOLS", " QCOM, NVDA ,INTC")
	got, err := getSymbols()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "QCOM|NVDA|INTC" {
		t.Errorf("getSymbols() = %q, want trimmed QCOM|NVDA|INTC", got)
	}
}

func TestGetAPIKey(t *testing.T) {
	t.Setenv("POLYGON_API_KEY", "")
	if _, err := getAPIKey(); err == nil {
		t.Error("expected an error when POLYGON_API_KEY is empty")
	}
	t.Setenv("POLYGON_API_KEY", "k")
	if got, _ := getAPIKey(); got != "k" {
		t.Errorf("getAPIKey() = %q, want k", got)
	}
}

func TestFetchPriceOK(t *testing.T) {
	var gotPath, gotKey string
	setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.URL.Query().Get("apikey")
		_, _ = io.WriteString(w, `{"status":"OK","resultsCount":1,"results":[{"c":123.45}]}`)
	}))

	price, err := fetchPrice("NVDA", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if price != 123.45 {
		t.Errorf("price = %v, want 123.45", price)
	}
	if gotPath != "/v2/aggs/ticker/NVDA/prev" || gotKey != "secret" {
		t.Errorf("request = %s apikey=%s, want /v2/aggs/ticker/NVDA/prev apikey=secret", gotPath, gotKey)
	}
}

func TestFetchPriceErrors(t *testing.T) {
	cases := []struct {
		name    string
		p       *polygon
		wantErr string
	}{
		{"rate limited", &polygon{status: http.StatusTooManyRequests, body: "slow down"}, "rate limit"},
		{"server error", &polygon{status: http.StatusInternalServerError, body: "boom"}, "unexpected status"},
		{"bad json", &polygon{body: "not json"}, "failed to parse JSON"},
		{"api status", &polygon{body: `{"status":"ERROR"}`}, "non-OK status"},
		{"no results", &polygon{prices: map[string]float64{}}, "no results"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup(t, tc.p)
			_, err := fetchPrice("AAPL", "k")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestFetchPriceTimesOut(t *testing.T) {
	release := make(chan struct{})
	setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer close(release)

	old := httpClient.Timeout
	httpClient.Timeout = 100 * time.Millisecond
	t.Cleanup(func() { httpClient.Timeout = old })

	start := time.Now()
	if _, err := fetchPrice("AAPL", "k"); err == nil {
		t.Fatal("expected a timeout error from a hung upstream")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("fetchPrice took %v, want it to give up after the client timeout", d)
	}
}

func TestFetchPricesFallsBackToCache(t *testing.T) {
	p := &polygon{prices: map[string]float64{"A": 1}}
	setup(t, p)
	cache.set("B", 42) // B fails upstream (no results) but has a cached value

	got := fetchPrices([]string{"A", "B", "C"}, "k")
	if got["A"] != 1 {
		t.Errorf("A = %v, want fresh price 1", got["A"])
	}
	if got["B"] != 42 {
		t.Errorf("B = %v, want cached price 42", got["B"])
	}
	if _, ok := got["C"]; ok {
		t.Errorf("C should be absent: it failed and was never cached")
	}
	if v, _ := cache.get("A"); v != 1 {
		t.Errorf("cache[A] = %v, want 1 after a successful fetch", v)
	}
}

func TestPriceCache(t *testing.T) {
	c := &priceCache{prices: map[string]float64{}, ttl: 50 * time.Millisecond}
	if !c.isExpired() {
		t.Error("a never-updated cache should be expired")
	}
	c.set("X", 1.5)
	if c.isExpired() {
		t.Error("cache should be fresh right after set")
	}
	all := c.getAll()
	all["X"] = 99 // getAll must return a copy
	if v, _ := c.get("X"); v != 1.5 {
		t.Errorf("mutating getAll() result changed the cache: X = %v", v)
	}
	time.Sleep(60 * time.Millisecond)
	if !c.isExpired() {
		t.Error("cache should expire after ttl")
	}
}

func TestRateLimiterWaitsWhenFull(t *testing.T) {
	rl := &rateLimiter{maxRequests: 2, window: 200 * time.Millisecond}
	start := time.Now()
	rl.waitIfNeeded()
	rl.waitIfNeeded()
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("first %d requests should not wait, took %v", rl.maxRequests, d)
	}
	rl.waitIfNeeded() // third request inside the window must wait
	if d := time.Since(start); d < 200*time.Millisecond {
		t.Errorf("third request returned after %v, want >= window (200ms)", d)
	}
}

func TestUpdateMetricsAndHandler(t *testing.T) {
	setup(t, &polygon{})
	cache.set("QCOM", 170.5)

	rec := httptest.NewRecorder()
	metricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if !strings.Contains(rec.Body.String(), `stock_price{symbol="QCOM"} 170.5`) {
		t.Errorf("/metrics does not contain the cached QCOM price:\n%s", rec.Body.String())
	}
	if got := testutil.ToFloat64(priceGauge.WithLabelValues("QCOM")); got != 170.5 {
		t.Errorf("stock_price{QCOM} = %v, want 170.5", got)
	}
}

func TestHandlerRefreshesExpiredCache(t *testing.T) {
	p := &polygon{prices: map[string]float64{"NVDA": 900}}
	setup(t, p)
	cache.ttl = 0 // always expired
	t.Setenv("SYMBOLS", "NVDA")
	t.Setenv("POLYGON_API_KEY", "k")

	rec := httptest.NewRecorder()
	metricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if p.calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1 refresh", p.calls.Load())
	}
	if !strings.Contains(rec.Body.String(), `stock_price{symbol="NVDA"} 900`) {
		t.Errorf("/metrics missing refreshed NVDA price")
	}
}

func TestMinHelper(t *testing.T) {
	if min(1, 2) != 1 || min(3, 2) != 2 {
		t.Error("min is wrong")
	}
}
