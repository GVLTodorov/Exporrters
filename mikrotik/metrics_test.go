package main

import (
	"errors"
	"fmt"
	"testing"

	ros "github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestApplySystemResource(t *testing.T) {
	applySystemResource(SystemResource{
		CPULoad:       "12",
		FreeMemory:    "1000",
		TotalMemory:   "2000",
		FreeHDDSpace:  "3000",
		TotalHDDSpace: "4000",
		Uptime:        "1d2h3m4s",
		Version:       "7.23.3 (stable)",
		BoardName:     "hAP ax^3",
	})

	if got := testutil.ToFloat64(cpuLoadPercent); got != 12 {
		t.Errorf("cpuLoadPercent = %v, want 12", got)
	}
	if got := testutil.ToFloat64(memoryFreeBytes); got != 1000 {
		t.Errorf("memoryFreeBytes = %v, want 1000", got)
	}
	if got := testutil.ToFloat64(memoryTotalBytes); got != 2000 {
		t.Errorf("memoryTotalBytes = %v, want 2000", got)
	}
	if got := testutil.ToFloat64(storageFreeBytes); got != 3000 {
		t.Errorf("storageFreeBytes = %v, want 3000", got)
	}
	if got := testutil.ToFloat64(storageTotalBytes); got != 4000 {
		t.Errorf("storageTotalBytes = %v, want 4000", got)
	}
	wantUptime := float64(24*3600 + 2*3600 + 3*60 + 4)
	if got := testutil.ToFloat64(uptimeSeconds); got != wantUptime {
		t.Errorf("uptimeSeconds = %v, want %v", got, wantUptime)
	}
	if got := testutil.ToFloat64(boardInfo.WithLabelValues("hAP ax^3", "7.23.3 (stable)")); got != 1 {
		t.Errorf("boardInfo{hAP ax^3, 7.23.3 (stable)} = %v, want 1", got)
	}
}

func TestApplyInterfacesResetsStaleLabels(t *testing.T) {
	applyInterfaces([]Interface{
		{Name: "ether1", Running: "true", Disabled: "false", RxByte: "10", TxByte: "20"},
		{Name: "wireguard1", Running: "true", Disabled: "false", RxByte: "1", TxByte: "2"},
	})
	if got := testutil.ToFloat64(interfaceRxBytesTotal.WithLabelValues("ether1")); got != 10 {
		t.Errorf("ether1 rx bytes = %v, want 10", got)
	}
	if n := testutil.CollectAndCount(interfaceUp); n != 2 {
		t.Fatalf("interfaceUp series count = %d, want 2", n)
	}

	// wireguard1 disappears (e.g. the peer was removed) — the next apply must
	// drop its series rather than keep serving a stale last-known value.
	applyInterfaces([]Interface{
		{Name: "ether1", Running: "true", Disabled: "false", RxByte: "30", TxByte: "40"},
	})
	if n := testutil.CollectAndCount(interfaceUp); n != 1 {
		t.Fatalf("interfaceUp series count = %d, want 1 after wireguard1 disappears", n)
	}
	if got := testutil.ToFloat64(interfaceRxBytesTotal.WithLabelValues("ether1")); got != 30 {
		t.Errorf("ether1 rx bytes = %v, want 30 (updated)", got)
	}
}

func TestApplyInterfacesRunningAndDisabled(t *testing.T) {
	applyInterfaces([]Interface{
		{Name: "ether2", Running: "false", Disabled: "true"},
	})
	if got := testutil.ToFloat64(interfaceUp.WithLabelValues("ether2")); got != 0 {
		t.Errorf("interfaceUp = %v, want 0 (not running)", got)
	}
	if got := testutil.ToFloat64(interfaceEnabled.WithLabelValues("ether2")); got != 0 {
		t.Errorf("interfaceEnabled = %v, want 0 (disabled)", got)
	}
}

func TestApplyHealth(t *testing.T) {
	applyHealth([]HealthSensor{
		{Name: "cpu-temperature", Value: "44", Type: "C"},
		{Name: "board-temperature1", Value: "38.5", Type: "C"},
		{Name: "voltage", Value: "24.1", Type: "V"},
		{Name: "fan1-speed", Value: "3200", Type: "RPM"},
	})
	if got := testutil.ToFloat64(healthTemperatureCelsius.WithLabelValues("cpu-temperature")); got != 44 {
		t.Errorf("cpu-temperature = %v, want 44", got)
	}
	if got := testutil.ToFloat64(healthTemperatureCelsius.WithLabelValues("board-temperature1")); got != 38.5 {
		t.Errorf("board-temperature1 = %v, want 38.5", got)
	}
	// Only type=C sensors become temperature series.
	if n := testutil.CollectAndCount(healthTemperatureCelsius); n != 2 {
		t.Fatalf("temperature series count = %d, want 2", n)
	}

	// board-temperature1 disappears and cpu-temperature reports garbage —
	// neither may leave a stale or zeroed series behind.
	applyHealth([]HealthSensor{
		{Name: "cpu-temperature", Value: "n/a", Type: "C"},
	})
	if n := testutil.CollectAndCount(healthTemperatureCelsius); n != 0 {
		t.Fatalf("temperature series count = %d, want 0", n)
	}
}

// fakeRouter wires up canned replies for every command collect() depends
// on, so behavior can be verified end-to-end without a real router
// connection. Its Run method inspects the full sentence (not just the
// command word) so it can distinguish the tcp/udp connection-count calls,
// which share a command word but differ in their ?protocol= filter.
type fakeRouter struct {
	dhcpFails   bool
	healthFails bool
}

func (f *fakeRouter) Run(words ...string) (*ros.Reply, error) {
	switch words[0] {
	case "/system/resource/print":
		return &ros.Reply{Re: []*proto.Sentence{sentence(map[string]string{
			"cpu-load": "7", "free-memory": "111", "total-memory": "222",
			"free-hdd-space": "333", "total-hdd-space": "444",
			"uptime": "5m", "version": "7.23.3", "board-name": "hAP ax^3",
		})}}, nil
	case "/interface/print":
		return &ros.Reply{Re: []*proto.Sentence{sentence(map[string]string{
			"name": "ether1", "running": "true", "disabled": "false",
		})}}, nil
	case "/system/health/print":
		if f.healthFails {
			return nil, errors.New("not enough permissions")
		}
		return &ros.Reply{Re: []*proto.Sentence{sentence(map[string]string{
			"name": "cpu-temperature", "value": "51", "type": "C",
		})}}, nil
	case "/ip/dhcp-server/lease/print":
		if f.dhcpFails {
			return nil, errors.New("timeout")
		}
		return &ros.Reply{Re: []*proto.Sentence{sentence(map[string]string{"ret": "9"})}}, nil
	case "/ip/firewall/connection/print":
		for _, word := range words {
			if word == "?protocol=tcp" {
				return &ros.Reply{Re: []*proto.Sentence{sentence(map[string]string{"ret": "5"})}}, nil
			}
			if word == "?protocol=udp" {
				return &ros.Reply{Re: []*proto.Sentence{sentence(map[string]string{"ret": "2"})}}, nil
			}
		}
		return nil, fmt.Errorf("unexpected sentence: %v", words)
	default:
		return nil, fmt.Errorf("unexpected command: %v", words)
	}
}

func TestCollectSuccess(t *testing.T) {
	collect(&fakeRouter{})

	if got := testutil.ToFloat64(up); got != 1 {
		t.Errorf("up = %v, want 1", got)
	}
	if got := testutil.ToFloat64(dhcpLeasesActive); got != 9 {
		t.Errorf("dhcpLeasesActive = %v, want 9", got)
	}
	if got := testutil.ToFloat64(connectionsTotal.WithLabelValues("tcp")); got != 5 {
		t.Errorf("connectionsTotal{tcp} = %v, want 5", got)
	}
	if got := testutil.ToFloat64(connectionsTotal.WithLabelValues("udp")); got != 2 {
		t.Errorf("connectionsTotal{udp} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(cpuLoadPercent); got != 7 {
		t.Errorf("cpuLoadPercent = %v, want 7", got)
	}
	if got := testutil.ToFloat64(healthTemperatureCelsius.WithLabelValues("cpu-temperature")); got != 51 {
		t.Errorf("healthTemperatureCelsius{cpu-temperature} = %v, want 51", got)
	}
}

func TestCollectPartialFailureMarksDown(t *testing.T) {
	collect(&fakeRouter{dhcpFails: true})

	if got := testutil.ToFloat64(up); got != 0 {
		t.Errorf("up = %v, want 0 when the DHCP lease query fails", got)
	}
	// Other metrics still reflect whatever succeeded this round.
	if got := testutil.ToFloat64(cpuLoadPercent); got != 7 {
		t.Errorf("cpuLoadPercent = %v, want 7 (system resource still succeeded)", got)
	}
}

func TestCollectHealthFailureMarksDown(t *testing.T) {
	collect(&fakeRouter{healthFails: true})

	if got := testutil.ToFloat64(up); got != 0 {
		t.Errorf("up = %v, want 0 when the health query fails", got)
	}
	if got := testutil.ToFloat64(dhcpLeasesActive); got != 9 {
		t.Errorf("dhcpLeasesActive = %v, want 9 (later queries still run)", got)
	}
}
