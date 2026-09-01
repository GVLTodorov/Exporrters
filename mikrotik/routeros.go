package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client talks to a single RouterOS device over its REST API
// (https://help.mikrotik.com/docs/spaces/ROS/pages/47579162/REST+API).
type Client struct {
	baseURL    string
	user       string
	password   string
	httpClient *http.Client
}

// newClient builds a Client for cfg.Address using cfg's scheme/TLS settings.
func newClient(cfg Config) *Client {
	scheme := "https"
	if !cfg.UseHTTPS {
		scheme = "http"
	}
	transport := &http.Transport{}
	if cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for home-router self-signed certs
	}
	return newClientWithBaseURL(
		fmt.Sprintf("%s://%s", scheme, cfg.Address),
		cfg.User, cfg.Password,
		&http.Client{Timeout: cfg.RequestTimeout, Transport: transport},
	)
}

// newClientWithBaseURL is the lower-level constructor used directly by tests
// (pointed at an httptest.Server, so no TLS involved).
func newClientWithBaseURL(baseURL, user, password string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		user:       user,
		password:   password,
		httpClient: httpClient,
	}
}

// maxBodyBytes bounds how much of a response we ever read, so a misbehaving
// endpoint can't blow up memory.
const maxBodyBytes = 4 << 20

// do performs an HTTP request against the REST API and returns the raw body,
// after checking for a non-2xx status.
func (c *Client) do(method, path string, body []byte) ([]byte, error) {
	url := c.baseURL + "/rest/" + strings.TrimPrefix(path, "/")

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxBodyBytes)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

// decodeOneOrMany decodes body as a single JSON object of type T, falling
// back to a one-element JSON array of T. RouterOS REST is inconsistent about
// whether a "singleton" endpoint (e.g. system/resource) or a print reply is
// wrapped in an array across versions, so both shapes are accepted.
func decodeOneOrMany[T any](body []byte) (T, error) {
	var one T
	if err := json.Unmarshal(body, &one); err == nil {
		return one, nil
	}
	var many []T
	if err := json.Unmarshal(body, &many); err != nil {
		var zero T
		return zero, fmt.Errorf("decoding response: %w", err)
	}
	if len(many) == 0 {
		var zero T
		return zero, fmt.Errorf("empty response")
	}
	return many[0], nil
}

// flexString accepts a RouterOS REST field encoded as a JSON string, number,
// or boolean and stores it as plain text, since RouterOS has changed the
// encoding of numeric fields across versions.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(b)
	return nil
}

func (f flexString) String() string {
	return string(f)
}

func (f flexString) Float() float64 {
	v, _ := strconv.ParseFloat(string(f), 64)
	return v
}

func (f flexString) Bool() bool {
	s := string(f)
	return s == "true" || s == "yes"
}

// SystemResource mirrors the fields of GET /rest/system/resource we use.
type SystemResource struct {
	CPULoad       flexString `json:"cpu-load"`
	FreeMemory    flexString `json:"free-memory"`
	TotalMemory   flexString `json:"total-memory"`
	FreeHDDSpace  flexString `json:"free-hdd-space"`
	TotalHDDSpace flexString `json:"total-hdd-space"`
	Uptime        flexString `json:"uptime"`
	Version       flexString `json:"version"`
	BoardName     flexString `json:"board-name"`
}

// SystemResource fetches GET /rest/system/resource.
func (c *Client) SystemResource() (SystemResource, error) {
	body, err := c.do(http.MethodGet, "system/resource", nil)
	if err != nil {
		return SystemResource{}, err
	}
	return decodeOneOrMany[SystemResource](body)
}

// Interface mirrors the fields of GET /rest/interface we use.
type Interface struct {
	Name     flexString `json:"name"`
	Running  flexString `json:"running"`
	Disabled flexString `json:"disabled"`
	RxByte   flexString `json:"rx-byte"`
	TxByte   flexString `json:"tx-byte"`
	RxPacket flexString `json:"rx-packet"`
	TxPacket flexString `json:"tx-packet"`
	RxError  flexString `json:"rx-error"`
	TxError  flexString `json:"tx-error"`
	RxDrop   flexString `json:"rx-drop"`
	TxDrop   flexString `json:"tx-drop"`
}

// Interfaces fetches GET /rest/interface — the live interface list, so
// callers never need to hardcode names like ether1/bridge/wireguard/wifi1.
func (c *Client) Interfaces() ([]Interface, error) {
	body, err := c.do(http.MethodGet, "interface", nil)
	if err != nil {
		return nil, err
	}
	var ifaces []Interface
	if err := json.Unmarshal(body, &ifaces); err != nil {
		return nil, fmt.Errorf("decoding interface list: %w", err)
	}
	return ifaces, nil
}

// countOnlyReply is the shape of a `print count-only` REST response, e.g.
// {"ret":"33"}.
type countOnlyReply struct {
	Ret flexString `json:"ret"`
}

// printCountOnly POSTs {path}/print with count-only:true and an optional
// .query filter, and returns the count. This deliberately never dumps the
// full table — important for /ip/firewall/connection, whose conntrack table
// can be large on a busy router.
func (c *Client) printCountOnly(path string, query []string) (int, error) {
	reqBody := map[string]any{"count-only": true}
	if len(query) > 0 {
		reqBody[".query"] = query
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	body, err := c.do(http.MethodPost, path+"/print", payload)
	if err != nil {
		return 0, err
	}
	reply, err := decodeOneOrMany[countOnlyReply](body)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(reply.Ret.String()))
	if err != nil {
		return 0, fmt.Errorf("parsing count %q: %w", reply.Ret, err)
	}
	return n, nil
}

// DHCPBoundLeaseCount returns the number of DHCP leases with status=bound.
func (c *Client) DHCPBoundLeaseCount() (int, error) {
	return c.printCountOnly("ip/dhcp-server/lease", []string{"status=bound"})
}

// ConnectionCount returns the number of active /ip/firewall/connection
// entries for the given protocol ("tcp" or "udp").
func (c *Client) ConnectionCount(protocol string) (int, error) {
	return c.printCountOnly("ip/firewall/connection", []string{"protocol=" + protocol})
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
