// Command arr is a Prometheus exporter for Sonarr, Radarr, Prowlarr and
// Bazarr. One process serves every configured application; each one needs
// only its URL and API key. See README.md for configuration and metrics.
//
// The choice of endpoints and metrics is based on exportarr
// (https://github.com/onedr0p/exportarr, MIT licence, Copyright (c) 2020
// Devin Buhl); see NOTICE.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Seams for tests.
var (
	exit   = os.Exit
	listen = net.Listen
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv); err != nil {
		log.Printf("error: %v", err)
		stop()
		exit(1)
	}
}

// run serves /metrics until ctx is cancelled, then shuts down cleanly.
func run(ctx context.Context, getenv func(string) string) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	ln, err := listen("tcp", cfg.listenAddr)
	if err != nil {
		return err
	}

	exp := newExporter(cfg)
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		exp,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	srv := &http.Server{
		Handler:           newMux(promhttp.HandlerFor(reg, promhttp.HandlerOpts{})),
		ReadHeaderTimeout: 10 * time.Second,
	}

	for _, t := range cfg.targets {
		log.Printf("%s: %s", t.spec.name, t.baseURL)
	}
	log.Printf("listening on %s, refreshing every %s", ln.Addr(), cfg.interval)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		exp.run(ctx, cfg.interval)
	}()
	go func() {
		defer wg.Done()
		<-ctx.Done()
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(shutdownCtx)
	}()

	err = srv.Serve(ln)
	cancel()
	wg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
