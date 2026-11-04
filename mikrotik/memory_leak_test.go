package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"testing"

	ros "github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func quietLogs(t *testing.T) {
	t.Helper()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
}

// fullStub returns a stub router that answers every query collect() makes,
// with `iface` as the name of the second interface.
func fullStub(iface string) *stubRunner {
	return &stubRunner{replies: map[string]*ros.Reply{
		"/system/resource/print": {Re: []*proto.Sentence{sentence(map[string]string{
			"cpu-load": "7", "free-memory": "1000", "total-memory": "2000",
			"free-hdd-space": "3000", "total-hdd-space": "4000",
			"uptime": "1w2d3h4m5s", "version": "7.23.3 (stable)", "board-name": "hAP ax^3",
		})}},
		"/interface/print": {Re: []*proto.Sentence{
			sentence(map[string]string{"name": "ether1", "running": "true", "disabled": "false", "rx-byte": "1", "tx-byte": "2"}),
			sentence(map[string]string{"name": iface, "running": "true", "disabled": "false", "rx-byte": "3", "tx-byte": "4"}),
		}},
		"/system/health/print": {Re: []*proto.Sentence{
			sentence(map[string]string{"name": "cpu-temperature", "value": "48", "type": "C"}),
		}},
		"/ip/dhcp-server/lease/print":    {Done: &proto.Sentence{Map: map[string]string{"ret": "12"}}},
		"/ip/firewall/connection/print": {Done: &proto.Sentence{Map: map[string]string{"ret": "345"}}},
	}}
}

// TestLeakInterfaceChurnKeepsSeriesBounded simulates a router whose
// interface list keeps changing (PPPoE / L2TP / WireGuard peers come and go
// with a new name every time). Every vanished interface must drop out of
// /metrics, otherwise the exporter's memory and Prometheus' series count
// grow forever.
func TestLeakInterfaceChurnKeepsSeriesBounded(t *testing.T) {
	quietLogs(t)
	i := 0
	cycle := func() {
		i++
		collect(fullStub(fmt.Sprintf("<pppoe-user%d>", i)))
	}

	assertNoHeapGrowth(t, 200, 3000, 256<<10, cycle)

	if n := testutil.CollectAndCount(interfaceUp); n != 2 {
		t.Errorf("mikrotik_interface_up has %d series after churn, want 2 (ether1 + current peer)", n)
	}
	if n := testutil.CollectAndCount(interfaceRxBytesTotal); n != 2 {
		t.Errorf("mikrotik_interface_rx_bytes_total has %d series after churn, want 2", n)
	}
	if got := testutil.ToFloat64(up); got != 1 {
		t.Errorf("mikrotik_up = %v, want 1 with a fully answering stub", got)
	}
}

// TestLeakBoardAndSensorLabelsDoNotAccumulate covers the other label sets
// that come from the router: a firmware upgrade changes board_info's
// version label and sensors can appear or disappear. Old label values
// must be dropped, not kept alongside the new ones.
func TestLeakBoardAndSensorLabelsDoNotAccumulate(t *testing.T) {
	quietLogs(t)
	for i := 0; i < 500; i++ {
		applySystemResource(SystemResource{Uptime: "1s", BoardName: "hAP", Version: fmt.Sprintf("7.%d", i)})
		applyHealth([]HealthSensor{{Name: fmt.Sprintf("sensor-%d", i), Value: "40", Type: "C"}})
	}
	if n := testutil.CollectAndCount(boardInfo); n != 1 {
		t.Errorf("mikrotik_board_info has %d series, want 1", n)
	}
	if n := testutil.CollectAndCount(healthTemperatureCelsius); n != 1 {
		t.Errorf("mikrotik_health_temperature_celsius has %d series, want 1", n)
	}
}

// TestLeakFailingRouterDoesNotGrowHeap: a router that keeps failing
// (wrong password, unreachable) must not make the exporter retain error
// values, log buffers or partial replies between scrapes.
func TestLeakFailingRouterDoesNotGrowHeap(t *testing.T) {
	quietLogs(t)
	failing := func() {
		collect(&stubRunner{errs: map[string]error{
			"/system/resource/print":        fmt.Errorf("timeout"),
			"/interface/print":              fmt.Errorf("timeout"),
			"/system/health/print":          fmt.Errorf("timeout"),
			"/ip/dhcp-server/lease/print":   fmt.Errorf("timeout"),
			"/ip/firewall/connection/print": fmt.Errorf("timeout"),
		}})
	}
	assertNoHeapGrowth(t, 100, 3000, 256<<10, failing)
	if got := testutil.ToFloat64(up); got != 0 {
		t.Errorf("mikrotik_up = %v, want 0 when every query fails", got)
	}
}
