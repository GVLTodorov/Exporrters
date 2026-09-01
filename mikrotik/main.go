package main

import (
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	scheme := "https"
	if !cfg.UseHTTPS {
		scheme = "http"
	}
	log.Printf(
		"Target: %s://%s (user=%s, insecure_skip_verify=%v, fetch_interval=%v)",
		scheme, cfg.Address, cfg.User, cfg.InsecureSkipVerify, cfg.FetchInterval,
	)

	registerMetrics()

	client := newClient(cfg)

	go func() {
		collectOnce(client)
		ticker := time.NewTicker(cfg.FetchInterval)
		defer ticker.Stop()
		for range ticker.C {
			collectOnce(client)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	addr := ":" + cfg.ListenPort
	log.Printf("Listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
