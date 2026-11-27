package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// appSpec describes one supported application: its env-var prefix, the API
// root under its base URL and the function that turns its API into metrics.
type appSpec struct {
	name    string // also the value of the "app" label
	apiPath string
	collect collectFunc
}

// specs lists every supported application, in the order they are logged.
var specs = []appSpec{
	{name: "sonarr", apiPath: "/api/v3", collect: collectSonarr},
	{name: "radarr", apiPath: "/api/v3", collect: collectRadarr},
	{name: "prowlarr", apiPath: "/api/v1", collect: collectProwlarr},
	{name: "bazarr", apiPath: "/api", collect: collectBazarr},
}

// target is one configured application instance.
type target struct {
	spec    appSpec
	baseURL string // normalised, without trailing slash or API path
	apiKey  string
}

type config struct {
	listenAddr string
	interval   time.Duration
	timeout    time.Duration
	targets    []target
}

const (
	defaultListenAddr = ":8080"
	defaultInterval   = 30 * time.Second
	defaultTimeout    = 10 * time.Second
)

// loadConfig reads the configuration from environment variables. getenv is
// os.Getenv in production and a map lookup in tests.
//
// Each application is enabled by setting both <APP>_URL and <APP>_API_KEY
// (for example SONARR_URL and SONARR_API_KEY). Setting only one of the two
// is an error, so a typo doesn't silently disable an app. At least one
// application must be configured.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		listenAddr: defaultListenAddr,
		interval:   defaultInterval,
		timeout:    defaultTimeout,
	}
	if v := strings.TrimSpace(getenv("LISTEN_ADDR")); v != "" {
		cfg.listenAddr = v
	}
	var err error
	if cfg.interval, err = durationEnv(getenv, "REFRESH_INTERVAL", defaultInterval); err != nil {
		return config{}, err
	}
	if cfg.timeout, err = durationEnv(getenv, "REQUEST_TIMEOUT", defaultTimeout); err != nil {
		return config{}, err
	}

	for _, s := range specs {
		prefix := strings.ToUpper(s.name)
		rawURL := strings.TrimSpace(getenv(prefix + "_URL"))
		key := strings.TrimSpace(getenv(prefix + "_API_KEY"))
		switch {
		case rawURL == "" && key == "":
			continue
		case rawURL == "":
			return config{}, fmt.Errorf("%s_API_KEY is set but %s_URL is not", prefix, prefix)
		case key == "":
			return config{}, fmt.Errorf("%s_URL is set but %s_API_KEY is not", prefix, prefix)
		}
		base, err := normalizeURL(rawURL)
		if err != nil {
			return config{}, fmt.Errorf("%s_URL: %w", prefix, err)
		}
		cfg.targets = append(cfg.targets, target{spec: s, baseURL: base, apiKey: key})
	}
	if len(cfg.targets) == 0 {
		return config{}, fmt.Errorf("no application configured: set <APP>_URL and <APP>_API_KEY for at least one of SONARR, RADARR, PROWLARR, BAZARR")
	}
	return cfg, nil
}

// durationEnv parses a positive Go duration ("30s", "2m") from name, or
// returns def when it is unset.
func durationEnv(getenv func(string) string, name string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %s", name, v)
	}
	return d, nil
}

// normalizeURL accepts "192.168.1.10:8989", "http://host:8989" or
// "https://host/sonarr/" and returns the base URL without a trailing slash.
// A missing scheme means http.
func normalizeURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q (use http or https)", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host in %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("query or fragment not allowed in %q", raw)
	}
	return strings.TrimRight(u.String(), "/"), nil
}
