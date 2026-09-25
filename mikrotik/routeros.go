package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"

	ros "github.com/go-routeros/routeros/v3"
)

// Client dials a single RouterOS device's classic binary API
// (https://help.mikrotik.com/docs/spaces/ROS/pages/8978457/API) — not the
// REST API, which requires the www/www-ssl service to be enabled and is off
// by default on most home routers, including the one this project targets.
type Client struct {
	host               string
	port               string
	user               string
	password           string
	useTLS             bool
	insecureSkipVerify bool
	timeout            time.Duration
}

func newClient(cfg Config) *Client {
	return &Client{
		host:               cfg.Address,
		port:               cfg.APIPort,
		user:               cfg.User,
		password:           cfg.Password,
		useTLS:             cfg.UseTLS,
		insecureSkipVerify: cfg.InsecureSkipVerify,
		timeout:            cfg.RequestTimeout,
	}
}

// runner is the subset of *ros.Client this file depends on, so tests can
// substitute a stub that returns canned sentences without speaking the real
// wire protocol.
type runner interface {
	Run(sentence ...string) (*ros.Reply, error)
}

// dial opens one connection and logs in. Callers must Close() it.
func (c *Client) dial() (*ros.Client, error) {
	addr := net.JoinHostPort(c.host, c.port)
	if c.useTLS {
		return ros.DialTLSTimeout(addr, c.user, c.password, &tls.Config{InsecureSkipVerify: c.insecureSkipVerify}, c.timeout) //nolint:gosec // opt-in for home-router self-signed certs
	}
	return ros.DialTimeout(addr, c.user, c.password, c.timeout)
}

// SystemResource mirrors the /system/resource print fields we use.
type SystemResource struct {
	CPULoad       string
	FreeMemory    string
	TotalMemory   string
	FreeHDDSpace  string
	TotalHDDSpace string
	Uptime        string
	Version       string
	BoardName     string
}

func fetchSystemResource(c runner) (SystemResource, error) {
	reply, err := c.Run("/system/resource/print")
	if err != nil {
		return SystemResource{}, err
	}
	if len(reply.Re) == 0 {
		return SystemResource{}, fmt.Errorf("empty /system/resource/print reply")
	}
	m := reply.Re[0].Map
	return SystemResource{
		CPULoad:       m["cpu-load"],
		FreeMemory:    m["free-memory"],
		TotalMemory:   m["total-memory"],
		FreeHDDSpace:  m["free-hdd-space"],
		TotalHDDSpace: m["total-hdd-space"],
		Uptime:        m["uptime"],
		Version:       m["version"],
		BoardName:     m["board-name"],
	}, nil
}

// Interface mirrors the /interface print fields we use.
type Interface struct {
	Name     string
	Running  string
	Disabled string
	RxByte   string
	TxByte   string
	RxPacket string
	TxPacket string
	RxError  string
	TxError  string
	RxDrop   string
	TxDrop   string
}

// fetchInterfaces reads the live interface list, so callers never need to
// hardcode names like ether1/bridge/wireguard/wifi1.
func fetchInterfaces(c runner) ([]Interface, error) {
	reply, err := c.Run("/interface/print")
	if err != nil {
		return nil, err
	}
	ifaces := make([]Interface, 0, len(reply.Re))
	for _, sen := range reply.Re {
		m := sen.Map
		ifaces = append(ifaces, Interface{
			Name:     m["name"],
			Running:  m["running"],
			Disabled: m["disabled"],
			RxByte:   m["rx-byte"],
			TxByte:   m["tx-byte"],
			RxPacket: m["rx-packet"],
			TxPacket: m["tx-packet"],
			RxError:  m["rx-error"],
			TxError:  m["tx-error"],
			RxDrop:   m["rx-drop"],
			TxDrop:   m["tx-drop"],
		})
	}
	return ifaces, nil
}

// HealthSensor is one row of RouterOS v7's /system/health print, which
// returns one sentence per sensor (e.g. name=cpu-temperature value=44
// type=C) rather than v6's single sentence of fixed fields.
type HealthSensor struct {
	Name  string
	Value string
	Type  string
}

// fetchHealth reads every /system/health sensor. Boards without sensors
// (e.g. CHR) return an empty list, which is not an error.
func fetchHealth(c runner) ([]HealthSensor, error) {
	reply, err := c.Run("/system/health/print")
	if err != nil {
		return nil, err
	}
	sensors := make([]HealthSensor, 0, len(reply.Re))
	for _, sen := range reply.Re {
		m := sen.Map
		sensors = append(sensors, HealthSensor{
			Name:  m["name"],
			Value: m["value"],
			Type:  m["type"],
		})
	}
	return sensors, nil
}

// countOnly runs {path}/print with a `?filter` query and the count-only
// flag, and returns the count. This deliberately never dumps the full
// table — important for /ip/firewall/connection, whose conntrack table can
// be large on a busy router.
func countOnly(c runner, path string, filter string) (int, error) {
	sentence := []string{path + "/print"}
	if filter != "" {
		sentence = append(sentence, "?"+filter)
	}
	sentence = append(sentence, "=count-only=")

	reply, err := c.Run(sentence...)
	if err != nil {
		return 0, err
	}

	var ret string
	switch {
	case len(reply.Re) > 0:
		ret = reply.Re[0].Map["ret"]
	case reply.Done != nil:
		ret = reply.Done.Map["ret"]
	}
	if ret == "" {
		return 0, fmt.Errorf("no ret in count-only reply from %s", path)
	}
	n, err := strconv.Atoi(ret)
	if err != nil {
		return 0, fmt.Errorf("parsing count %q from %s: %w", ret, path, err)
	}
	return n, nil
}

// fetchDHCPBoundLeaseCount returns the number of DHCP leases with status=bound.
func fetchDHCPBoundLeaseCount(c runner) (int, error) {
	return countOnly(c, "/ip/dhcp-server/lease", "status=bound")
}

// fetchConnectionCount returns the number of active /ip/firewall/connection
// entries for the given protocol ("tcp" or "udp").
func fetchConnectionCount(c runner, protocol string) (int, error) {
	return countOnly(c, "/ip/firewall/connection", "protocol="+protocol)
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseBool(s string) bool {
	return s == "true" || s == "yes"
}

// uptimePattern matches RouterOS's "1w2d3h4m5s"-style uptime duration
// strings; any subset of the five components may be present.
var uptimePattern = regexp.MustCompile(`(\d+)([wdhms])`)

// parseUptime converts a RouterOS uptime string (e.g. "4w3d12h5m54s",
// "2d20h12m20s", "45s") into seconds. An empty or unrecognized string yields
// 0 and false.
func parseUptime(s string) (float64, bool) {
	matches := uptimePattern.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return 0, false
	}
	var total float64
	for _, m := range matches {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		switch m[2] {
		case "w":
			total += n * 7 * 24 * time.Hour.Seconds()
		case "d":
			total += n * 24 * time.Hour.Seconds()
		case "h":
			total += n * time.Hour.Seconds()
		case "m":
			total += n * time.Minute.Seconds()
		case "s":
			total += n
		}
	}
	return total, true
}
