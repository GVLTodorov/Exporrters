package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// appTarget is one application being polled. Each refresh builds a full new
// set of metrics and swaps it in, so /metrics always serves one consistent
// snapshot and series that disappear upstream (a finished queue item, a
// fixed health issue) disappear here too.
type appTarget struct {
	name    string
	client  *apiClient
	collect collectFunc

	mu       sync.RWMutex
	snapshot []prometheus.Metric
}

// exporter is a prometheus.Collector serving the latest snapshot of every
// configured application.
type exporter struct {
	targets []*appTarget
}

func newExporter(cfg config) *exporter {
	httpClient := &http.Client{Timeout: cfg.timeout}
	e := &exporter{}
	for _, t := range cfg.targets {
		e.targets = append(e.targets, &appTarget{
			name:    t.spec.name,
			client:  &apiClient{http: httpClient, apiURL: t.baseURL + t.spec.apiPath, apiKey: t.apiKey},
			collect: t.spec.collect,
		})
	}
	return e
}

// Describe implements prometheus.Collector.
func (e *exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range allDescs {
		ch <- d
	}
}

// Collect implements prometheus.Collector.
func (e *exporter) Collect(ch chan<- prometheus.Metric) {
	for _, t := range e.targets {
		t.mu.RLock()
		for _, m := range t.snapshot {
			ch <- m
		}
		t.mu.RUnlock()
	}
}

// refresh polls the application once and replaces its snapshot.
func (t *appTarget) refresh(ctx context.Context) {
	start := time.Now()
	s := &sink{app: t.name}
	t.collect(ctx, t.client, s)
	s.shared(descUp, boolFloat(s.up))
	s.shared(descRefreshErrors, float64(len(s.errs)))
	s.shared(descRefreshDuration, time.Since(start).Seconds())
	s.shared(descLastRefresh, float64(time.Now().UnixNano())/1e9)
	if len(s.errs) > 0 {
		log.Print(s.errorSummary())
	}
	t.mu.Lock()
	t.snapshot = s.metrics
	t.mu.Unlock()
}

// run refreshes every application immediately and then every interval, each
// on its own goroutine so a slow application never delays the others. It
// returns once ctx is cancelled and every refresh has stopped.
func (e *exporter) run(ctx context.Context, interval time.Duration) {
	var wg sync.WaitGroup
	for _, t := range e.targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				t.refresh(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	wg.Wait()
}
