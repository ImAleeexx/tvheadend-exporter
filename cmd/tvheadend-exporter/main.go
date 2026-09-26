// Command tvheadend-exporter exposes Tvheadend metrics for Prometheus.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/config"
	"github.com/imaleeexx/tvheadend-exporter/internal/exporter"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return // usage already printed by config.Load
		}
		fmt.Fprintln(os.Stderr, "tvheadend-exporter:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:], os.Getenv, os.ReadFile)
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	ex, err := exporter.New(cfg, log, version, commit)
	if err != nil {
		return err
	}
	if cfg.DumpMetrics {
		fmt.Print(ex.Catalog())
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cctx, cancel := context.WithTimeout(ctx, cfg.Timeout+time.Second)
	err = ex.Check(cctx)
	cancel()
	if err != nil {
		if errors.Is(err, tvh.ErrUnauthorized) {
			return err
		}
		return fmt.Errorf("cannot reach tvheadend at %s: %w", redactURL(cfg.URL), err)
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for range hup {
			ex.Reload()
		}
	}()

	log.Info("starting", "version", version, "commit", commit, "listen", cfg.Listen, "metrics_auth", cfg.MetricsUsername != "")
	return ex.Run(ctx)
}

// redactURL hides any userinfo password embedded in the configured URL.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	return u.Redacted()
}

func newLogger(cfg config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: lvl}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
