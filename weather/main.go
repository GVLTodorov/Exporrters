package main

import (
	"os"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// API parameters
var (
	latitude string
	longitude string
	timezone string
	weatherFields string
	airQualityFields string
)

type MetricMap map[string]prometheus.Gauge

var weatherMetrics = make(MetricMap)
var airQualityMetrics = make(MetricMap)

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

func fetchWeatherData() {
	for {
		apiURL := fmt.Sprintf(
			"https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current=%s&timezone=%s",
			latitude, longitude, weatherFields, timezone)

		resp, err := http.Get(apiURL)
		if err != nil {
			log.Printf("Error fetching weather data: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response: %v", err)
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			log.Printf("Error parsing JSON: %v", err)
			continue
		}

		if current, exists := data["current"].(map[string]interface{}); exists {
			for key, value := range current {
				if numValue, ok := value.(float64); ok {
					createOrUpdateMetric(weatherMetrics, "T", key, numValue, fmt.Sprintf("Current %s from Open-Meteo", key))
				}
			}
		}

		log.Println("Weather metrics updated.")
		time.Sleep(30 * time.Second) // Update every 30 seconds
	}
}

func fetchAirQualityData() {
	for {
		apiURL := fmt.Sprintf(
			"https://air-quality-api.open-meteo.com/v1/air-quality?latitude=%s&longitude=%s&current=%s&timezone=%s",
			latitude, longitude, airQualityFields, timezone)

		resp, err := http.Get(apiURL)
		if err != nil {
			log.Printf("Error fetching air quality data: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response: %v", err)
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			log.Printf("Error parsing JSON: %v", err)
			continue
		}

		if current, exists := data["current"].(map[string]interface{}); exists {
			for key, value := range current {
				if numValue, ok := value.(float64); ok {
					createOrUpdateMetric(airQualityMetrics, "A", key, numValue, fmt.Sprintf("Current %s from Open-Meteo Air Quality API", key))
				}
			}
		}

		log.Println("Air quality metrics updated.")
		time.Sleep(30 * time.Second) // Update every 30 seconds
	}
}

func init() {
	// Read environment variables
	weatherFields = os.Getenv("WEATHER_FIELDS")
	if weatherFields == "" {
		weatherFields = "temperature_2m,apparent_temperature,relative_humidity_2m"
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

	fmt.Println("Weather Fields:", weatherFields)
	fmt.Println("Air Quality Fields:", airQualityFields)
	fmt.Println("Latitude:", latitude)
	fmt.Println("Longitude:", longitude)
	fmt.Println("Timezone:", timezone)
}

func main() {
	// Start fetching data in separate goroutines
	go fetchWeatherData()
	go fetchAirQualityData()

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":8080", nil))
}
