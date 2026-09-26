// Package exporter wires config, client, collectors, pollers and HTTP together.
package exporter

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/collector"
	"github.com/imaleeexx/tvheadend-exporter/internal/config"
	"github.com/imaleeexx/tvheadend-exporter/internal/geoip"
	"github.com/imaleeexx/tvheadend-exporter/internal/poller"
	"github.com/imaleeexx/tvheadend-exporter/internal/sessions"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// serverPollInterval is how often serverinfo is refreshed after startup.
const serverPollInterval = 5 * time.Minute

// Exporter is the running application.
type Exporter struct {
	cfg    config.Config
	log    *slog.Logger
	reg    *recordingRegistry
	client *tvh.Client
	geo    geoip.Resolver

	status   *collector.Status
	inputs   *collector.Inputs
	topology *collector.Topology
	dvr      *collector.DVR
	server   *collector.Server
	self     *collector.Self

	mu           sync.Mutex   // serialises collector updates
	lastStatusOK atomic.Int64 // UnixNano of the last successful status poll; 0 = never
}

// New builds the registry, API client and collectors. It performs no network
// I/O; call Check to verify connectivity and credentials.
func New(cfg config.Config, log *slog.Logger, version, commit string) (*Exporter, error) {
	reg := newRecordingRegistry()
	client, err := tvh.New(cfg.URL, cfg.Username, cfg.Password, cfg.Timeout, cfg.TLSInsecureSkipVerify, reg)
	if err != nil {
		return nil, err
	}
	var geo geoip.Resolver = geoip.Noop{}
	if cfg.GeoIPDB != "" {
		mm, err := geoip.Open(cfg.GeoIPDB, reg)
		if err != nil {
			return nil, fmt.Errorf("open geoip db: %w", err)
		}
		geo = mm
	}
	e := &Exporter{cfg: cfg, log: log, reg: reg, client: client, geo: geo,
		status:   collector.NewStatus(reg, sessions.New(cfg.SessionGrace), geo, log),
		inputs:   collector.NewInputs(reg),
		topology: collector.NewTopology(reg),
		dvr:      collector.NewDVR(reg),
		server:   collector.NewServer(reg),
		self:     collector.NewSelf(reg, version, commit),
	}
	// Runtime collectors go on the underlying registry so they are served on
	// /metrics but stay out of the catalogue (which must not depend on the
	// Go version or OS).
	reg.Registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return e, nil
}

// Check performs the startup connectivity/credential check (serverinfo).
func (e *Exporter) Check(ctx context.Context) error {
	info, err := e.client.ServerInfo(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.server.Update(info)
	e.mu.Unlock()
	e.log.Info("connected to tvheadend", "version", info.SWVersion, "api_version", info.APIVersion.Int64(), "name", info.Name)
	return nil
}

// Catalog returns the markdown metric catalogue.
func (e *Exporter) Catalog() string { return Catalog(e.reg) }

// Reload re-opens the GeoIP database if configured (SIGHUP).
func (e *Exporter) Reload() {
	mm, ok := e.geo.(*geoip.MaxMind)
	if !ok {
		e.log.Info("reload requested; no geoip database configured")
		return
	}
	if err := mm.Reload(); err != nil {
		e.log.Error("geoip reload failed", "err", err)
		return
	}
	e.log.Info("geoip database reloaded")
}

// pollStatus is all-or-nothing: collectors are updated only if all three
// status endpoints succeed.
func (e *Exporter) pollStatus(ctx context.Context) error {
	subs, err := e.client.Subscriptions(ctx)
	if err == nil {
		var conns []tvh.Connection
		if conns, err = e.client.Connections(ctx); err == nil {
			var ins []tvh.Input
			if ins, err = e.client.Inputs(ctx); err == nil {
				e.mu.Lock()
				e.status.Update(subs, conns, time.Now())
				e.inputs.Update(ins)
				e.server.SetUp(true)
				e.mu.Unlock()
				e.lastStatusOK.Store(time.Now().UnixNano())
				return nil
			}
		}
	}
	e.mu.Lock()
	e.status.Fail(time.Now())
	e.server.SetUp(false)
	e.mu.Unlock()
	return err
}

// pollTopology is all-or-nothing across topology, access and DVR endpoints.
func (e *Exporter) pollTopology(ctx context.Context) error {
	nets, err := e.client.Networks(ctx)
	if err != nil {
		return err
	}
	muxes, err := e.client.Muxes(ctx)
	if err != nil {
		return err
	}
	svcs, err := e.client.Services(ctx)
	if err != nil {
		return err
	}
	chans, err := e.client.Channels(ctx)
	if err != nil {
		return err
	}
	tags, err := e.client.ChannelTags(ctx)
	if err != nil {
		return err
	}
	access, err := e.client.AccessEntries(ctx)
	if err != nil {
		return err
	}
	entries, err := e.client.DVREntries(ctx)
	if err != nil {
		return err
	}
	cfgs, err := e.client.DVRConfigs(ctx)
	if err != nil {
		return err
	}
	autorec, err := e.client.DVRAutorecCount(ctx)
	if err != nil {
		return err
	}
	timerec, err := e.client.DVRTimerecCount(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.topology.Update(nets, muxes, svcs, chans, tags)
	e.status.UpdateAccess(access)
	e.dvr.Update(entries, cfgs, autorec, timerec)
	return nil
}

func (e *Exporter) pollServer(ctx context.Context) error {
	info, err := e.client.ServerInfo(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.server.Update(info)
	return nil
}

// Handler serves /metrics, /healthz, /-/ready and a landing page.
func (e *Exporter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", e.requireAuth(promhttp.HandlerFor(e.reg, promhttp.HandlerOpts{})))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		n := e.lastStatusOK.Load()
		if n == 0 {
			http.Error(w, "no successful status poll yet", http.StatusServiceUnavailable)
			return
		}
		age := time.Since(time.Unix(0, n))
		if age > 3*e.cfg.PollStatus {
			http.Error(w, fmt.Sprintf("last successful status poll %s ago", age.Truncate(time.Second)), http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/-/ready", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprintln(w, "ok") })
	mux.Handle("/", e.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><head><title>Tvheadend Exporter</title></head><body><h1>Tvheadend Exporter</h1><p><a href="/metrics">Metrics</a> · <a href="/healthz">Health</a></p></body></html>`)
	})))
	return mux
}

// requireAuth wraps h with HTTP basic auth when metrics credentials are
// configured; otherwise it returns h unchanged. Health probes are not wrapped.
func (e *Exporter) requireAuth(h http.Handler) http.Handler {
	if e.cfg.MetricsUsername == "" {
		return h
	}
	wantUser := sha256.Sum256([]byte(e.cfg.MetricsUsername))
	wantPass := sha256.Sum256([]byte(e.cfg.MetricsPassword))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		gotUser := sha256.Sum256([]byte(user))
		gotPass := sha256.Sum256([]byte(pass))
		userOK := subtle.ConstantTimeCompare(gotUser[:], wantUser[:]) == 1
		passOK := subtle.ConstantTimeCompare(gotPass[:], wantPass[:]) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="tvheadend-exporter", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Run starts pollers and the HTTP server. It returns after ctx is done (nil)
// or the HTTP server fails (that error), once pollers and shutdown complete.
func (e *Exporter) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	srv := &http.Server{Addr: e.cfg.Listen, Handler: e.Handler(), ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		e.log.Info("listening", "addr", e.cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	var wg sync.WaitGroup
	run := func(group string, every time.Duration, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			poller.Run(ctx, group, every, e.self, e.log, fn)
		}()
	}
	run("status", e.cfg.PollStatus, e.pollStatus)
	run("topology", e.cfg.PollTopology, e.pollTopology)
	run("server", serverPollInterval, e.pollServer)

	var err error
	select {
	case <-ctx.Done():
	case err = <-errCh:
		e.log.Error("http server failed", "err", err)
	}
	cancel() // stop pollers even when the server failed
	wg.Wait()

	e.mu.Lock()
	e.status.Shutdown(time.Now())
	e.mu.Unlock()

	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	_ = srv.Shutdown(sctx)
	if mm, ok := e.geo.(*geoip.MaxMind); ok {
		_ = mm.Close()
	}
	return err
}
