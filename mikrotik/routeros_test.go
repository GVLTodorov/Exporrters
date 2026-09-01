package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return newClientWithBaseURL(server.URL, "prometheus", "secret", server.Client())
}

func TestFlexStringUnmarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"string", `"123456789"`, "123456789"},
		{"number", `123456789`, "123456789"},
		{"bool true", `true`, "true"},
		{"bool false", `false`, "false"},
		{"empty string", `""`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f flexString
			if err := json.Unmarshal([]byte(tc.json), &f); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.String() != tc.want {
				t.Errorf("got %q, want %q", f.String(), tc.want)
			}
		})
	}
}

func TestFlexStringFloatAndBool(t *testing.T) {
	f := flexString("1503133696")
	if f.Float() != 1503133696 {
		t.Errorf("Float() = %v, want 1503133696", f.Float())
	}

	if !flexString("true").Bool() {
		t.Error(`flexString("true").Bool() = false, want true`)
	}
	if !flexString("yes").Bool() {
		t.Error(`flexString("yes").Bool() = false, want true`)
	}
	if flexString("false").Bool() {
		t.Error(`flexString("false").Bool() = true, want false`)
	}
	if flexString("no").Bool() {
		t.Error(`flexString("no").Bool() = true, want false`)
	}
}

func TestSystemResourceObjectShape(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/system/resource" {
			t.Errorf("path = %q, want /rest/system/resource", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "prometheus" || pass != "secret" {
			t.Errorf("BasicAuth = (%q, %q, %v), want (prometheus, secret, true)", user, pass, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"cpu-load": "3",
			"free-memory": "1503133696",
			"total-memory": "2046820352",
			"free-hdd-space": "83439616",
			"total-hdd-space": "134217728",
			"uptime": "2d20h12m20s",
			"version": "7.23.3 (stable)",
			"board-name": "hAP ax^3"
		}`))
	})

	res, err := client.SystemResource()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.BoardName.String() != "hAP ax^3" {
		t.Errorf("BoardName = %q, want %q", res.BoardName, "hAP ax^3")
	}
	if res.CPULoad.Float() != 3 {
		t.Errorf("CPULoad = %v, want 3", res.CPULoad.Float())
	}
	if res.FreeMemory.Float() != 1503133696 {
		t.Errorf("FreeMemory = %v, want 1503133696", res.FreeMemory.Float())
	}
}

// RouterOS REST is inconsistent across versions about wrapping a singleton
// endpoint's reply in a one-element array; the client must accept both.
func TestSystemResourceArrayShape(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"cpu-load": "5", "board-name": "CCR1016-12S-1S+"}]`))
	})

	res, err := client.SystemResource()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.BoardName.String() != "CCR1016-12S-1S+" {
		t.Errorf("BoardName = %q, want %q", res.BoardName, "CCR1016-12S-1S+")
	}
}

func TestSystemResourceHTTPError(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":401,"message":"Unauthorized"}`))
	})

	if _, err := client.SystemResource(); err == nil {
		t.Fatal("expected an error on HTTP 401")
	}
}

func TestInterfaces(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/interface" {
			t.Errorf("path = %q, want /rest/interface", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name": "ether1", "running": "true", "disabled": "false", "rx-byte": "100", "tx-byte": "200", "rx-packet": "1", "tx-packet": "2", "rx-error": "0", "tx-error": "0", "rx-drop": "0", "tx-drop": "0"},
			{"name": "wireguard1", "running": false, "disabled": true, "rx-byte": 0, "tx-byte": 0, "rx-packet": 0, "tx-packet": 0, "rx-error": 0, "tx-error": 0, "rx-drop": 0, "tx-drop": 0}
		]`))
	})

	ifaces, err := client.Interfaces()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ifaces) != 2 {
		t.Fatalf("len(ifaces) = %d, want 2", len(ifaces))
	}
	if ifaces[0].Name.String() != "ether1" || !ifaces[0].Running.Bool() {
		t.Errorf("ifaces[0] = %+v, want running ether1", ifaces[0])
	}
	if ifaces[1].Name.String() != "wireguard1" || ifaces[1].Running.Bool() || !ifaces[1].Disabled.Bool() {
		t.Errorf("ifaces[1] = %+v, want disabled non-running wireguard1", ifaces[1])
	}
}

func TestDHCPBoundLeaseCount(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/rest/ip/dhcp-server/lease/print" {
			t.Errorf("path = %q, want /rest/ip/dhcp-server/lease/print", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if body["count-only"] != true {
			t.Errorf(`body["count-only"] = %v, want true`, body["count-only"])
		}
		query, _ := body[".query"].([]any)
		if len(query) != 1 || query[0] != "status=bound" {
			t.Errorf(`body[".query"] = %v, want ["status=bound"]`, body[".query"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":"33"}`))
	})

	n, err := client.DHCPBoundLeaseCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 33 {
		t.Errorf("n = %d, want 33", n)
	}
}

func TestConnectionCount(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/ip/firewall/connection/print" {
			t.Errorf("path = %q, want /rest/ip/firewall/connection/print", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		query, _ := body[".query"].([]any)
		if len(query) != 1 || query[0] != "protocol=tcp" {
			t.Errorf(`body[".query"] = %v, want ["protocol=tcp"]`, body[".query"])
		}
		w.Header().Set("Content-Type", "application/json")
		// Also accept the array-wrapped shape here.
		_, _ = w.Write([]byte(`[{"ret":"128"}]`))
	})

	n, err := client.ConnectionCount("tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 128 {
		t.Errorf("n = %d, want 128", n)
	}
}

func TestParseUptime(t *testing.T) {
	cases := []struct {
		in     string
		want   float64
		wantOK bool
	}{
		{"4w3d12h5m54s", 4*7*24*3600 + 3*24*3600 + 12*3600 + 5*60 + 54, true},
		{"2d20h12m20s", 2*24*3600 + 20*3600 + 12*60 + 20, true},
		{"45s", 45, true},
		{"1w", 7 * 24 * 3600, true},
		{"", 0, false},
		{"garbage", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseUptime(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
