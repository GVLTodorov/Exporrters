package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	// Embed the IANA timezone database in the binary so time.LoadLocation
	// works on minimal base images (Alpine, scratch, distroless, …) that
	// do not ship /usr/share/zoneinfo. Costs ~450 KB in the final binary.
	_ "time/tzdata"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// API parameters
var (
	latitude            string
	longitude           string
	timezone            string
	weatherFields       string
	weatherHourlyFields string
	airQualityFields    string
	forecastHours       int
)

type MetricMap map[string]prometheus.Gauge
type ForecastMetricMap map[string]*prometheus.GaugeVec

var weatherMetrics = make(MetricMap)
var airQualityMetrics = make(MetricMap)
var forecastMetrics = make(ForecastMetricMap)

// Shared HTTP client so a slow upstream cannot pile up goroutines / sockets.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// configuredLocation is resolved once at startup so we do not re-parse the
// embedded timezone database on every scrape. Falls back to UTC if the
// TIMEZONE env var is invalid (warned about in init()).
var configuredLocation *time.Location = time.UTC

// maxBodyBytes caps the size of an upstream response we are willing to
// allocate. Open-Meteo responses are well under 100 KB even for a 7-day
// hourly forecast; 1 MiB is a generous safety ceiling that protects us if
// DNS / a captive portal / a misconfigured proxy returns something huge.
const maxBodyBytes = 1 << 20

func createOrUpdateMetric(metricMap MetricMap, prefix, name string, value float64, help string) {
	metricName := fmt.Sprintf("%s_%s", prefix, strings.ReplaceAll(name, ".", "_"))
	metricName = strings.ToLower(metricName)

	if gauge, exists := metricMap[metricName]; exists {
		gauge.Set(value)
	} else {
		newGauge := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: metricName,
			Help: help,
		})
		newGauge.Set(value)
		prometheus.MustRegister(newGauge)
		metricMap[metricName] = newGauge
	}
}

// createOrUpdateForecastMetric registers (once) and updates a GaugeVec with a
// single bounded label `hour_offset`. The set of series is fixed at
// 0..forecastHours-1, so this cannot grow over time.
func createOrUpdateForecastMetric(name string, hourOffset int, value float64, help string) {
	metricName := strings.ToLower(strings.ReplaceAll(name, ".", "_"))
	vec, exists := forecastMetrics[metricName]
	if !exists {
		vec = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Name: metricName, Help: help},
			[]string{"hour_offset"},
		)
		prometheus.MustRegister(vec)
		forecastMetrics[metricName] = vec
	}
	vec.WithLabelValues(strconv.Itoa(hourOffset)).Set(value)
}

// resetForecastMetric clears all label values from the named forecast metric
// if it is already registered. This is called once per scrape before the
// metric is repopulated, so hours that the API returns as null (or omits)
// do not keep showing a stale value from a previous scrape.
func resetForecastMetric(name string) {
	metricName := strings.ToLower(strings.ReplaceAll(name, ".", "_"))
	if vec, exists := forecastMetrics[metricName]; exists {
		vec.Reset()
	}
}

func getWeatherData() {
		// Build URL. Hourly forecast is only requested when configured.
		apiURL := fmt.Sprintf(
			"https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current=%s&timezone=%s",
			latitude, longitude, weatherFields, timezone)
		if weatherHourlyFields != "" && forecastHours > 0 {
			days := (forecastHours + 23) / 24
			if days < 1 {
				days = 1
			}
			if days > 7 {
				days = 7
			}
			apiURL += fmt.Sprintf("&hourly=%s&forecast_days=%d", weatherHourlyFields, days)
		}

		resp, err := httpClient.Get(apiURL)
		if err != nil {
			log.Printf("Error fetching weather data: %v", err)
			return
		}
		defer func() {
			// Drain any trailing bytes (json.Decoder may stop mid-stream on
			// error) so the connection can be reused, then close.
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		var data map[string]interface{}
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&data); err != nil {
			log.Printf("Error parsing JSON: %v", err)
			return
		}

		if current, exists := data["current"].(map[string]interface{}); exists {
			for key, value := range current {
				if numValue, ok := value.(float64); ok {
					createOrUpdateMetric(weatherMetrics, "T", key, numValue, fmt.Sprintf("Current %s from Open-Meteo", key))
				}
			}
		}

		if hourly, exists := data["hourly"].(map[string]interface{}); exists && forecastHours > 0 {
			times, _ := hourly["time"].([]interface{})

			// Clear stale hour_offset slots before repopulating so that
			// hours the API omits or returns as null disappear instead of
			// keeping the previous scrape's value.
			resetForecastMetric("t_forecast_time_seconds")
			for i := 0; i < forecastHours && i < len(times); i++ {
				ts, ok := times[i].(string)
				if !ok {
					continue
				}
				t, err := time.ParseInLocation("2006-01-02T15:04", ts, configuredLocation)
				if err != nil {
					continue
				}
				createOrUpdateForecastMetric(
					"t_forecast_time_seconds", i, float64(t.Unix()),
					"Forecast target time (unix seconds) for hour_offset",
				)
			}

			for key, raw := range hourly {
				if key == "time" {
					continue
				}
				arr, ok := raw.([]interface{})
				if !ok {
					continue
				}
				limit := forecastHours
				if len(arr) < limit {
					limit = len(arr)
				}
				metric := "t_forecast_" + key
				help := fmt.Sprintf("Hourly forecast %s from Open-Meteo", key)

				// Reset before repopulating; fields like precipitation_probability
				// occasionally come back with null entries, and the old value would
				// otherwise stay set for that hour_offset.
				resetForecastMetric(metric)

				populated := 0
				for i := 0; i < limit; i++ {
					num, ok := arr[i].(float64)
					if !ok {
						// null / unexpected type for this hour, leave the slot absent
						continue
					}
					createOrUpdateForecastMetric(metric, i, num, help)
					populated++
				}

				if populated == 0 {
					log.Printf("Hourly field %q returned no usable values; metric will be absent this scrape", key)
				}
			}
		}

		log.Println("Weather metrics updated.")
}

func getAirQualityData() {
		apiURL := fmt.Sprintf(
			"https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%s&longitude=%s&current=%s&timezone=%s",
			latitude, longitude, airQualityFields, timezone)

		resp, err := httpClient.Get(apiURL)
		if err != nil {
			log.Printf("Error fetching air quality data: %v", err)
			return
		}
		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		var data map[string]interface{}
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&data); err != nil {
			log.Printf("Error parsing JSON: %v", err)
			return
		}

		if current, exists := data["current"].(map[string]interface{}); exists {
			for key, value := range current {
				if numValue, ok := value.(float64); ok {
					createOrUpdateMetric(airQualityMetrics, "A", key, numValue, fmt.Sprintf("Current %s from Open-Meteo Air Quality API", key))
				}
			}
		}

		log.Println("Air quality metrics updated.")
}

func init() {
	// Read environment variables
	weatherFields = os.Getenv("WEATHER_FIELDS")
	if weatherFields == "" {
		weatherFields = "temperature_2m,apparent_temperature,relative_humidity_2m"
	}

	weatherHourlyFields = os.Getenv("WEATHER_HOURLY_FIELDS")
	if weatherHourlyFields == "" {
		weatherHourlyFields = "temperature_2m,apparent_temperature,precipitation_probability"
	}

	airQualityFields = os.Getenv("AIR_QUALITY_FIELDS")
	if airQualityFields == "" {
		airQualityFields = "european_aqi,us_aqi,pm10,pm2_5"
	}

	latitude = os.Getenv("LATITUDE")
	if latitude == "" {
		latitude = "42.6975" // Default value
	}

	longitude = os.Getenv("LONGITUDE")
	if longitude == "" {
		longitude = "23.3241" // Default value
	}

	timezone = os.Getenv("TIMEZONE")
	if timezone == "" {
		timezone = "Europe/Sofia" // Default value
	}

	// Resolve and cache the timezone once, instead of re-loading it on every
	// scrape from inside getWeatherData().
	if loc, err := time.LoadLocation(timezone); err == nil {
		configuredLocation = loc
	} else {
		log.Printf("Could not load TIMEZONE=%q: %v (forecast timestamps will use UTC)", timezone, err)
	}

	forecastHours = 24
	if v := os.Getenv("FORECAST_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			// Hard cap so a misconfiguration cannot inflate label cardinality.
			if n > 168 {
				n = 168
			}
			forecastHours = n
		} else {
			log.Printf("Invalid FORECAST_HOURS=%q, falling back to %d", v, forecastHours)
		}
	}

	fmt.Println("Weather Fields:", weatherFields)
	fmt.Println("Weather Hourly Fields:", weatherHourlyFields)
	fmt.Println("Forecast Hours:", forecastHours)
	fmt.Println("Air Quality Fields:", airQualityFields)
	fmt.Println("Latitude:", latitude)
	fmt.Println("Longitude:", longitude)
	fmt.Println("Timezone:", timezone)
}

func main() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop() // Stops the ticker when main exits
	
		for {
			select {
			case <-ticker.C:
				fmt.Println("Fetching weather data...")
				getWeatherData()
				getAirQualityData()
			}
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":8080", nil))
}
