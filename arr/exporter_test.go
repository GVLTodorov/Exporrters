package main

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// allAppsServer serves all four fixtures under their real API roots.
func allAppsServer(t *testing.T) (*fakeAPI, string) {
	t.Helper()
	bodies := map[string]string{}
	for _, part := range []map[string]string{
		prefixed("/sonarr/api/v3", sonarrFixture),
		prefixed("/radarr/api/v3", radarrFixture),
		prefixed("/prowlarr/api/v1", prowlarrFixture),
		prefixed("/bazarr/api", bazarrFixture),
	} {
		for k, v := range part {
			bodies[k] = v
		}
	}
	f := newFakeAPI(bodies)
	srv, _ := newCountingServer(t, f)
	return f, srv.URL
}

func allAppsEnv(base string) map[string]string {
	return map[string]string{
		"SONARR_URL": base + "/sonarr", "SONARR_API_KEY": testKey,
		"RADARR_URL": base + "/radarr", "RADARR_API_KEY": testKey,
		"PROWLARR_URL": base + "/prowlarr", "PROWLARR_API_KEY": testKey,
		"BAZARR_URL": base + "/bazarr", "BAZARR_API_KEY": testKey,
	}
}

func newTestExporter(t *testing.T, e map[string]string) *exporter {
	t.Helper()
	cfg, err := loadConfig(env(e))
	if err != nil {
		t.Fatal(err)
	}
	exp := newExporter(cfg)
	t.Cleanup(func() {
		for _, tg := range exp.targets {
			tg.client.http.CloseIdleConnections()
		}
	})
	return exp
}

func TestExporterServesEveryApp(t *testing.T) {
	_, base := allAppsServer(t)
	exp := newTestExporter(t, allAppsEnv(base))

	// Before the first refresh there is nothing to serve.
	if got := gather(t, exp); len(got) != 0 {
		t.Fatalf("series before first refresh: %v", keys(got))
	}
	for _, tg := range exp.targets {
		tg.refresh(t.Context())
	}
	got := gather(t, exp)
	for _, app := range []string{"sonarr", "radarr", "prowlarr", "bazarr"} {
		expect(t, got, map[string]float64{
			`arr_up{app="` + app + `"}`:             1,
			`arr_refresh_errors{app="` + app + `"}`: 0,
		})
		for _, name := range []string{"arr_refresh_duration_seconds", "arr_last_refresh_timestamp_seconds"} {
			if _, ok := got[name+`{app="`+app+`"}`]; !ok {
				t.Errorf("missing %s for %s", name, app)
			}
		}
	}
	expect(t, got, map[string]float64{
		`sonarr_series`:              3,
		`radarr_movies`:              5,
		`prowlarr_indexers`:          4,
		`bazarr_throttled_providers`: 1,
		`arr_info{app="bazarr",version="1.6.2"}`: 1,
	})
	if ts := got[`arr_last_refresh_timestamp_seconds{app="sonarr"}`]; time.Since(time.Unix(int64(ts), 0)) > time.Minute {
		t.Errorf("last refresh timestamp %v is not recent", ts)
	}
}

func TestExporterReportsDownApp(t *testing.T) {
	f, base := allAppsServer(t)
	f.fail("/radarr/api/v3/system/status", http.StatusServiceUnavailable)
	f.fail("/radarr/api/v3/movie", http.StatusServiceUnavailable)
	exp := newTestExporter(t, map[string]string{"RADARR_URL": base + "/radarr", "RADARR_API_KEY": testKey})

	exp.targets[0].refresh(t.Context())
	got := gather(t, exp)
	expect(t, got, map[string]float64{
		`arr_up{app="radarr"}`:             0,
		`arr_refresh_errors{app="radarr"}`: 2,
		`arr_wanted_missing{app="radarr"}`: 1, // other endpoints still exported
	})
	expectAbsent(t, got, "radarr_movies", "arr_info")
}

func TestExporterWrongAPIKey(t *testing.T) {
	_, base := allAppsServer(t)
	exp := newTestExporter(t, map[string]string{"SONARR_URL": base + "/sonarr", "SONARR_API_KEY": "wrong"})
	exp.targets[0].refresh(t.Context())
	got := gather(t, exp)
	expect(t, got, map[string]float64{`arr_up{app="sonarr"}`: 0, `arr_refresh_errors{app="sonarr"}`: 7})
}

// TestRefreshReplacesSnapshot: series that vanish upstream vanish here.
func TestRefreshReplacesSnapshot(t *testing.T) {
	f, base := allAppsServer(t)
	exp := newTestExporter(t, map[string]string{"SONARR_URL": base + "/sonarr", "SONARR_API_KEY": testKey})
	tg := exp.targets[0]

	tg.refresh(t.Context())
	if n := countPrefix(gather(t, exp), "arr_health_issue"); n != 2 {
		t.Fatalf("%d health series, want 2", n)
	}
	f.set("/sonarr/api/v3/health", `[]`)
	tg.refresh(t.Context())
	got := gather(t, exp)
	if n := countPrefix(got, "arr_health_issue"); n != 0 {
		t.Errorf("%d health series after the issues cleared, want 0", n)
	}
	expect(t, got, map[string]float64{`arr_health_issues{app="sonarr"}`: 0})
}

func TestRunRefreshesUntilCancelled(t *testing.T) {
	f, base := allAppsServer(t)
	exp := newTestExporter(t, allAppsEnv(base))

	ctx, cancel := context.WithCancel(t.Context())
	var stopped atomic.Bool
	done := make(chan struct{})
	go func() {
		exp.run(ctx, 5*time.Millisecond)
		stopped.Store(true)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for f.hitCount("/sonarr/api/v3/series") < 3 || f.hitCount("/bazarr/api/badges") < 3 {
		if time.Now().After(deadline) {
			t.Fatal("refresh loop did not tick")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancel")
	}
	if !stopped.Load() {
		t.Fatal("run returned early")
	}
}

func TestDescribeListsEveryMetric(t *testing.T) {
	ch := make(chan *prometheus.Desc, len(allDescs)+1)
	go func() { (&exporter{}).Describe(ch); close(ch) }()
	n := 0
	for d := range ch {
		n++
		if !strings.Contains(d.String(), "fqName") {
			t.Errorf("odd desc %s", d)
		}
	}
	if n != len(allDescs) || n < 40 {
		t.Errorf("Describe sent %d descs, have %d", n, len(allDescs))
	}
}

func TestErrorSummary(t *testing.T) {
	s := &sink{app: "sonarr"}
	s.fail(errString("/health: boom"))
	s.fail(errString("/queue: bang"))
	if got, want := s.errorSummary(), "sonarr: 2 request(s) failed: /health: boom; /queue: bang"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
