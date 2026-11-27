package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClientSendsKeyInHeaderAndEncodesQuery(t *testing.T) {
	var gotPath, gotQuery, gotKey, gotAccept string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotKey, gotAccept = r.Header.Get("X-Api-Key"), r.Header.Get("Accept")
		_, _ = io.WriteString(w, `{"version":"1"}`)
	}), "/api/v3")

	var out struct{ Version string }
	if err := c.get(t.Context(), "/system/status", url.Values{"page": {"2"}, "a b": {"c&d"}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != "1" {
		t.Errorf("decoded %+v", out)
	}
	if gotPath != "/api/v3/system/status" || gotQuery != "a+b=c%26d&page=2" {
		t.Errorf("request = %s?%s", gotPath, gotQuery)
	}
	if gotKey != testKey || gotAccept != "application/json" {
		t.Errorf("headers: key=%q accept=%q", gotKey, gotAccept)
	}
}

func TestClientErrors(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
		case "/html":
			_, _ = io.WriteString(w, "<html>login</html>")
		}
	}), "")
	var out any
	for path, want := range map[string]string{
		"/unauthorized": "/unauthorized: unexpected status 401 Unauthorized",
		"/html":         "/html: decoding response",
	} {
		err := c.get(t.Context(), path, nil, &out)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("get %s: err = %v, want prefix %q", path, err, want)
		}
		if err != nil && strings.Contains(err.Error(), testKey) {
			t.Errorf("error leaks the API key: %v", err)
		}
	}
}

func TestClientBadURL(t *testing.T) {
	c := &apiClient{http: http.DefaultClient, apiURL: "http://bad\x7f", apiKey: testKey}
	var out any
	if err := c.get(t.Context(), "/x", nil, &out); err == nil || !strings.Contains(err.Error(), "invalid control character") {
		t.Errorf("err = %v, want an URL parse error", err)
	}
}

func TestClientConnectionRefused(t *testing.T) {
	// A port nobody listens on: start a server and close it straight away.
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	c := &apiClient{http: &http.Client{Timeout: 5 * time.Second}, apiURL: srv.URL, apiKey: testKey}
	var out any
	if err := c.get(t.Context(), "/system/status", nil, &out); err == nil || !strings.HasPrefix(err.Error(), "/system/status: ") {
		t.Errorf("err = %v, want a dial error prefixed with the path", err)
	}
}

func TestClientRejectsOversizedBody(t *testing.T) {
	old := maxBodyBytes
	maxBodyBytes = 1 << 10
	t.Cleanup(func() { maxBodyBytes = old })

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `["`+strings.Repeat("x", 4<<10)+`"]`)
	}), "")
	var out []string
	err := c.get(t.Context(), "/movie", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "response larger than 1024 bytes") {
		t.Errorf("err = %v, want a size-limit error", err)
	}
}
