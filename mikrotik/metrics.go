package main

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	up = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_up",
		Help: "1 if the last scrape of the RouterOS device succeeded, 0 otherwise.",
	})

	cpuLoadPercent = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_cpu_load_percent",
		Help: "CPU load percentage reported by /system/resource.",
	})
	memoryFreeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_memory_free_bytes",
		Help: "Free RAM in bytes.",
	})
	memoryTotalBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_memory_total_bytes",
		Help: "Total RAM in bytes.",
	})
	storageFreeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_storage_free_bytes",
		Help: "Free storage in bytes.",
	})
	storageTotalBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_storage_total_bytes",
		Help: "Total storage in bytes.",
	})
	uptimeSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_uptime_seconds",
		Help: "Device uptime in seconds.",
	})
	boardInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_board_info",
		Help: "Constant 1, labeled with board model and RouterOS version.",
	}, []string{"board_name", "version"})

	dhcpLeasesActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mikrotik_dhcp_leases_active",
		Help: "Number of DHCP leases with status=bound.",
	})

	connectionsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_connections_total",
		Help: "Active /ip/firewall/connection entries by protocol.",
	}, []string{"protocol"})

	interfaceRxBytesTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_rx_bytes_total",
		Help: "Received bytes per interface, as reported by the router (resets on interface reset/reboot).",
	}, []string{"interface"})
	interfaceTxBytesTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_tx_bytes_total",
		Help: "Transmitted bytes per interface, as reported by the router (resets on interface reset/reboot).",
	}, []string{"interface"})
	interfaceRxPacketsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_rx_packets_total",
		Help: "Received packets per interface.",
	}, []string{"interface"})
	interfaceTxPacketsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_tx_packets_total",
		Help: "Transmitted packets per interface.",
	}, []string{"interface"})
	interfaceRxErrorsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_rx_errors_total",
		Help: "Receive errors per interface.",
	}, []string{"interface"})
	interfaceTxErrorsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_tx_errors_total",
		Help: "Transmit errors per interface.",
	}, []string{"interface"})
	interfaceRxDropsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_rx_drops_total",
		Help: "Received packets dropped per interface.",
	}, []string{"interface"})
	interfaceTxDropsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_tx_drops_total",
		Help: "Transmitted packets dropped per interface.",
	}, []string{"interface"})
	interfaceUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_up",
		Help: "1 if the interface is running, 0 otherwise.",
	}, []string{"interface"})
	interfaceEnabled = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mikrotik_interface_enabled",
		Help: "1 if the interface is enabled (not administratively disabled), 0 otherwise.",
	}, []string{"interface"})
)

// allCollectors is used both to register everything once and, in tests, to
// reset every metric to a clean state between cases.
var allCollectors = []prometheus.Collector{
	up,
	cpuLoadPercent, memoryFreeBytes, memoryTotalBytes, storageFreeBytes, storageTotalBytes,
	uptimeSeconds, boardInfo,
	dhcpLeasesActive, connectionsTotal,
	interfaceRxBytesTotal, interfaceTxBytesTotal,
	interfaceRxPacketsTotal, interfaceTxPacketsTotal,
	interfaceRxErrorsTotal, interfaceTxErrorsTotal,
	interfaceRxDropsTotal, interfaceTxDropsTotal,
	interfaceUp, interfaceEnabled,
}

func registerMetrics() {
	prometheus.MustRegister(allCollectors...)
}

// applySystemResource maps a SystemResource reply onto the scalar gauges.
func applySystemResource(r SystemResource) {
	cpuLoadPercent.Set(r.CPULoad.Float())
	memoryFreeBytes.Set(r.FreeMemory.Float())
	memoryTotalBytes.Set(r.TotalMemory.Float())
	storageFreeBytes.Set(r.FreeHDDSpace.Float())
	storageTotalBytes.Set(r.TotalHDDSpace.Float())

	if seconds, ok := parseUptime(r.Uptime.String()); ok {
		uptimeSeconds.Set(seconds)
	} else {
		log.Printf("Could not parse uptime %q", r.Uptime)
	}

	boardInfo.Reset()
	boardInfo.WithLabelValues(r.BoardName.String(), r.Version.String()).Set(1)
}

// applyInterfaces maps the live interface list onto the per-interface
// GaugeVecs. Every vector is reset first so an interface that disappears
// (VLAN/peer removed) drops out of /metrics instead of leaving a stale
// series behind.
func applyInterfaces(ifaces []Interface) {
	interfaceRxBytesTotal.Reset()
	interfaceTxBytesTotal.Reset()
	interfaceRxPacketsTotal.Reset()
	interfaceTxPacketsTotal.Reset()
	interfaceRxErrorsTotal.Reset()
	interfaceTxErrorsTotal.Reset()
	interfaceRxDropsTotal.Reset()
	interfaceTxDropsTotal.Reset()
	interfaceUp.Reset()
	interfaceEnabled.Reset()

	for _, iface := range ifaces {
		name := iface.Name.String()
		interfaceRxBytesTotal.WithLabelValues(name).Set(iface.RxByte.Float())
		interfaceTxBytesTotal.WithLabelValues(name).Set(iface.TxByte.Float())
		interfaceRxPacketsTotal.WithLabelValues(name).Set(iface.RxPacket.Float())
		interfaceTxPacketsTotal.WithLabelValues(name).Set(iface.TxPacket.Float())
		interfaceRxErrorsTotal.WithLabelValues(name).Set(iface.RxError.Float())
		interfaceTxErrorsTotal.WithLabelValues(name).Set(iface.TxError.Float())
		interfaceRxDropsTotal.WithLabelValues(name).Set(iface.RxDrop.Float())
		interfaceTxDropsTotal.WithLabelValues(name).Set(iface.TxDrop.Float())

		interfaceUp.WithLabelValues(name).Set(boolToFloat(iface.Running.Bool()))
		interfaceEnabled.WithLabelValues(name).Set(boolToFloat(!iface.Disabled.Bool()))
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// collectOnce polls the router once and updates every metric. mikrotik_up
// reflects whether every call this round succeeded, so a partial failure
// (e.g. connection-tracking query times out) is still visible even though
// the other metrics keep their last-known values.
func collectOnce(client *Client) {
	ok := true

	if res, err := client.SystemResource(); err != nil {
		log.Printf("system resource: %v", err)
		ok = false
	} else {
		applySystemResource(res)
	}

	if ifaces, err := client.Interfaces(); err != nil {
		log.Printf("interfaces: %v", err)
		ok = false
	} else {
		applyInterfaces(ifaces)
	}

	if n, err := client.DHCPBoundLeaseCount(); err != nil {
		log.Printf("dhcp lease count: %v", err)
		ok = false
	} else {
		dhcpLeasesActive.Set(float64(n))
	}

	for _, protocol := range []string{"tcp", "udp"} {
		if n, err := client.ConnectionCount(protocol); err != nil {
			log.Printf("connection count (%s): %v", protocol, err)
			ok = false
		} else {
			connectionsTotal.WithLabelValues(protocol).Set(float64(n))
		}
	}

	up.Set(boolToFloat(ok))
}
