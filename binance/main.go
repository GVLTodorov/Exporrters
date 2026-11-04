package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// binanceBaseURL is a variable so tests can point it at a local server.
	binanceBaseURL = "https://api.binance.com"

	// A timeout keeps a hung upstream request from blocking the scrape
	// (and the goroutine + socket behind it) forever. Prometheus' default
	// scrape timeout is 10s, so give up before that.
	httpClient = &http.Client{Timeout: 8 * time.Second}

	// Built once: promhttp.Handler() allocates a new instrumented handler
	// on every call.
	promHandler = promhttp.Handler()
)

// maxBodyBytes caps how much of an upstream response is read into memory.
// A real ticker/price reply is ~50 bytes per symbol.
const maxBodyBytes = 1 << 20

type Price struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

var priceGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "binance_crypto_price",
		Help: "Current cryptocurrency prices from Binance",
	},
	[]string{"symbol"},
)

func init() {
	prometheus.MustRegister(priceGauge)
}

func getSymbols() []string {
	symbolsEnv := os.Getenv("SYMBOLS")
	if symbolsEnv == "" {
		return []string{"BTCUSDT", "ETHUSDT", "BNBUSDT"}
	}
	var symbols []string
	for _, s := range strings.Split(symbolsEnv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			symbols = append(symbols, s)
		}
	}
	return symbols
}

func fetchPrices(symbols []string) ([]Price, error) {
	symbolsJson, err := json.Marshal(symbols)
	if err != nil {
		return nil, err
	}

	apiUrl := fmt.Sprintf("%s/api/v3/ticker/price?symbols=%s", binanceBaseURL, url.QueryEscape(string(symbolsJson)))

	resp, err := httpClient.Get(apiUrl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the whole (capped) body so the connection can be reused.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s, body: %s", resp.Status, truncate(body, 512))
	}

	var prices []Price
	if err := json.Unmarshal(body, &prices); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	return prices, nil
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}

func updateMetrics(prices []Price) {
	for _, price := range prices {
		value, err := parsePrice(price.Price)
		if err != nil {
			fmt.Printf("Error parsing price for %s: %v\n", price.Symbol, err)
			continue
		}
		priceGauge.WithLabelValues(price.Symbol).Set(value)
	}
}

func parsePrice(priceStr string) (float64, error) {
	return strconv.ParseFloat(priceStr, 64)
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	symbols := getSymbols()

	prices, err := fetchPrices(symbols)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch prices: %v", err), http.StatusInternalServerError)
		return
	}

	updateMetrics(prices)
	promHandler.ServeHTTP(w, r)
}

func main() {
	if v := os.Getenv("BINANCE_BASE_URL"); v != "" {
		binanceBaseURL = strings.TrimRight(v, "/")
	}

	fmt.Println("Starting server on :8080")
	fmt.Printf("Symbols: %v\n", getSymbols())
	if err := http.ListenAndServe(":8080", newMux(http.HandlerFunc(metricsHandler))); err != nil {
		fmt.Printf("Server failed to start: %v\n", err)
	}
}
