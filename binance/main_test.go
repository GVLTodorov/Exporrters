package main

import (
	"encoding/json"
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
	// hung upstream requests).
	goleak.VerifyTestMain(m)
}

// binance is a fake Binance /api/v3/ticker/price?symbols=[...] endpoint.
type binance struct {
	prices map[string]float64
	status int // 0 = 200
	body   string
	calls  atomic.Int64
}

func (b *binance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.calls.Add(1)
	if b.status != 0 {
		w.WriteHeader(b.status)
		_, _ = io.WriteString(w, b.body)
		return
	}
	if b.body != "" {
		_, _ = io.WriteString(w, b.body)
		return
	}
	var symbols []string
	if err := json.Unmarshal([]byte(r.URL.Query().Get("symbols")), &symbols); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":-1100,"msg":"Illegal characters found in parameter 'symbols'"}`)
		return
	}
	out := make([]Price, 0, len(symbols))
	for _, s := range symbols {
		if p, ok := b.prices[s]; ok {
			out = append(out, Price{Symbol: s, Price: fmt.Sprintf("%.8f", p)})
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

// setup points the exporter at h, clears the gauge and restores
// everything afterwards.
func setup(t *testing.T, h http.Handler) (*httptest.Server, *connCounter) {
	t.Helper()
	srv, cc := newCountingServer(t, h)

	oldURL := binanceBaseURL
	binanceBaseURL = srv.URL
	priceGauge.Reset()
	t.Cleanup(func() {
		binanceBaseURL = oldURL
		httpClient.CloseIdleConnections()
	})
	return srv, cc
}

func TestGetSymbols(t *testing.T) {
	t.Setenv("SYMBOLS", "")
	if got := strings.Join(getSymbols(), "|"); got != "BTCUSDT|ETHUSDT|BNBUSDT" {
		t.Errorf("default getSymbols() = %q, want BTCUSDT|ETHUSDT|BNBUSDT", got)
	}
	t.Setenv("SYMBOLS", " BTCUSDT, ETHUSDT ,,SOLUSDT,")
	if got := strings.Join(getSymbols(), "|"); got != "BTCUSDT|ETHUSDT|SOLUSDT" {
		t.Errorf("getSymbols() = %q, want trimmed BTCUSDT|ETHUSDT|SOLUSDT", got)
	}
}

func TestFetchPricesOK(t *testing.T) {
	var gotPath, gotSymbols string
	setup(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotSymbols = r.URL.Path, r.URL.Query().Get("symbols")
		_, _ = io.WriteString(w, `[{"symbol":"BTCUSDT","price":"67012.34000000"},{"symbol":"ETHUSDT","price":"3456.78000000"}]`)
	}))

	prices, err := fetchPrices([]string{"BTCUSDT", "ETHUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v3/ticker/price" || gotSymbols != `["BTCUSDT","ETHUSDT"]` {
		t.Errorf("request = %s symbols=%s, want /api/v3/ticker/price symbols=[\"BTCUSDT\",\"ETHUSDT\"]", gotPath, gotSymbols)
	}
	if len(prices) != 2 || prices[0] != (Price{"BTCUSDT", "67012.34000000"}) || prices[1] != (Price{"ETHUSDT", "3456.78000000"}) {
		t.Errorf("prices = %+v", prices)
	}
}

func TestFetchPricesErrors(t *testing.T) {
	cases := []struct {
		name    string
		b       *binance
		wantErr string
	}{
		{"bad symbol", &binance{status: http.StatusBadRequest, body: `{"code":-1121,"msg":"Invalid symbol."}`}, "Invalid symbol"},
		{"rate limited", &binance{status: http.StatusTooManyRequests, body: "slow down"}, "429"},
		{"geo blocked", &binance{status: http.StatusUnavailableForLegalReasons, body: "restricted location"}, "451"},
		{"server error", &binance{status: http.StatusInternalServerError, body: "boom"}, "unexpected status"},
		{"bad json", &binance{body: "not json"}, "failed to parse JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup(t, tc.b)
			_, err := fetchPrices([]string{"BTCUSDT"})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestFetchPricesTimesOut(t *testing.T) {
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
	if _, err := fetchPrices([]string{"BTCUSDT"}); err == nil {
		t.Fatal("expected a timeout error from a hung upstream")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("fetchPrices took %v, want it to give up after the client timeout", d)
	}
}

func TestUpdateMetricsSkipsUnparsablePrices(t *testing.T) {
	priceGauge.Reset()
	t.Cleanup(priceGauge.Reset)

	updateMetrics([]Price{{"BTCUSDT", "67000.5"}, {"BAD", "n/a"}})
	if got := testutil.ToFloat64(priceGauge.WithLabelValues("BTCUSDT")); got != 67000.5 {
		t.Errorf("BTCUSDT = %v, want 67000.5", got)
	}
	if n := testutil.CollectAndCount(priceGauge); n != 1 {
		t.Errorf("binance_crypto_price has %d series, want 1 (BAD must be skipped)", n)
	}
}

func TestMetricsHandler(t *testing.T) {
	b := &binance{prices: map[string]float64{"BTCUSDT": 67000.5, "ETHUSDT": 3400}}
	setup(t, b)
	t.Setenv("SYMBOLS", "BTCUSDT,ETHUSDT")

	rec := httptest.NewRecorder()
	metricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`binance_crypto_price{symbol="BTCUSDT"} 67000.5`,
		`binance_crypto_price{symbol="ETHUSDT"} 3400`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics is missing %q", want)
		}
	}
}

func TestMetricsHandlerUpstreamError(t *testing.T) {
	setup(t, &binance{status: http.StatusInternalServerError, body: "boom"})

	rec := httptest.NewRecorder()
	metricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET /metrics with a failing upstream = %d, want 500", rec.Code)
	}
}
