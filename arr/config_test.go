package main

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"SONARR_URL":     "192.168.1.10:8989",
		"SONARR_API_KEY": " k1 ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.listenAddr != ":8080" || cfg.interval != 30*time.Second || cfg.timeout != 10*time.Second {
		t.Errorf("defaults = %q %s %s", cfg.listenAddr, cfg.interval, cfg.timeout)
	}
	if len(cfg.targets) != 1 {
		t.Fatalf("%d targets, want 1", len(cfg.targets))
	}
	tg := cfg.targets[0]
	if tg.spec.name != "sonarr" || tg.baseURL != "http://192.168.1.10:8989" || tg.apiKey != "k1" {
		t.Errorf("target = %s %s %q", tg.spec.name, tg.baseURL, tg.apiKey)
	}
}

func TestLoadConfigAllApps(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"LISTEN_ADDR":      "127.0.0.1:9707",
		"REFRESH_INTERVAL": "1m",
		"REQUEST_TIMEOUT":  "5s",
		"SONARR_URL":       "http://sonarr:8989/", "SONARR_API_KEY": "a",
		"RADARR_URL": "https://radarr.lan/radarr/", "RADARR_API_KEY": "b",
		"PROWLARR_URL": "prowlarr:9696", "PROWLARR_API_KEY": "c",
		"BAZARR_URL": "bazarr:6767", "BAZARR_API_KEY": "d",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.listenAddr != "127.0.0.1:9707" || cfg.interval != time.Minute || cfg.timeout != 5*time.Second {
		t.Errorf("got %q %s %s", cfg.listenAddr, cfg.interval, cfg.timeout)
	}
	want := []struct{ name, url, path string }{
		{"sonarr", "http://sonarr:8989", "/api/v3"},
		{"radarr", "https://radarr.lan/radarr", "/api/v3"},
		{"prowlarr", "http://prowlarr:9696", "/api/v1"},
		{"bazarr", "http://bazarr:6767", "/api"},
	}
	if len(cfg.targets) != len(want) {
		t.Fatalf("%d targets, want %d", len(cfg.targets), len(want))
	}
	for i, w := range want {
		g := cfg.targets[i]
		if g.spec.name != w.name || g.baseURL != w.url || g.spec.apiPath != w.path {
			t.Errorf("target %d = %s %s %s, want %v", i, g.spec.name, g.baseURL, g.spec.apiPath, w)
		}
	}
}

func TestLoadConfigErrors(t *testing.T) {
	ok := map[string]string{"RADARR_URL": "radarr:7878", "RADARR_API_KEY": "k"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range ok {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"nothing configured", map[string]string{}, "no application configured"},
		{"key without url", map[string]string{"SONARR_API_KEY": "k"}, "SONARR_API_KEY is set but SONARR_URL is not"},
		{"url without key", map[string]string{"BAZARR_URL": "bazarr:6767"}, "BAZARR_URL is set but BAZARR_API_KEY is not"},
		{"bad url", map[string]string{"SONARR_URL": "http://[::1", "SONARR_API_KEY": "k"}, "SONARR_URL:"},
		{"bad interval", with(map[string]string{"REFRESH_INTERVAL": "soon"}), "REFRESH_INTERVAL"},
		{"zero interval", with(map[string]string{"REFRESH_INTERVAL": "0s"}), "REFRESH_INTERVAL must be positive"},
		{"bad timeout", with(map[string]string{"REQUEST_TIMEOUT": "-1s"}), "REQUEST_TIMEOUT must be positive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfig(env(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.10:8989":           "http://192.168.1.10:8989",
		"http://192.168.1.10:8989/":   "http://192.168.1.10:8989",
		"https://arr.example/sonarr/": "https://arr.example/sonarr",
		"sonarr":                      "http://sonarr",
	} {
		got, err := normalizeURL(in)
		if err != nil || got != want {
			t.Errorf("normalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for in, want := range map[string]string{
		"ftp://host":         "unsupported scheme",
		"http://":            "missing host",
		"http://h:1/?a=b":    "query or fragment",
		"http://h:1/#frag":   "query or fragment",
		"http://[::1":        "missing ']'",
	} {
		if _, err := normalizeURL(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("normalizeURL(%q) err = %v, want containing %q", in, err, want)
		}
	}
}
