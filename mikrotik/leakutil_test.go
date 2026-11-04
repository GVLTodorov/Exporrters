package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
)

// Helpers shared by this package's *Leak* tests. CI runs those tests in a
// separate step without -race (see .github/workflows/ci.yml), because the
// race detector's shadow memory makes heap numbers meaningless.

// liveHeap returns the bytes still reachable after a full GC, i.e. what a
// long-running exporter would actually keep holding on to.
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC() // second pass collects objects freed by finalizers of the first
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// assertNoHeapGrowth runs fn `warmup` times (so caches, pools and metric
// vectors reach their steady state), measures the live heap, runs fn
// `cycles` more times and fails if the live heap grew by more than
// maxGrowth bytes. A per-cycle leak as small as maxGrowth/cycles bytes is
// caught; normal GC noise stays well below the limit.
func assertNoHeapGrowth(t *testing.T, warmup, cycles int, maxGrowth uint64, fn func()) {
	t.Helper()
	if raceEnabled {
		t.Skip("heap measurements are not meaningful under -race")
	}
	for i := 0; i < warmup; i++ {
		fn()
	}
	before := liveHeap()
	for i := 0; i < cycles; i++ {
		fn()
	}
	after := liveHeap()
	delta := int64(after) - int64(before)
	t.Logf("live heap before=%d after=%d delta=%+d bytes over %d cycles (%.1f B/cycle)",
		before, after, delta, cycles, float64(delta)/float64(cycles))
	if delta > int64(maxGrowth) {
		t.Fatalf("live heap grew by %d bytes over %d cycles (limit %d) - something is retained every cycle",
			delta, cycles, maxGrowth)
	}
}

// connCounter wraps an httptest server and counts how many TCP
// connections clients opened. If a response body is not fully read and
// closed, net/http cannot reuse the connection and every request dials a
// new one, so "new connections == requests" is the signature of a body /
// socket leak, while a healthy client stays at 1-2.
type connCounter struct {
	mu     sync.Mutex
	opened int
	active int
}

func (c *connCounter) track(_ interface{}, state http.ConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch state {
	case http.StateNew:
		c.opened++
		c.active++
	case http.StateClosed, http.StateHijacked:
		c.active--
	}
}

func (c *connCounter) counts() (opened, active int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opened, c.active
}

// newCountingServer starts an httptest server whose connections are
// counted. The server is closed when the test ends.
func newCountingServer(t *testing.T, h http.Handler) (*httptest.Server, *connCounter) {
	t.Helper()
	cc := &connCounter{}
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ConnState = func(c net.Conn, s http.ConnState) { cc.track(c, s) }
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, cc
}
