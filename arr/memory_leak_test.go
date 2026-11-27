package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestLeakClientReusesConnections: if a response body is not drained and
// closed, net/http cannot reuse the connection and every request dials a
// new socket. Success, error and undecodable replies are all covered.
func TestLeakClientReusesConnections(t *testing.T) {
	var i atomic.Int64
	c, cc := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch i.Add(1) % 3 {
		case 0:
			_, _ = io.WriteString(w, `{"version":"1"}`)
		case 1:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, strings.Repeat("e", 4<<10))
		case 2:
			_, _ = io.WriteString(w, `{"version": broken`+strings.Repeat(" ", 4<<10))
		}
	}), "")

	var out struct{ Version string }
	for n := 0; n < 300; n++ {
		_ = c.get(t.Context(), "/system/status", nil, &out)
	}
	if opened, _ := cc.counts(); opened > 2 {
		t.Errorf("300 requests opened %d connections, want <= 2 (bodies are not being drained/closed)", opened)
	}
}

// TestLeakClientCapsResponseBody: a misbehaving proxy returning a huge body
// must not be read into memory in full.
func TestLeakClientCapsResponseBody(t *testing.T) {
	old := maxBodyBytes
	maxBodyBytes = 1 << 20
	t.Cleanup(func() { maxBodyBytes = old })

	const huge = 64 << 20
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `["`)
		chunk := strings.Repeat("x", 32<<10)
		for sent := 0; sent < huge; sent += len(chunk) {
			if _, err := io.WriteString(w, chunk); err != nil {
				return // client hung up, as it should
			}
		}
	}), "")

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var out []string
	err := c.get(t.Context(), "/movie", nil, &out)
	runtime.ReadMemStats(&after)

	if err == nil || !strings.Contains(err.Error(), "response larger than") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Errorf("one oversized response allocated %d MiB, want < 16 MiB (body is not capped)", allocated>>20)
	}
}

// TestLeakRefreshCyclesKeepHeapAndSeriesStable runs many refresh + scrape
// cycles for all four apps while the queue and health contents keep
// changing, and checks that nothing is retained per cycle and that old
// label sets do not pile up as stale series.
func TestLeakRefreshCyclesKeepHeapAndSeriesStable(t *testing.T) {
	f, base := allAppsServer(t)
	exp := newTestExporter(t, allAppsEnv(base))

	cycle := 0
	run := func() {
		cycle++
		// A different download title/state and health message every cycle.
		f.set("/sonarr/api/v3/queue", fmt.Sprintf(`{"totalRecords":1,"records":[{"size":1,"sizeleft":1,"status":"downloading","trackedDownloadStatus":"ok","trackedDownloadState":"state-%d"}]}`, cycle))
		f.set("/radarr/api/v3/health", fmt.Sprintf(`[{"source":"S","type":"warning","message":"msg %d"}]`, cycle))
		for _, tg := range exp.targets {
			tg.refresh(context.Background())
		}
		_ = gather(t, exp)
	}
	assertNoHeapGrowth(t, 30, 500, 1<<20, run)

	got := gather(t, exp)
	if n := countPrefix(got, "arr_queue_items"); n != 1 {
		t.Errorf("arr_queue_items has %d series after %d cycles, want 1", n, cycle)
	}
	// Distinct issues: sonarr 2, prowlarr 1, bazarr 2, radarr 1 (the one
	// that changes every cycle).
	if n := countPrefix(got, "arr_health_issue"); n != 6 {
		t.Errorf("arr_health_issue has %d series after %d cycles, want 6", n, cycle)
	}
}

// TestLeakHungUpstreamDoesNotPinGoroutines: with the client timeout a stuck
// app releases its request goroutines (goleak in TestMain verifies nothing
// is left once the test finishes).
func TestLeakHungUpstreamDoesNotPinGoroutines(t *testing.T) {
	srv, _ := newCountingServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	e := map[string]string{"SONARR_URL": srv.URL, "SONARR_API_KEY": testKey, "REQUEST_TIMEOUT": "50ms"}
	exp := newTestExporter(t, e)

	base := runtime.NumGoroutine()
	start := time.Now()
	exp.targets[0].refresh(t.Context())
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("refresh against a hung app took %s; the timeout is not applied", took)
	}
	expect(t, gather(t, exp), map[string]float64{`arr_up{app="sonarr"}`: 0, `arr_refresh_errors{app="sonarr"}`: 7})

	exp.targets[0].client.http.CloseIdleConnections()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if g := runtime.NumGoroutine(); g > base+2 {
		t.Errorf("goroutines grew from %d to %d after timed-out requests", base, g)
	}
}

// TestLeakRunStopsEveryLoop: cancelling run stops every per-app refresh
// goroutine (goleak catches any left behind).
func TestLeakRunStopsEveryLoop(t *testing.T) {
	_, base := allAppsServer(t)
	exp := newTestExporter(t, allAppsEnv(base))
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		exp.run(ctx, time.Millisecond)
		cancel()
	}
}
