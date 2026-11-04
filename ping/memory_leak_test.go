package main

import (
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/goleak"
)

// TestLeakDNSChurnStopsReplacedProbers simulates a dynamic-DNS target whose
// address changes on every refresh. Each change starts a new prober
// goroutine; the replaced one must exit and its series must be deleted,
// otherwise a long-running exporter accumulates goroutines, ring buffers
// and stale ping_* series for every IP the hostname ever had.
func TestLeakDNSChurnStopsReplacedProbers(t *testing.T) {
	for _, c := range allCollectors {
		if r, ok := c.(interface{ Reset() }); ok {
			r.Reset()
		}
	}
	m := newManager(testConfig(), &stubPinger{rtt: time.Millisecond})
	t.Cleanup(m.stopAll)

	i := 0
	cycle := func() {
		i++
		ip := net.IPv4(10, byte(i>>16), byte(i>>8), byte(i))
		m.reconcile("dyn.example.com", []net.IP{ip})
	}

	baseGoroutines := runtime.NumGoroutine()
	assertNoHeapGrowth(t, 100, 2000, 512<<10, cycle)

	m.mu.Lock()
	n := len(m.probers)
	m.mu.Unlock()
	if n != 1 {
		t.Errorf("len(probers) = %d after churn, want 1", n)
	}
	// Each running prober is exactly one goroutine; allow a little slack
	// for runtime/test goroutines starting or finishing meanwhile.
	if g := runtime.NumGoroutine(); g > baseGoroutines+3 {
		t.Errorf("goroutines grew from %d to %d during DNS churn", baseGoroutines, g)
	}
	if got := testutil.CollectAndCount(lossRatio); got > 1 {
		t.Errorf("ping_loss_ratio has %d series after churn, want at most 1", got)
	}
	if got := testutil.CollectAndCount(rttMean); got > 1 {
		t.Errorf("ping_rtt_mean_seconds has %d series after churn, want at most 1", got)
	}

	m.stopAll()
	if err := goleak.Find(); err != nil {
		t.Errorf("goroutines left after stopAll: %v", err)
	}
}

// TestLeakManyTargetsStartStop starts and stops a large fleet of probers
// repeatedly; every cycle must return to zero probers, zero series and no
// extra goroutines.
func TestLeakManyTargetsStartStop(t *testing.T) {
	m := newManager(testConfig(), &stubPinger{rtt: time.Millisecond})
	cycle := func() {
		for j := 0; j < 50; j++ {
			m.startProber(fmt.Sprintf("t%d", j), net.IPv4(192, 168, 0, byte(j)))
		}
		m.stopAll()
	}
	assertNoHeapGrowth(t, 20, 200, 512<<10, cycle)

	if got := testutil.CollectAndCount(lossRatio); got != 0 {
		t.Errorf("ping_loss_ratio has %d series after stopAll, want 0", got)
	}
	if err := goleak.Find(); err != nil {
		t.Errorf("goroutines left after stopAll: %v", err)
	}
}

// TestLeakHistoryIsFixedSize: the rolling window is a ring buffer, so
// recording a probe must not allocate at all - a slice that grows per
// probe would leak ~1 sample every ping.interval for every target.
func TestLeakHistoryIsFixedSize(t *testing.T) {
	h := newPingHistory(50)
	allocs := testing.AllocsPerRun(10000, func() {
		h.record(true, 3*time.Millisecond)
		h.record(false, 0)
	})
	if allocs != 0 {
		t.Errorf("pingHistory.record allocates %.1f times per call, want 0", allocs)
	}
	if got := len(h.samples); got != 50 {
		t.Errorf("history holds %d samples after 20000 records, want 50", got)
	}
	// stats() builds one short-lived scratch slice per call (freed right
	// after); what matters is that the cost does not grow with uptime.
	statsAllocs := testing.AllocsPerRun(1000, func() { _ = h.stats() })
	if statsAllocs > 1 {
		t.Errorf("pingHistory.stats allocates %.1f times per call, want <= 1", statsAllocs)
	}
	assertNoHeapGrowth(t, 100, 20000, 64<<10, func() {
		h.record(true, time.Millisecond)
		_ = h.stats()
	})
}
