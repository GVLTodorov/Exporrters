package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type AggregatesResponse struct {
	Status       string `json:"status"`
	Ticker       string `json:"ticker"`
	ResultsCount int    `json:"resultsCount"`
	Results      []struct {
		Close float64 `json:"c"`
	} `json:"results"`
}


var priceGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "stock_price",
		Help: "Current stock prices from Polygon.io",
	},
	[]string{"symbol"},
)

type priceCache struct {
	prices map[string]float64
	mu     sync.RWMutex
	ttl    time.Duration
	lastUpdate time.Time
}

var cache = &priceCache{
	prices: make(map[string]float64),
	ttl:    2 * time.Minute, // Cache prices for 2 minutes to reduce API calls
}

// Rate limiter to enforce 5 requests per minute (free tier limit)
type rateLimiter struct {
	requests []time.Time
	mu       sync.Mutex
	maxRequests int
	window      time.Duration
}

var limiter = &rateLimiter{
	maxRequests: 5,              // Free tier: 5 requests per minute
	window:      1 * time.Minute,
}

func (rl *rateLimiter) waitIfNeeded() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	// Remove requests older than the window
	cutoff := now.Add(-rl.window)
	validRequests := []time.Time{}
	for _, req := range rl.requests {
		if req.After(cutoff) {
			validRequests = append(validRequests, req)
		}
	}
	rl.requests = validRequests
	
	// If we're at the limit, wait until the oldest request expires
	if len(rl.requests) >= rl.maxRequests {
		oldest := rl.requests[0]
		waitTime := rl.window - now.Sub(oldest) + 100*time.Millisecond // Add small buffer
		if waitTime > 0 {
			fmt.Printf("Rate limit: waiting %.1f seconds before next request\n", waitTime.Seconds())
			rl.mu.Unlock()
			time.Sleep(waitTime)
			rl.mu.Lock()
		}
		// Clean up again after waiting
		now = time.Now()
		cutoff = now.Add(-rl.window)
		validRequests = []time.Time{}
		for _, req := range rl.requests {
			if req.After(cutoff) {
				validRequests = append(validRequests, req)
			}
		}
		rl.requests = validRequests
	}
	
	// Record this request
	rl.requests = append(rl.requests, time.Now())
}

func init() {
	prometheus.MustRegister(priceGauge)
}

func getSymbols() ([]string, error) {
	symbolsEnv := os.Getenv("SYMBOLS")
	if symbolsEnv == "" {
		return nil, fmt.Errorf("SYMBOLS environment variable is required")
	}
	symbols := strings.Split(symbolsEnv, ",")
	// Trim whitespace from each symbol
	for i := range symbols {
		symbols[i] = strings.TrimSpace(symbols[i])
	}
	return symbols, nil
}

func getAPIKey() (string, error) {
	apiKey := os.Getenv("POLYGON_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("POLYGON_API_KEY environment variable is required")
	}
	return apiKey, nil
}

func fetchPrice(symbol string, apiKey string) (float64, error) {
	// Enforce rate limit before making request
	limiter.waitIfNeeded()
	
	apiUrl := fmt.Sprintf("https://api.polygon.io/v2/aggs/ticker/%s/prev?apikey=%s", symbol, apiKey)

	resp, err := http.Get(apiUrl)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return 0, fmt.Errorf("rate limit exceeded: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %s, body: %s", resp.Status, string(body))
	}

	var aggResponse AggregatesResponse
	if err := json.Unmarshal(body, &aggResponse); err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %v, body: %s", err, string(body))
	}

	if aggResponse.Status != "OK" {
		return 0, fmt.Errorf("API returned non-OK status: %s, body: %s", aggResponse.Status, string(body))
	}

	if len(aggResponse.Results) == 0 {
		return 0, fmt.Errorf("no results found for symbol %s", symbol)
	}

	price := aggResponse.Results[0].Close
	fmt.Printf("[API] Fetched price for %s: $%.2f\n", symbol, price)
	return price, nil
}

func (c *priceCache) get(symbol string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	price, ok := c.prices[symbol]
	return price, ok
}

func (c *priceCache) set(symbol string, price float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prices[symbol] = price
	c.lastUpdate = time.Now()
}

func (c *priceCache) isExpired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.lastUpdate) > c.ttl
}

func (c *priceCache) getAll() map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]float64)
	for k, v := range c.prices {
		result[k] = v
	}
	return result
}

func fetchPrices(symbols []string, apiKey string) map[string]float64 {
	prices := make(map[string]float64)
	
	// Rate limiter handles spacing between requests automatically
	for _, symbol := range symbols {
		price, err := fetchPrice(symbol, apiKey)
		if err != nil {
			// If rate limited, try to get from cache
			if strings.Contains(err.Error(), "rate limit") {
				if cachedPrice, ok := cache.get(symbol); ok {
					fmt.Printf("Rate limited for %s, using cached price: %.2f\n", symbol, cachedPrice)
					prices[symbol] = cachedPrice
					continue
				}
			}
			fmt.Printf("Error fetching price for %s: %v\n", symbol, err)
			// Try to use cached value if available
			if cachedPrice, ok := cache.get(symbol); ok {
				prices[symbol] = cachedPrice
			}
			continue
		}
		prices[symbol] = price
		cache.set(symbol, price)
		fmt.Printf("[CACHE] Updated cache for %s: $%.2f\n", symbol, price)
	}
	return prices
}

func refreshPrices(symbols []string, apiKey string) {
	for {
		// Wait for cache to expire before refreshing
		// With 3 symbols and 5 req/min limit, we can refresh every 2 minutes safely
		time.Sleep(cache.ttl)
		
		fmt.Println("Refreshing prices...")
		prices := fetchPrices(symbols, apiKey)
		updateMetrics(prices)
		fmt.Printf("Prices refreshed. Next refresh in %.0f minutes\n", cache.ttl.Minutes())
	}
}

func updateMetrics(prices map[string]float64) {
	for symbol, price := range prices {
		priceGauge.WithLabelValues(symbol).Set(price)
	}
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	// Use cached prices if available and not expired
	if !cache.isExpired() {
		prices := cache.getAll()
		fmt.Printf("[CACHE] Serving metrics from cache (%d symbols)\n", len(prices))
		updateMetrics(prices)
		promhttp.Handler().ServeHTTP(w, r)
		return
	}

	// Cache expired, try to refresh (but don't block if rate limited)
	symbols, err := getSymbols()
	if err != nil {
		// If we can't get symbols, use cached values
		prices := cache.getAll()
		updateMetrics(prices)
		promhttp.Handler().ServeHTTP(w, r)
		return
	}

	apiKey, err := getAPIKey()
	if err != nil {
		// If we can't get API key, use cached values
		prices := cache.getAll()
		updateMetrics(prices)
		promhttp.Handler().ServeHTTP(w, r)
		return
	}

	// Try to fetch new prices (will use cache if rate limited)
	prices := fetchPrices(symbols, apiKey)
	updateMetrics(prices)
	promhttp.Handler().ServeHTTP(w, r)
}

func main() {
	// Validate environment variables at startup
	symbols, err := getSymbols()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	apiKey, err := getAPIKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Starting server on :8080\n")
	if len(apiKey) > 12 {
		fmt.Printf("API Key: %s...%s\n", apiKey[:8], apiKey[len(apiKey)-4:])
	} else {
		fmt.Printf("API Key: %s***\n", apiKey[:min(4, len(apiKey))])
	}
	fmt.Printf("Symbols: %v\n", symbols)

	// Initial price fetch
	fmt.Println("Fetching initial prices...")
	initialPrices := fetchPrices(symbols, apiKey)
	updateMetrics(initialPrices)

	// Start background goroutine to refresh prices periodically
	go refreshPrices(symbols, apiKey)

	http.HandleFunc("/metrics", metricsHandler)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Printf("Server failed to start: %v\n", err)
	}
}

