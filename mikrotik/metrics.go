package main

import (
	"log"

	"github.com/prometheus/client_golang/prometheus"
)

// Metric-name mapping to the currently-deployed nshttpd/mikrotik-exporter,
// whose exact metric names + an "address" label the live "Mikrotik" Grafana
// dashboard on l10 is hard-coded to query (checked 2026-09-02 via the
// dashboard's own JSON, uid 000000168). None of these match today, so
// swapping the Prometheus scrape target to this exporter as-is would blank
// every panel. Kept here so a future rename/dashboard-update pass has a
// ready reference instead of re-diffing the dashboard JSON from scratch:
//
//	ours                        -> nshttpd (dashboard queries this)
//	mikrotik_cpu_load_percent   -> mikrotik_system_cpu_load{address}
//	mikrotik_memory_free_bytes  -> mikrotik_system_free_memory{address}
//	mikrotik_memory_total_bytes -> mikrotik_system_total_memory{address}
//	mikrotik_storage_free_bytes -> mikrotik_system_free_hdd_space{address}
//	mikrotik_storage_total_bytes-> mikrotik_system_total_hdd_space{address}
//	mikrotik_uptime_seconds     -> mikrotik_system_uptime{address}, which
//	                               ALSO carries "version"/"boardname" as
//	                               labels on that same series (the dashboard's
//	                               "Router Summary" panel legend is literally
//	                               "{{version}} on {{boardname}}" off this
//	                               metric) -- we expose those as a separate
//	                               mikrotik_board_info{board_name,version}
//	                               gauge instead, a different shape, not just
//	                               a rename
//	mikrotik_dhcp_leases_active -> mikrotik_dhcp_leases_active_count{address}
//	mikrotik_interface_*_bytes_total -> mikrotik_interface_rx_byte / _tx_byte
//	                               {address,interface,comment} -- note the
//	                               singular name AND a "comment" label (the
//	                               router's own interface comment, e.g.
//	                               "2.4ghz"/"5ghz" on wifi1/wifi2, used in the
//	                               WiFi panel's legend) that we don't
//	                               currently read from /interface/print at all
//
// Every nshttpd metric above carries "address" (its target router's IP) as
// a label, including the dashboard's $node template variable
// (label_values(mikrotik_system_uptime,address)); we deliberately don't
// emit one anywhere (single-target exporter, see requirements.md §5).
//
// mikrotik_up, mikrotik_connections_total, mikrotik_interface_up/enabled,
// and the packet/error/drop interface counters below have no equivalent in
// the current dashboard queries -- they're net-new, not renames.
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
	cpuLoadPercent.Set(parseFloat(r.CPULoad))
	memoryFreeBytes.Set(parseFloat(r.FreeMemory))
	memoryTotalBytes.Set(parseFloat(r.TotalMemory))
	storageFreeBytes.Set(parseFloat(r.FreeHDDSpace))
	storageTotalBytes.Set(parseFloat(r.TotalHDDSpace))

	if seconds, ok := parseUptime(r.Uptime); ok {
		uptimeSeconds.Set(seconds)
	} else {
		log.Printf("Could not parse uptime %q", r.Uptime)
	}

	boardInfo.Reset()
	boardInfo.WithLabelValues(r.BoardName, r.Version).Set(1)
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
		name := iface.Name
		interfaceRxBytesTotal.WithLabelValues(name).Set(parseFloat(iface.RxByte))
		interfaceTxBytesTotal.WithLabelValues(name).Set(parseFloat(iface.TxByte))
		interfaceRxPacketsTotal.WithLabelValues(name).Set(parseFloat(iface.RxPacket))
		interfaceTxPacketsTotal.WithLabelValues(name).Set(parseFloat(iface.TxPacket))
		interfaceRxErrorsTotal.WithLabelValues(name).Set(parseFloat(iface.RxError))
		interfaceTxErrorsTotal.WithLabelValues(name).Set(parseFloat(iface.TxError))
		interfaceRxDropsTotal.WithLabelValues(name).Set(parseFloat(iface.RxDrop))
		interfaceTxDropsTotal.WithLabelValues(name).Set(parseFloat(iface.TxDrop))

		interfaceUp.WithLabelValues(name).Set(boolToFloat(parseBool(iface.Running)))
		interfaceEnabled.WithLabelValues(name).Set(boolToFloat(!parseBool(iface.Disabled)))
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// collectOnce opens one connection to the router and hands it to collect.
func collectOnce(client *Client) {
	conn, err := client.dial()
	if err != nil {
		log.Printf("connect: %v", err)
		up.Set(0)
		return
	}
	defer conn.Close()

	collect(conn)
}

// collect runs every query over an already-connected runner and updates
// every metric. mikrotik_up reflects whether every call this round
// succeeded, so a partial failure (e.g. connection-tracking query times
// out) is still visible even though the other metrics keep their
// last-known values. Split out from collectOnce so it can be unit tested
// against a stub runner instead of a real router connection.
func collect(conn runner) {
	ok := true

	if res, err := fetchSystemResource(conn); err != nil {
		log.Printf("system resource: %v", err)
		ok = false
	} else {
		applySystemResource(res)
	}

	if ifaces, err := fetchInterfaces(conn); err != nil {
		log.Printf("interfaces: %v", err)
		ok = false
	} else {
		applyInterfaces(ifaces)
	}

	if n, err := fetchDHCPBoundLeaseCount(conn); err != nil {
		log.Printf("dhcp lease count: %v", err)
		ok = false
	} else {
		dhcpLeasesActive.Set(float64(n))
	}

	for _, protocol := range []string{"tcp", "udp"} {
		if n, err := fetchConnectionCount(conn, protocol); err != nil {
			log.Printf("connection count (%s): %v", protocol, err)
			ok = false
		} else {
			connectionsTotal.WithLabelValues(protocol).Set(float64(n))
		}
	}

	up.Set(boolToFloat(ok))
}
