package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// captureListen makes run listen on a free loopback port and reports the
// address it got.
func captureListen(t *testing.T) <-chan string {
	t.Helper()
	addrs := make(chan string, 1)
	old := listen
	listen = func(network, _ string) (net.Listener, error) {
		ln, err := old(network, "127.0.0.1:0")
		if err == nil {
			addrs <- ln.Addr().String()
		}
		return ln, err
	}
	t.Cleanup(func() { listen = old })
	return addrs
}

func TestRunServesMetricsAndShutsDown(t *testing.T) {
	_, base := allAppsServer(t)
	addrs := captureListen(t)
	e := allAppsEnv(base)
	e["REFRESH_INTERVAL"] = "50ms"

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() { errc <- run(ctx, env(e)) }()

	addr := <-addrs
	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	var body string
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(body, `arr_up{app="bazarr"} 1`) || !strings.Contains(body, `arr_up{app="sonarr"} 1`) {
		if time.Now().After(deadline) {
			t.Fatalf("metrics never showed every app up:\n%s", body)
		}
		resp, err := client.Get("http://" + addr + "/")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body = string(b)
		time.Sleep(5 * time.Millisecond)
	}
	for _, want := range []string{"sonarr_series 3", "go_goroutines", "process_"} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	if strings.Contains(body, testKey) {
		t.Error("metrics leak the API key")
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("run returned %v after cancel, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not stop after cancel")
	}
}

func TestRunConfigError(t *testing.T) {
	if err := run(t.Context(), env(nil)); err == nil || !strings.Contains(err.Error(), "no application configured") {
		t.Errorf("err = %v", err)
	}
}

func TestRunListenError(t *testing.T) {
	e := map[string]string{"SONARR_URL": "127.0.0.1:1", "SONARR_API_KEY": "k", "LISTEN_ADDR": "127.0.0.1:not-a-port"}
	if err := run(t.Context(), env(e)); err == nil {
		t.Error("run with an invalid LISTEN_ADDR returned nil")
	}
}

// brokenListener fails every Accept, so http.Server.Serve returns at once.
type brokenListener struct{ net.Listener }

var errAccept = errors.New("accept failed")

func (brokenListener) Accept() (net.Conn, error) { return nil, errAccept }

func TestRunServeError(t *testing.T) {
	old := listen
	listen = func(network, addr string) (net.Listener, error) {
		ln, err := old(network, "127.0.0.1:0")
		return brokenListener{ln}, err
	}
	t.Cleanup(func() { listen = old })

	e := map[string]string{"SONARR_URL": "127.0.0.1:1", "SONARR_API_KEY": "k", "REQUEST_TIMEOUT": "100ms"}
	if err := run(t.Context(), env(e)); !errors.Is(err, errAccept) {
		t.Errorf("err = %v, want the accept error", err)
	}
}

func TestMainExitsOnBadConfig(t *testing.T) {
	for _, s := range specs {
		t.Setenv(strings.ToUpper(s.name)+"_URL", "")
		t.Setenv(strings.ToUpper(s.name)+"_API_KEY", "")
	}
	code := -1
	old := exit
	exit = func(c int) { code = c }
	t.Cleanup(func() { exit = old })

	main()
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
