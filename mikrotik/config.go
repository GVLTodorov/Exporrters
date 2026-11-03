package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

// Config holds everything read from the environment at startup.
type Config struct {
	Address            string
	APIPort            string
	User               string
	Password           string
	UseTLS             bool
	InsecureSkipVerify bool
	ListenPort         string
	FetchInterval      time.Duration
	RequestTimeout     time.Duration
}

// loadConfig reads MIKROTIK_* / LISTEN_PORT / FETCH_INTERVAL from the
// environment. MIKROTIK_ADDRESS and MIKROTIK_PASSWORD are required; everything
// else has a sane default so a home-router deployment only needs those two
// plus MIKROTIK_USER.
func loadConfig() (Config, error) {
	address := os.Getenv("MIKROTIK_ADDRESS")
	if address == "" {
		return Config{}, fmt.Errorf("MIKROTIK_ADDRESS is required")
	}

	password := os.Getenv("MIKROTIK_PASSWORD")
	if password == "" {
		return Config{}, fmt.Errorf("MIKROTIK_PASSWORD is required")
	}

	user := os.Getenv("MIKROTIK_USER")
	if user == "" {
		user = "prometheus"
	}

	listenPort := os.Getenv("LISTEN_PORT")
	if listenPort == "" {
		listenPort = "8080"
	}

	useTLS := parseBoolEnv("MIKROTIK_USE_TLS", false)

	apiPort := os.Getenv("MIKROTIK_API_PORT")
	if apiPort == "" {
		if useTLS {
			apiPort = "8729" // api-ssl
		} else {
			apiPort = "8728" // api
		}
	}

	return Config{
		Address:            address,
		APIPort:            apiPort,
		User:               user,
		Password:           password,
		UseTLS:             useTLS,
		InsecureSkipVerify: parseBoolEnv("MIKROTIK_INSECURE_SKIP_VERIFY", true),
		ListenPort:         listenPort,
		FetchInterval:      parseDurationEnv("FETCH_INTERVAL", 15*time.Second),
		RequestTimeout:     parseDurationEnv("MIKROTIK_TIMEOUT", 10*time.Second),
	}, nil
}

func parseBoolEnv(name string, def bool) bool {
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		log.Printf("Invalid %s=%q (%v); using default %v", name, s, err, def)
		return def
	}
	return v
}

func parseDurationEnv(name string, def time.Duration) time.Duration {
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		log.Printf("Invalid %s=%q (%v); using default %v", name, s, err, def)
		return def
	}
	return d
}
