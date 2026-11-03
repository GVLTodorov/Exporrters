package main

import (
	"errors"
	"testing"

	ros "github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
)

// stubRunner lets tests exercise the sentence-mapping logic in routeros.go
// without speaking the real RouterOS binary protocol. It records the
// sentence(s) it was called with and returns a canned reply (or error) keyed
// by the command word (sentence[0]).
type stubRunner struct {
	replies map[string]*ros.Reply
	errs    map[string]error
	calls   [][]string
}

func (s *stubRunner) Run(sentence ...string) (*ros.Reply, error) {
	s.calls = append(s.calls, sentence)
	cmd := sentence[0]
	if err, ok := s.errs[cmd]; ok {
		return nil, err
	}
	if r, ok := s.replies[cmd]; ok {
		return r, nil
	}
	return &ros.Reply{}, nil
}

func sentence(m map[string]string) *proto.Sentence {
	return &proto.Sentence{Word: "!re", Map: m}
}

func TestFetchSystemResource(t *testing.T) {
	stub := &stubRunner{replies: map[string]*ros.Reply{
		"/system/resource/print": {Re: []*proto.Sentence{sentence(map[string]string{
			"cpu-load":        "3",
			"free-memory":     "1503133696",
			"total-memory":    "2046820352",
			"free-hdd-space":  "83439616",
			"total-hdd-space": "134217728",
			"uptime":          "2d20h12m20s",
			"version":         "7.23.3 (stable)",
			"board-name":      "hAP ax^3",
		})}},
	}}

	res, err := fetchSystemResource(stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.BoardName != "hAP ax^3" {
		t.Errorf("BoardName = %q, want %q", res.BoardName, "hAP ax^3")
	}
	if parseFloat(res.CPULoad) != 3 {
		t.Errorf("CPULoad = %v, want 3", parseFloat(res.CPULoad))
	}
	if parseFloat(res.FreeMemory) != 1503133696 {
		t.Errorf("FreeMemory = %v, want 1503133696", parseFloat(res.FreeMemory))
	}
}

func TestFetchSystemResourceEmptyReply(t *testing.T) {
	stub := &stubRunner{replies: map[string]*ros.Reply{
		"/system/resource/print": {},
	}}
	if _, err := fetchSystemResource(stub); err == nil {
		t.Fatal("expected an error on an empty reply")
	}
}

func TestFetchSystemResourceRunError(t *testing.T) {
	stub := &stubRunner{errs: map[string]error{
		"/system/resource/print": errors.New("connection reset"),
	}}
	if _, err := fetchSystemResource(stub); err == nil {
		t.Fatal("expected an error when Run fails")
	}
}

func TestFetchInterfaces(t *testing.T) {
	stub := &stubRunner{replies: map[string]*ros.Reply{
		"/interface/print": {Re: []*proto.Sentence{
			sentence(map[string]string{
				"name": "ether1", "running": "true", "disabled": "false",
				"rx-byte": "100", "tx-byte": "200", "rx-packet": "1", "tx-packet": "2",
				"rx-error": "0", "tx-error": "0", "rx-drop": "0", "tx-drop": "0",
			}),
			sentence(map[string]string{
				"name": "wireguard1", "running": "false", "disabled": "true",
				"rx-byte": "0", "tx-byte": "0", "rx-packet": "0", "tx-packet": "0",
				"rx-error": "0", "tx-error": "0", "rx-drop": "0", "tx-drop": "0",
			}),
		}},
	}}

	ifaces, err := fetchInterfaces(stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ifaces) != 2 {
		t.Fatalf("len(ifaces) = %d, want 2", len(ifaces))
	}
	if ifaces[0].Name != "ether1" || !parseBool(ifaces[0].Running) {
		t.Errorf("ifaces[0] = %+v, want running ether1", ifaces[0])
	}
	if ifaces[1].Name != "wireguard1" || parseBool(ifaces[1].Running) || !parseBool(ifaces[1].Disabled) {
		t.Errorf("ifaces[1] = %+v, want disabled non-running wireguard1", ifaces[1])
	}
}

func TestFetchDHCPBoundLeaseCount(t *testing.T) {
	stub := &stubRunner{replies: map[string]*ros.Reply{
		"/ip/dhcp-server/lease/print": {Re: []*proto.Sentence{sentence(map[string]string{"ret": "33"})}},
	}}

	n, err := fetchDHCPBoundLeaseCount(stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 33 {
		t.Errorf("n = %d, want 33", n)
	}

	got := stub.calls[0]
	want := []string{"/ip/dhcp-server/lease/print", "?status=bound", "=count-only="}
	if len(got) != len(want) {
		t.Fatalf("sentence = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sentence[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFetchConnectionCount(t *testing.T) {
	stub := &stubRunner{replies: map[string]*ros.Reply{
		// count-only can also come back on the !done sentence rather than !re.
		"/ip/firewall/connection/print": {Done: sentence(map[string]string{"ret": "128"})},
	}}

	n, err := fetchConnectionCount(stub, "tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 128 {
		t.Errorf("n = %d, want 128", n)
	}

	got := stub.calls[0]
	want := []string{"/ip/firewall/connection/print", "?protocol=tcp", "=count-only="}
	if len(got) != len(want) {
		t.Fatalf("sentence = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sentence[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFetchHealth(t *testing.T) {
	stub := &stubRunner{replies: map[string]*ros.Reply{
		"/system/health/print": {Re: []*proto.Sentence{
			sentence(map[string]string{".id": "*D", "name": "cpu-temperature", "value": "44", "type": "C"}),
			sentence(map[string]string{".id": "*E", "name": "voltage", "value": "24.1", "type": "V"}),
		}},
	}}

	sensors, err := fetchHealth(stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []HealthSensor{
		{Name: "cpu-temperature", Value: "44", Type: "C"},
		{Name: "voltage", Value: "24.1", Type: "V"},
	}
	if len(sensors) != len(want) {
		t.Fatalf("sensors = %+v, want %+v", sensors, want)
	}
	for i := range want {
		if sensors[i] != want[i] {
			t.Errorf("sensors[%d] = %+v, want %+v", i, sensors[i], want[i])
		}
	}
}

func TestFetchHealthNoSensors(t *testing.T) {
	// Boards without health sensors (e.g. CHR) reply with just !done.
	sensors, err := fetchHealth(&stubRunner{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sensors) != 0 {
		t.Errorf("sensors = %+v, want none", sensors)
	}
}

func TestFetchHealthRunError(t *testing.T) {
	stub := &stubRunner{errs: map[string]error{
		"/system/health/print": errors.New("not enough permissions"),
	}}
	if _, err := fetchHealth(stub); err == nil {
		t.Fatal("expected an error when Run fails")
	}
}

func TestCountOnlyMissingRet(t *testing.T) {
	stub := &stubRunner{replies: map[string]*ros.Reply{
		"/ip/firewall/connection/print": {Re: []*proto.Sentence{sentence(map[string]string{})}},
	}}
	if _, err := fetchConnectionCount(stub, "udp"); err == nil {
		t.Fatal("expected an error when the reply has no ret field")
	}
}

func TestParseFloatAndBool(t *testing.T) {
	if parseFloat("1503133696") != 1503133696 {
		t.Errorf("parseFloat(1503133696) = %v, want 1503133696", parseFloat("1503133696"))
	}
	if parseFloat("not-a-number") != 0 {
		t.Errorf("parseFloat(garbage) = %v, want 0", parseFloat("not-a-number"))
	}
	if !parseBool("true") || !parseBool("yes") {
		t.Error("parseBool(true/yes) = false, want true")
	}
	if parseBool("false") || parseBool("no") || parseBool("") {
		t.Error("parseBool(false/no/empty) = true, want false")
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
