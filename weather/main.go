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

	// Embed the IANA timezone database so time.LoadLocation works on minimal
	// base images (Alpine, scratch, distroless) that don't ship zoneinfo.
	_ "time/tzdata"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// =============================================================================
// Hard-coded Open-Meteo field sets. We always request the full superset; the
// API silently drops fields not supported at the requested location.
// =============================================================================

const (
	weatherCurrentFields = "" +
		"temperature_2m,relative_humidity_2m,dew_point_2m,apparent_temperature," +
		"precipitation,rain,showers,snowfall,weather_code," +
		"cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high," +
		"pressure_msl,surface_pressure," +
		"wind_speed_10m,wind_direction_10m,wind_gusts_10m," +
		"is_day"

	weatherHourlyFields = "" +
		"temperature_2m,relative_humidity_2m,dew_point_2m,apparent_temperature," +
		"precipitation_probability,precipitation,rain,showers,snowfall,snow_depth," +
		"weather_code,pressure_msl,surface_pressure," +
		"cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high," +
		"visibility,evapotranspiration,et0_fao_evapotranspiration,vapour_pressure_deficit," +
		"wind_speed_10m,wind_speed_80m,wind_speed_120m,wind_speed_180m," +
		"wind_direction_10m,wind_direction_80m,wind_direction_120m,wind_direction_180m," +
		"wind_gusts_10m,uv_index,uv_index_clear_sky,sunshine_duration,is_day," +
		"freezing_level_height,cape,lifted_index,convective_inhibition," +
		"shortwave_radiation,direct_radiation,diffuse_radiation," +
		"direct_normal_irradiance,terrestrial_radiation," +
		"soil_temperature_0cm,soil_temperature_6cm,soil_temperature_18cm,soil_temperature_54cm," +
		"soil_moisture_0_to_1cm,soil_moisture_1_to_3cm,soil_moisture_3_to_9cm," +
		"soil_moisture_9_to_27cm,soil_moisture_27_to_81cm"

	weatherDailyFields = "" +
		"weather_code," +
		"temperature_2m_max,temperature_2m_min," +
		"apparent_temperature_max,apparent_temperature_min," +
		"sunrise,sunset,daylight_duration,sunshine_duration," +
		"uv_index_max,uv_index_clear_sky_max," +
		"precipitation_sum,rain_sum,showers_sum,snowfall_sum," +
		"precipitation_hours,precipitation_probability_max," +
		"wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant," +
		"shortwave_radiation_sum,et0_fao_evapotranspiration"

	airQualityFields = "" +
		"pm10,pm2_5,carbon_monoxide,nitrogen_dioxide,sulphur_dioxide,ozone," +
		"aerosol_optical_depth,dust,ammonia," +
		"alder_pollen,birch_pollen,grass_pollen,mugwort_pollen,olive_pollen,ragweed_pollen," +
		"european_aqi,european_aqi_pm2_5,european_aqi_pm10," +
		"european_aqi_nitrogen_dioxide,european_aqi_ozone,european_aqi_sulphur_dioxide," +
		"us_aqi,us_aqi_pm2_5,us_aqi_pm10," +
		"us_aqi_nitrogen_dioxide,us_aqi_carbon_monoxide,us_aqi_ozone,us_aqi_sulphur_dioxide," +
		"uv_index,uv_index_clear_sky"

	forecastHours = 48 // hourly horizon (bounded label cardinality)
	forecastDays  = 7  // daily horizon
)

// =============================================================================
// Config + shared state
// =============================================================================

var (
	latitude  string
	longitude string
	timezone  string

	// Resolved once at startup so we never re-parse the embedded tz database.
	configuredLocation = time.UTC

	// Shared HTTP client with a timeout to prevent goroutine / socket pile-up.
	httpClient = &http.Client{Timeout: 15 * time.Second}
)

// Upper bound for an upstream response body so a runaway proxy / DNS detour /
// captive portal can never blow up RAM. Real Open-Meteo bodies are ~50–100 KB.
const maxBodyBytes = 1 << 20

// =============================================================================
// Metric registries. Bounded by the (fixed) field lists above, and for labeled
// metrics by forecastHours / forecastDays. Reset before each scrape so a
// missing or null entry disappears instead of leaving stale data behind.
// =============================================================================

type ScalarMetricMap map[string]prometheus.Gauge
type LabeledMetricMap map[string]*prometheus.GaugeVec

var (
	scalarMetrics  = make(ScalarMetricMap)
	labeledMetrics = make(LabeledMetricMap)
)

func sanitize(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, ".", "_"))
}

func setScalarMetric(name string, value float64, help string) {
	metric := sanitize(name)
	if g, ok := scalarMetrics[metric]; ok {
		g.Set(value)
		return
	}
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: metric, Help: help})
	g.Set(value)
	prometheus.MustRegister(g)
	scalarMetrics[metric] = g
}

func setLabeledMetric(name, labelName string, index int, value float64, help string) {
	metric := sanitize(name)
	vec, ok := labeledMetrics[metric]
	if !ok {
		vec = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Name: metric, Help: help},
			[]string{labelName},
		)
		prometheus.MustRegister(vec)
		labeledMetrics[metric] = vec
	}
	vec.WithLabelValues(strconv.Itoa(index)).Set(value)
}

// resetLabeledMetric clears stale label values for a metric so that any
// index the API doesn't provide this scrape disappears from /metrics
// instead of keeping the previous scrape's value forever.
func resetLabeledMetric(name string) {
	metric := sanitize(name)
	if vec, ok := labeledMetrics[metric]; ok {
		vec.Reset()
	}
}

// parseOpenMeteoTime parses both the date-only ("2026-05-12") and
// date-with-hour ("2026-05-12T15:04") formats Open-Meteo emits.
func parseOpenMeteoTime(s string) (time.Time, bool) {
	if t, err := time.ParseInLocation("2006-01-02T15:04", s, configuredLocation); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("2006-01-02", s, configuredLocation); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// =============================================================================
// Response processing
// =============================================================================

// processCurrentBlock emits one gauge per numeric field in data["current"].
// String fields that look like ISO timestamps are emitted as "<name>_seconds".
func processCurrentBlock(data map[string]interface{}, prefix, source string) {
	block, ok := data["current"].(map[string]interface{})
	if !ok {
		return
	}
	for key, value := range block {
		switch v := value.(type) {
		case float64:
			setScalarMetric(
				prefix+"_"+key, v,
				fmt.Sprintf("Current %s from Open-Meteo %s", key, source),
			)
		case string:
			if t, ok := parseOpenMeteoTime(v); ok {
				setScalarMetric(
					prefix+"_"+key+"_seconds", float64(t.Unix()),
					fmt.Sprintf("Current %s (unix seconds) from Open-Meteo %s", key, source),
				)
			}
		}
	}
}

// processArrayBlock walks data[blockKey] (e.g. "hourly" or "daily") and emits
// one GaugeVec per field, labeled by `labelName` ("hour_offset" / "day_offset").
// Numeric entries go to "<prefix>_<field>"; ISO-time strings go to
// "<prefix>_<field>_seconds". Up to `limit` entries are emitted per field.
func processArrayBlock(data map[string]interface{}, blockKey, prefix, labelName, source string, limit int) {
	block, ok := data[blockKey].(map[string]interface{})
	if !ok {
		return
	}
	for field, raw := range block {
		arr, isArr := raw.([]interface{})
		if !isArr {
			continue
		}

		metric := prefix + "_" + field
		secondsMetric := metric + "_seconds"

		// Clear both potential targets so a value that disappears this
		// scrape (null entry, shorter array, field dropped entirely) is
		// reflected as a gap rather than a stale carry-over.
		resetLabeledMetric(metric)
		resetLabeledMetric(secondsMetric)

		help := fmt.Sprintf("%s %s from Open-Meteo %s", strings.Title(blockKey), field, source)
		secondsHelp := fmt.Sprintf("%s %s (unix seconds) from Open-Meteo %s", strings.Title(blockKey), field, source)

		end := limit
		if len(arr) < end {
			end = len(arr)
		}
		for i := 0; i < end; i++ {
			switch v := arr[i].(type) {
			case float64:
				setLabeledMetric(metric, labelName, i, v, help)
			case string:
				if t, ok := parseOpenMeteoTime(v); ok {
					setLabeledMetric(secondsMetric, labelName, i, float64(t.Unix()), secondsHelp)
				}
			}
		}
	}
}

// fetchJSON performs a GET and decodes JSON. label identifies the request in logs (e.g. Docker).
func fetchJSON(label, url string) (map[string]interface{}, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	log.Printf("%s: HTTP %d %s", label, resp.StatusCode, resp.Status)

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	limited := io.LimitReader(resp.Body, maxBodyBytes)
	var data map[string]interface{}
	if err := json.NewDecoder(limited).Decode(&data); err != nil {
		return nil, err
	}
	// Drain up to the same byte cap so the transport can reuse the connection safely.
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return nil, err
	}
	return data, nil
}

// =============================================================================
// Fetchers
// =============================================================================

func getWeatherData() {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast"+
			"?latitude=%s&longitude=%s&timezone=%s"+
			"&current=%s&hourly=%s&daily=%s"+
			"&forecast_days=%d",
		latitude, longitude, timezone,
		weatherCurrentFields, weatherHourlyFields, weatherDailyFields,
		forecastDays,
	)
	data, err := fetchJSON("forecast", url)
	if err != nil {
		log.Printf("Error fetching weather data: %v", err)
		return
	}
	processCurrentBlock(data, "t", "Forecast API")
	processArrayBlock(data, "hourly", "t_forecast", "hour_offset", "Forecast API", forecastHours)
	processArrayBlock(data, "daily", "t_daily", "day_offset", "Forecast API", forecastDays)
	log.Println("Weather metrics updated.")
}

func getAirQualityData() {
	url := fmt.Sprintf(
		"https://air-quality-api.open-meteo.com/v1/air-quality"+
			"?latitude=%s&longitude=%s&timezone=%s"+
			"&current=%s&hourly=%s"+
			"&forecast_days=%d",
		latitude, longitude, timezone,
		airQualityFields, airQualityFields,
		(forecastHours+23)/24,
	)
	data, err := fetchJSON("air-quality", url)
	if err != nil {
		log.Printf("Error fetching air quality data: %v", err)
		return
	}
	processCurrentBlock(data, "a", "Air Quality API")
	processArrayBlock(data, "hourly", "a_forecast", "hour_offset", "Air Quality API", forecastHours)
	log.Println("Air quality metrics updated.")
}

// =============================================================================
// Bootstrap
// =============================================================================

func init() {
	latitude = os.Getenv("LATITUDE")
	if latitude == "" {
		latitude = "42.6975"
	}
	longitude = os.Getenv("LONGITUDE")
	if longitude == "" {
		longitude = "23.3241"
	}
	timezone = os.Getenv("TIMEZONE")
	if timezone == "" {
		timezone = "Europe/Sofia"
	}
	if loc, err := time.LoadLocation(timezone); err == nil {
		configuredLocation = loc
	} else {
		log.Printf("Could not load TIMEZONE=%q: %v (timestamps will use UTC)", timezone, err)
	}

	fmt.Println("Latitude:", latitude)
	fmt.Println("Longitude:", longitude)
	fmt.Println("Timezone:", timezone)
	fmt.Println("Forecast Hours:", forecastHours)
	fmt.Println("Forecast Days:", forecastDays)
}

func main() {
	fetchEvery := 10 * time.Minute
	if s := os.Getenv("FETCH_INTERVAL"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			log.Printf("Invalid FETCH_INTERVAL=%q (%v); using default %v", s, err, fetchEvery)
		} else {
			fetchEvery = d
		}
	}
	log.Printf("Refreshing Open-Meteo data every %v", fetchEvery)

	go func() {
		refresh := func() {
			log.Println("Fetching weather and air-quality data...")
			getWeatherData()
			getAirQualityData()
		}
		refresh()
		ticker := time.NewTicker(fetchEvery)
		defer ticker.Stop()
		for range ticker.C {
			refresh()
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":8080", nil))
}
