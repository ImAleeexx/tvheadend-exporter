package exporter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/config"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

var fixtures = map[string]string{
	"serverinfo":           "serverinfo.json",
	"status/subscriptions": "subscriptions.json",
	"status/connections":   "connections.json",
	"status/inputs":        "inputs.json",
	"mpegts/network/grid":  "networks.json",
	"mpegts/mux/grid":      "muxes.json",
	"mpegts/service/grid":  "services.json",
	"channel/grid":         "channels.json",
	"channeltag/grid":      "channeltags.json",
	"access/entry/grid":    "access.json",
	"dvr/entry/grid":       "dvr_entries.json",
	"dvr/config/grid":      "dvr_configs.json",
	"dvr/autorec/grid":     "empty_grid.json",
	"dvr/timerec/grid":     "empty_grid.json",
}

// fakeTVH serves fixtures; while down is set every request returns 500.
type fakeTVH struct {
	*httptest.Server
	down atomic.Bool
}

func newFakeTVH(t *testing.T) *fakeTVH {
	t.Helper()
	f := &fakeTVH{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("non-GET request %s %s", r.Method, r.URL.Path)
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "user" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f.down.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		name, ok := fixtures[strings.TrimPrefix(r.URL.Path, "/api/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, err := os.ReadFile(filepath.Join("..", "testdata", name))
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.Close)
	return f
}

func testConfig(url string) config.Config {
	return config.Config{
		URL: url, Username: "user", Password: "pw",
		PollStatus: time.Second, PollTopology: time.Second,
		Timeout: time.Second, SessionGrace: time.Minute,
		Listen: "127.0.0.1:0", LogFormat: "json", LogLevel: "info",
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestExporter(t *testing.T, url string) *Exporter {
	t.Helper()
	e, err := New(testConfig(url), discard(), "v-test", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func TestNew_CatalogCoversCollectorsButNotRuntime(t *testing.T) {
	e := newTestExporter(t, "http://127.0.0.1:1")
	md := e.Catalog()
	for _, want := range []string{
		"| `tvheadend_up` | gauge |",
		"| `tvheadend_session_duration_seconds` | histogram |",
		"| `tvheadend_exporter_api_request_duration_seconds` | histogram |",
		"| `tvheadend_exporter_poll_errors_total` | counter | `group` |",
		"`tvheadend_input_info`",
		"`tvheadend_exporter_build_info`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("catalogue missing %q", want)
		}
	}
	for _, bad := range []string{"`go_", "`process_"} {
		if strings.Contains(md, bad) {
			t.Errorf("catalogue must not contain runtime metrics %q", bad)
		}
	}
	// ...but the runtime collectors are still served on /metrics.
	_, body := get(t, e.Handler(), "/metrics")
	if !strings.Contains(body, "go_goroutines") {
		t.Error("/metrics should expose Go runtime metrics")
	}
}

func TestCheck(t *testing.T) {
	f := newFakeTVH(t)
	e := newTestExporter(t, f.URL)
	if err := e.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, e.Handler(), "/metrics")
	if !strings.Contains(body, "tvheadend_build_info{") {
		t.Error("Check should populate tvheadend_build_info")
	}

	bad := testConfig(f.URL)
	bad.Password = "wrong"
	e2, err := New(bad, discard(), "v", "c")
	if err != nil {
		t.Fatal(err)
	}
	if err := e2.Check(context.Background()); !errors.Is(err, tvh.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestHandler_StaticEndpoints(t *testing.T) {
	h := newTestExporter(t, "http://127.0.0.1:1").Handler()
	if code, body := get(t, h, "/"); code != 200 || !strings.Contains(body, `href="/metrics"`) {
		t.Errorf("/ = %d %q", code, body)
	}
	if code, _ := get(t, h, "/-/ready"); code != 200 {
		t.Errorf("/-/ready = %d", code)
	}
	if code, _ := get(t, h, "/nope"); code != 404 {
		t.Errorf("/nope = %d", code)
	}
}

func TestHealthz_FollowsStatusPoll(t *testing.T) {
	f := newFakeTVH(t)
	e := newTestExporter(t, f.URL)
	h := e.Handler()
	if code, _ := get(t, h, "/healthz"); code != http.StatusServiceUnavailable {
		t.Fatalf("healthz before any poll = %d, want 503", code)
	}
	if err := e.pollStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, h, "/healthz"); code != 200 {
		t.Fatalf("healthz after success = %d, want 200", code)
	}
	if _, body := get(t, h, "/metrics"); !strings.Contains(body, "tvheadend_up 1") {
		t.Error("want tvheadend_up 1 after successful status poll")
	}
	// A success older than 3x the interval is stale.
	e.lastStatusOK.Store(time.Now().Add(-4 * e.cfg.PollStatus).UnixNano())
	if code, _ := get(t, h, "/healthz"); code != http.StatusServiceUnavailable {
		t.Fatalf("stale healthz = %d, want 503", code)
	}
}

func TestPollStatus_FailureSetsDown(t *testing.T) {
	f := newFakeTVH(t)
	e := newTestExporter(t, f.URL)
	if err := e.pollStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.down.Store(true)
	if err := e.pollStatus(context.Background()); err == nil {
		t.Fatal("want error while tvheadend is down")
	}
	if _, body := get(t, e.Handler(), "/metrics"); !strings.Contains(body, "tvheadend_up 0") {
		t.Error("want tvheadend_up 0 after failed status poll")
	}
}

func TestPollTopology(t *testing.T) {
	f := newFakeTVH(t)
	e := newTestExporter(t, f.URL)
	if err := e.pollTopology(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, e.Handler(), "/metrics")
	for _, want := range []string{"tvheadend_channel_tags", "tvheadend_dvr_"} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q after topology poll", want)
		}
	}
	f.down.Store(true)
	if err := e.pollTopology(context.Background()); err == nil {
		t.Fatal("want error while tvheadend is down")
	}
}

func TestRun_StopsOnCancel(t *testing.T) {
	f := newFakeTVH(t)
	e := newTestExporter(t, f.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_PortInUseReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	f := newFakeTVH(t)
	cfg := testConfig(f.URL)
	cfg.Listen = ln.Addr().String()
	e, err := New(cfg, discard(), "v", "c")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want listen error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run deadlocked when the listen address was in use")
	}
}
