package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestRootRedirectsToMetrics(t *testing.T) {
	mux := newMux(promhttp.Handler())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("GET / = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/metrics" {
		t.Errorf("Location = %q, want /metrics", loc)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Errorf("GET /metrics = %d, want 200 with Prometheus output", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /favicon.ico = %d, want 404", rec.Code)
	}
}

func TestRootRedirectIsFollowedByClients(t *testing.T) {
	srv := httptest.NewServer(newMux(promhttp.Handler()))
	t.Cleanup(srv.Close)
	client := srv.Client()
	t.Cleanup(client.CloseIdleConnections)

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/metrics" {
		t.Errorf("following / ended at %s with %d, want /metrics 200", resp.Request.URL.Path, resp.StatusCode)
	}
}
