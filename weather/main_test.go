package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func quietLogs(t *testing.T) {
	t.Helper()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
}

// openMeteoPayload builds a response shaped like the real API: a "current"
// block, an "hourly" block with `hours` entries per field and a "daily"
// block with `days` entries per field, plus the matching "time" arrays.
func openMeteoPayload(hours, days int, fields []string) map[string]interface{} {
	start := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	current := map[string]interface{}{"time": "2026-05-12T15:00", "interval": 900.0}
	hourly := map[string]interface{}{}
	daily := map[string]interface{}{}

	hTimes := make([]interface{}, hours)
	for i := range hTimes {
		hTimes[i] = start.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:04")
	}
	dTimes := make([]interface{}, days)
	for i := range dTimes {
		dTimes[i] = start.AddDate(0, 0, i).Format("2006-01-02")
	}
	hourly["time"], daily["time"] = hTimes, dTimes

	for fi, f := range fields {
		current[f] = float64(fi)
		h := make([]interface{}, hours)
		for i := range h {
			h[i] = float64(i + fi)
		}
		hourly[f] = h
		d := make([]interface{}, days)
		for i := range d {
			d[i] = float64(i * fi)
		}
		daily[f+"_max"] = d
	}
	return map[string]interface{}{"current": current, "hourly": hourly, "daily": daily}
}

// fakeOpenMeteo serves the same payload for both APIs and records the
// last query string per path.
type fakeOpenMeteo struct {
	payload []byte
	status  int
	queries map[string]url.Values
}

func (f *fakeOpenMeteo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.queries[r.URL.Path] = r.URL.Query()
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write(f.payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(f.payload)
}

func newFake(t *testing.T, payload interface{}) (*fakeOpenMeteo, *connCounter) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOpenMeteo{payload: b, queries: map[string]url.Values{}}
	srv, cc := newCountingServer(t, f)

	oldF, oldA := forecastURL, airQualityURL
	forecastURL, airQualityURL = srv.URL+"/v1/forecast", srv.URL+"/v1/air-quality"
	t.Cleanup(func() {
		forecastURL, airQualityURL = oldF, oldA
		httpClient.CloseIdleConnections()
	})
	return f, cc
}

func TestSanitize(t *testing.T) {
	if got := sanitize("T.Forecast.PM2.5"); got != "t_forecast_pm2_5" {
		t.Errorf("sanitize = %q, want t_forecast_pm2_5", got)
	}
}

func TestParseOpenMeteoTime(t *testing.T) {
	old := configuredLocation
	configuredLocation = time.UTC
	t.Cleanup(func() { configuredLocation = old })

	if got, ok := parseOpenMeteoTime("2026-05-12T15:04"); !ok || got.Unix() != time.Date(2026, 5, 12, 15, 4, 0, 0, time.UTC).Unix() {
		t.Errorf("hour format: got %v %v", got, ok)
	}
	if got, ok := parseOpenMeteoTime("2026-05-12"); !ok || got.Unix() != time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC).Unix() {
		t.Errorf("date format: got %v %v", got, ok)
	}
	for _, bad := range []string{"", "yesterday", "12/05/2026"} {
		if _, ok := parseOpenMeteoTime(bad); ok {
			t.Errorf("parseOpenMeteoTime(%q) should fail", bad)
		}
	}
}

func TestParseOpenMeteoTimeUsesConfiguredZone(t *testing.T) {
	sofia, err := time.LoadLocation("Europe/Sofia")
	if err != nil {
		t.Fatal(err)
	}
	old := configuredLocation
	configuredLocation = sofia
	t.Cleanup(func() { configuredLocation = old })

	got, _ := parseOpenMeteoTime("2026-01-01T12:00")
	if want := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("12:00 Sofia = %v, want %v UTC", got.UTC(), want)
	}
}

func TestProcessCurrentBlock(t *testing.T) {
	processCurrentBlock(map[string]interface{}{"current": map[string]interface{}{
		"unit_temp":  21.5,
		"unit_label": "not a time", // ignored
		"time":       "2026-05-12T15:00",
	}}, "ut", "Test")

	if got := testutil.ToFloat64(scalarMetrics["ut_unit_temp"]); got != 21.5 {
		t.Errorf("ut_unit_temp = %v, want 21.5", got)
	}
	if _, ok := scalarMetrics["ut_unit_label"]; ok {
		t.Error("non-time strings must not become metrics")
	}
	want, _ := parseOpenMeteoTime("2026-05-12T15:00")
	if got := testutil.ToFloat64(scalarMetrics["ut_time_seconds"]); got != float64(want.Unix()) {
		t.Errorf("ut_time_seconds = %v, want %v", got, want.Unix())
	}

	// Missing or malformed block: nothing happens, nothing panics.
	processCurrentBlock(map[string]interface{}{}, "ut", "Test")
	processCurrentBlock(map[string]interface{}{"current": "oops"}, "ut", "Test")
}

func TestProcessArrayBlockRespectsLimit(t *testing.T) {
	arr := make([]interface{}, 100)
	for i := range arr {
		arr[i] = float64(i)
	}
	processArrayBlock(map[string]interface{}{"hourly": map[string]interface{}{"unit_lim": arr}},
		"hourly", "ua", "hour_offset", "Test", 48)

	vec := labeledMetrics["ua_unit_lim"]
	if n := testutil.CollectAndCount(vec); n != 48 {
		t.Errorf("series = %d, want 48 (limit)", n)
	}
	if got := testutil.ToFloat64(vec.WithLabelValues("47")); got != 47 {
		t.Errorf("hour_offset=47 = %v, want 47", got)
	}
}

func TestProcessArrayBlockDropsStaleAndNullEntries(t *testing.T) {
	long := make([]interface{}, 20)
	for i := range long {
		long[i] = 1.0
	}
	block := func(a []interface{}) map[string]interface{} {
		return map[string]interface{}{"daily": map[string]interface{}{"unit_stale": a, "unit_scalar": 3.0}}
	}
	processArrayBlock(block(long), "daily", "ud", "day_offset", "Test", 48)
	processArrayBlock(block([]interface{}{1.0, nil, 2.0}), "daily", "ud", "day_offset", "Test", 48)

	vec := labeledMetrics["ud_unit_stale"]
	if n := testutil.CollectAndCount(vec); n != 2 {
		t.Errorf("series = %d after the array shrank to 3 with one null, want 2", n)
	}
	if _, ok := labeledMetrics["ud_unit_scalar"]; ok {
		t.Error("non-array fields in an array block must be skipped")
	}
}

func TestProcessArrayBlockTimeStrings(t *testing.T) {
	processArrayBlock(map[string]interface{}{"hourly": map[string]interface{}{
		"unit_when": []interface{}{"2026-05-12T00:00", "2026-05-12T01:00"},
	}}, "hourly", "uw", "hour_offset", "Test", 48)

	vec := labeledMetrics["uw_unit_when_seconds"]
	if vec == nil {
		t.Fatal("expected uw_unit_when_seconds to be created")
	}
	want, _ := parseOpenMeteoTime("2026-05-12T01:00")
	if got := testutil.ToFloat64(vec.WithLabelValues("1")); got != float64(want.Unix()) {
		t.Errorf("hour_offset=1 = %v, want %v", got, want.Unix())
	}
}

func TestFetchJSON(t *testing.T) {
	quietLogs(t)
	f, _ := newFake(t, map[string]interface{}{"ok": true})

	data, err := fetchJSON("test", forecastURL)
	if err != nil || data["ok"] != true {
		t.Fatalf("fetchJSON = %v, %v", data, err)
	}

	f.status, f.payload = http.StatusServiceUnavailable, []byte("maintenance")
	if _, err := fetchJSON("test", forecastURL); err == nil || !strings.Contains(err.Error(), "HTTP 503: maintenance") {
		t.Errorf("err = %v, want HTTP 503 with body snippet", err)
	}

	f.status, f.payload = 0, []byte("{not json")
	if _, err := fetchJSON("test", forecastURL); err == nil {
		t.Error("expected a decode error for invalid JSON")
	}
}

func TestGetWeatherDataEndToEnd(t *testing.T) {
	quietLogs(t)
	f, _ := newFake(t, openMeteoPayload(168, 7, []string{"temperature_2m", "relative_humidity_2m"}))

	getWeatherData()

	q := f.queries["/v1/forecast"]
	if q.Get("latitude") != latitude || q.Get("longitude") != longitude || q.Get("timezone") != timezone {
		t.Errorf("location params = %v", q)
	}
	if q.Get("forecast_days") != "7" || !strings.Contains(q.Get("hourly"), "temperature_2m") {
		t.Errorf("forecast params = %v", q)
	}
	if got := testutil.ToFloat64(scalarMetrics["t_relative_humidity_2m"]); got != 1 {
		t.Errorf("t_relative_humidity_2m = %v, want 1", got)
	}
	if n := testutil.CollectAndCount(labeledMetrics["t_forecast_temperature_2m"]); n != forecastHours {
		t.Errorf("t_forecast_temperature_2m series = %d, want %d", n, forecastHours)
	}
	if n := testutil.CollectAndCount(labeledMetrics["t_daily_temperature_2m_max"]); n != forecastDays {
		t.Errorf("t_daily_temperature_2m_max series = %d, want %d", n, forecastDays)
	}
}

func TestGetAirQualityDataEndToEnd(t *testing.T) {
	quietLogs(t)
	f, _ := newFake(t, openMeteoPayload(96, 0, []string{"pm10"}))

	getAirQualityData()

	if q := f.queries["/v1/air-quality"]; q.Get("forecast_days") != "2" {
		t.Errorf("air-quality forecast_days = %q, want 2 (48h)", q.Get("forecast_days"))
	}
	if got := testutil.ToFloat64(scalarMetrics["a_pm10"]); got != 0 {
		t.Errorf("a_pm10 = %v, want 0", got)
	}
	if n := testutil.CollectAndCount(labeledMetrics["a_forecast_pm10"]); n != forecastHours {
		t.Errorf("a_forecast_pm10 series = %d, want %d", n, forecastHours)
	}
}

func TestUpstreamFailureKeepsLastValues(t *testing.T) {
	quietLogs(t)
	f, _ := newFake(t, openMeteoPayload(48, 7, []string{"keep_me"}))
	getWeatherData()
	before := testutil.ToFloat64(scalarMetrics["t_keep_me"])

	f.status, f.payload = http.StatusInternalServerError, []byte("boom")
	getWeatherData() // must log and return, not panic or wipe metrics

	if got := testutil.ToFloat64(scalarMetrics["t_keep_me"]); got != before {
		t.Errorf("t_keep_me changed from %v to %v after a failed fetch", before, got)
	}
}

// metricSeries counts every series this exporter serves (t_* and a_*).
func metricSeries(t *testing.T) int {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, mf := range mfs {
		if name := mf.GetName(); strings.HasPrefix(name, "t_") || strings.HasPrefix(name, "a_") {
			n += len(mf.GetMetric())
		}
	}
	return n
}

func jsonMarshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
