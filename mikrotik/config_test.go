package main

import (
	"testing"
	"time"
)

// clearMikrotikEnv ensures no leftover env var from another test (or the
// developer's shell) leaks into a case that expects the default.
func clearMikrotikEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"MIKROTIK_ADDRESS", "MIKROTIK_USER", "MIKROTIK_PASSWORD",
		"MIKROTIK_API_PORT", "MIKROTIK_USE_TLS", "MIKROTIK_INSECURE_SKIP_VERIFY",
		"LISTEN_PORT", "FETCH_INTERVAL", "MIKROTIK_TIMEOUT",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadConfigRequiresAddress(t *testing.T) {
	clearMikrotikEnv(t)
	t.Setenv("MIKROTIK_PASSWORD", "secret")

	if _, err := loadConfig(); err == nil {
		t.Fatal("expected an error when MIKROTIK_ADDRESS is missing")
	}
}

func TestLoadConfigRequiresPassword(t *testing.T) {
	clearMikrotikEnv(t)
	t.Setenv("MIKROTIK_ADDRESS", "192.168.1.1")

	if _, err := loadConfig(); err == nil {
		t.Fatal("expected an error when MIKROTIK_PASSWORD is missing")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	clearMikrotikEnv(t)
	t.Setenv("MIKROTIK_ADDRESS", "192.168.1.1")
	t.Setenv("MIKROTIK_PASSWORD", "secret")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.User != "prometheus" {
		t.Errorf("User = %q, want %q", cfg.User, "prometheus")
	}
	if cfg.ListenPort != "8080" {
		t.Errorf("ListenPort = %q, want %q", cfg.ListenPort, "8080")
	}
	if cfg.APIPort != "8728" {
		t.Errorf("APIPort = %q, want %q (default plaintext api)", cfg.APIPort, "8728")
	}
	if cfg.UseTLS {
		t.Error("UseTLS = true, want false (default)")
	}
	if !cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false, want true (default)")
	}
	if cfg.FetchInterval != 15*time.Second {
		t.Errorf("FetchInterval = %v, want %v", cfg.FetchInterval, 15*time.Second)
	}
	if cfg.RequestTimeout != 10*time.Second {
		t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 10*time.Second)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	clearMikrotikEnv(t)
	t.Setenv("MIKROTIK_ADDRESS", "10.0.0.1")
	t.Setenv("MIKROTIK_PASSWORD", "secret")
	t.Setenv("MIKROTIK_USER", "admin")
	t.Setenv("MIKROTIK_USE_TLS", "true")
	t.Setenv("MIKROTIK_INSECURE_SKIP_VERIFY", "false")
	t.Setenv("LISTEN_PORT", "9090")
	t.Setenv("FETCH_INTERVAL", "30s")
	t.Setenv("MIKROTIK_TIMEOUT", "5s")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.User != "admin" {
		t.Errorf("User = %q, want %q", cfg.User, "admin")
	}
	if !cfg.UseTLS {
		t.Error("UseTLS = false, want true")
	}
	if cfg.APIPort != "8729" {
		t.Errorf("APIPort = %q, want %q (default api-ssl when UseTLS)", cfg.APIPort, "8729")
	}
	if cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = true, want false")
	}
	if cfg.ListenPort != "9090" {
		t.Errorf("ListenPort = %q, want %q", cfg.ListenPort, "9090")
	}
	if cfg.FetchInterval != 30*time.Second {
		t.Errorf("FetchInterval = %v, want %v", cfg.FetchInterval, 30*time.Second)
	}
	if cfg.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 5*time.Second)
	}
}

func TestLoadConfigExplicitAPIPortOverridesTLSDefault(t *testing.T) {
	clearMikrotikEnv(t)
	t.Setenv("MIKROTIK_ADDRESS", "10.0.0.1")
	t.Setenv("MIKROTIK_PASSWORD", "secret")
	t.Setenv("MIKROTIK_API_PORT", "12345")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIPort != "12345" {
		t.Errorf("APIPort = %q, want %q", cfg.APIPort, "12345")
	}
}

func TestLoadConfigInvalidFetchIntervalFallsBackToDefault(t *testing.T) {
	clearMikrotikEnv(t)
	t.Setenv("MIKROTIK_ADDRESS", "10.0.0.1")
	t.Setenv("MIKROTIK_PASSWORD", "secret")
	t.Setenv("FETCH_INTERVAL", "not-a-duration")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FetchInterval != 15*time.Second {
		t.Errorf("FetchInterval = %v, want default %v", cfg.FetchInterval, 15*time.Second)
	}
}

func TestLoadConfigNegativeFetchIntervalFallsBackToDefault(t *testing.T) {
	clearMikrotikEnv(t)
	t.Setenv("MIKROTIK_ADDRESS", "10.0.0.1")
	t.Setenv("MIKROTIK_PASSWORD", "secret")
	t.Setenv("FETCH_INTERVAL", "-5s")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FetchInterval != 15*time.Second {
		t.Errorf("FetchInterval = %v, want default %v", cfg.FetchInterval, 15*time.Second)
	}
}
