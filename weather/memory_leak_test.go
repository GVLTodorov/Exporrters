package main

import (
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// realisticFields mirrors the size of the real requests (dozens of fields,
// 168 hourly entries of which 48 are kept).
var realisticFields = func() []string {
	var out []string
	for _, f := range strings.Split(weatherHourlyFields, ",") {
		out = append(out, "leak_"+f)
	}
	return out
}()

// TestLeakRepeatedRefreshKeepsSeriesAndHeapStable runs the full refresh
// cycle the exporter runs every FETCH_INTERVAL - HTTP fetch, JSON decode,
// gauge updates - many times. The series count must not change after the
// first cycle and the live heap must not grow.
func TestLeakRepeatedRefreshKeepsSeriesAndHeapStable(t *testing.T) {
	quietLogs(t)
	_, cc := newFake(t, openMeteoPayload(168, 7, realisticFields))

	refresh := func() {
		getWeatherData()
		getAirQualityData()
	}
	refresh()
	series := metricSeries(t)

	assertNoHeapGrowth(t, 20, 300, 1<<20, refresh)

	if got := metricSeries(t); got != series {
		t.Errorf("series count changed from %d to %d across refreshes", series, got)
	}
	if opened, _ := cc.counts(); opened > 2 {
		t.Errorf("321 refreshes opened %d connections, want <= 2 (responses not drained/closed)", opened)
	}
}

// TestLeakShrinkingForecastDropsSeries: when the API returns fewer entries
// (or a field disappears) old label values must be removed, so a long
// uptime cannot accumulate stale hour_offset series.
func TestLeakShrinkingForecastDropsSeries(t *testing.T) {
	quietLogs(t)
	f, _ := newFake(t, openMeteoPayload(168, 7, []string{"shrink"}))
	getWeatherData()
	full := metricSeries(t)

	b, _ := jsonMarshal(openMeteoPayload(10, 2, []string{"shrink"}))
	f.payload = b
	for i := 0; i < 50; i++ {
		getWeatherData()
	}
	if got := metricSeries(t); got >= full {
		t.Errorf("series = %d after the forecast shrank, want fewer than %d", got, full)
	}
}

// TestLeakOversizedBodyIsCapped: a captive portal / proxy returning a huge
// body must be cut off at maxBodyBytes instead of being decoded into RAM.
func TestLeakOversizedBodyIsCapped(t *testing.T) {
	quietLogs(t)
	srv, _ := newCountingServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"current":{"x":"`)
		chunk := strings.Repeat("y", 32<<10)
		for sent := 0; sent < 64<<20; sent += len(chunk) {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(httpClient.CloseIdleConnections)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := fetchJSON("test", srv.URL)
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("expected an error for a truncated 64 MiB body")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Errorf("fetchJSON allocated %d MiB for one oversized body, want < 16 MiB", allocated>>20)
	}
}

// TestLeakErrorResponsesDoNotPileUpConnections: non-200 replies are only
// partially read (a 2 KB snippet). Small ones keep the connection
// reusable; large ones force a reconnect - but either way no connection
// may stay open afterwards.
func TestLeakErrorResponsesDoNotPileUpConnections(t *testing.T) {
	quietLogs(t)
	for _, size := range []int{100, 64 << 10} {
		f, cc := newFake(t, nil)
		f.status, f.payload = http.StatusBadGateway, []byte(strings.Repeat("e", size))
		for i := 0; i < 100; i++ {
			if _, err := fetchJSON("test", forecastURL); err == nil {
				t.Fatal("expected an HTTP 502 error")
			}
		}
		httpClient.CloseIdleConnections()

		deadline := time.Now().Add(2 * time.Second)
		for {
			_, active := cc.counts()
			if active == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("body size %d: %d connection(s) still open after 100 error replies", size, active)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if opened, _ := cc.counts(); size == 100 && opened > 2 {
			t.Errorf("small error bodies opened %d connections, want <= 2", opened)
		}
	}
}
