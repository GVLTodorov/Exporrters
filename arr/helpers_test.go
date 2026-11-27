package main

import (
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// Refresh errors are logged; keep test output readable.
	log.SetOutput(io.Discard)
	// Every test must leave no goroutine behind (refresh loops, HTTP
	// keep-alive readers, the shutdown watcher in run).
	goleak.VerifyTestMain(m)
}

const testKey = "test-api-key"

// fakeAPI is a fake *arr API: a fixed JSON body (or status code) per path.
// Requests without the right X-Api-Key get 401, like the real apps.
type fakeAPI struct {
	mu     sync.Mutex
	bodies map[string]string
	status map[string]int
	hits   map[string]int
	// handlers override bodies/status for paths that need request-dependent
	// replies (queue paging).
	handlers map[string]http.HandlerFunc
}

func newFakeAPI(bodies map[string]string) *fakeAPI {
	f := &fakeAPI{bodies: map[string]string{}, status: map[string]int{}, hits: map[string]int{}, handlers: map[string]http.HandlerFunc{}}
	for k, v := range bodies {
		f.bodies[k] = v
	}
	return f
}

func (f *fakeAPI) set(path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies[path] = body
}

func (f *fakeAPI) fail(path string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[path] = code
}

func (f *fakeAPI) handle(path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[path] = h
}

func (f *fakeAPI) hitCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != testKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	f.hits[r.URL.Path]++
	h, hasHandler := f.handlers[r.URL.Path]
	code, failing := f.status[r.URL.Path]
	body, ok := f.bodies[r.URL.Path]
	f.mu.Unlock()
	switch {
	case hasHandler:
		h(w, r)
	case failing:
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"message":"boom"}`)
	case ok:
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	default:
		http.NotFound(w, r)
	}
}

// newTestClient starts srv for h and returns a client for apiPath on it.
func newTestClient(t *testing.T, h http.Handler, apiPath string) (*apiClient, *connCounter) {
	t.Helper()
	srv, cc := newCountingServer(t, h)
	hc := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(hc.CloseIdleConnections)
	return &apiClient{http: hc, apiURL: srv.URL + apiPath, apiKey: testKey}, cc
}

// staticCollector serves a fixed metric list, so a sink can be gathered
// through a real registry (which also rejects duplicate series).
type staticCollector []prometheus.Metric

func (c staticCollector) Describe(ch chan<- *prometheus.Desc) { prometheus.DescribeByCollect(c, ch) }
func (c staticCollector) Collect(ch chan<- prometheus.Metric) {
	for _, m := range c {
		ch <- m
	}
}

// gather returns every series as `name{k="v",...}` -> value.
func gather(t *testing.T, c prometheus.Collector) map[string]float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	out := map[string]float64{}
	for _, mf := range families {
		for _, m := range mf.GetMetric() {
			var labels []string
			for _, lp := range m.GetLabel() {
				labels = append(labels, lp.GetName()+`="`+lp.GetValue()+`"`)
			}
			sort.Strings(labels)
			key := mf.GetName()
			if len(labels) > 0 {
				key += "{" + strings.Join(labels, ",") + "}"
			}
			var v float64
			switch {
			case m.GetGauge() != nil:
				v = m.GetGauge().GetValue()
			case m.GetCounter() != nil:
				v = m.GetCounter().GetValue()
			}
			out[key] = v
		}
	}
	return out
}

// expect checks that every wanted series is present with its value.
func expect(t *testing.T, got map[string]float64, want map[string]float64) {
	t.Helper()
	for k, v := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("missing series %s\ngot: %v", k, keys(got))
			continue
		}
		if g != v {
			t.Errorf("%s = %v, want %v", k, g, v)
		}
	}
}

// expectAbsent checks that no series of the given metric names exist.
func expectAbsent(t *testing.T, got map[string]float64, names ...string) {
	t.Helper()
	for k := range got {
		for _, n := range names {
			if k == n || strings.HasPrefix(k, n+"{") {
				t.Errorf("unexpected series %s", k)
			}
		}
	}
}

func keys(m map[string]float64) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// collectWith runs one collectFunc against h and gathers the result.
func collectWith(t *testing.T, fn collectFunc, h http.Handler, apiPath string) (*sink, map[string]float64) {
	t.Helper()
	c, _ := newTestClient(t, h, apiPath)
	s := &sink{app: "test"}
	fn(t.Context(), c, s)
	return s, gather(t, staticCollector(s.metrics))
}
