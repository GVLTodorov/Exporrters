package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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
	return strings.Split(symbolsEnv, ",")
}

func fetchPrices(symbols []string) ([]Price, error) {
	symbolsJson, err := json.Marshal(symbols)
	if err != nil {
		return nil, err
	}

	apiUrl := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbols=%s", url.QueryEscape(string(symbolsJson)))

	resp, err := http.Get(apiUrl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var prices []Price
	if err := json.NewDecoder(resp.Body).Decode(&prices); err != nil {
		return nil, err
	}

	return prices, nil
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
	promhttp.Handler().ServeHTTP(w, r)
}

func main() {
	http.HandleFunc("/metrics", metricsHandler)
	fmt.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Printf("Server failed to start: %v\n", err)
	}
}
