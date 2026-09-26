package tvh

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// count returns the number of series for name in g. testutil.CollectAndCount
// requires a prometheus.Collector, but a *prometheus.Registry is only a
// Gatherer/Registerer, so tests use testutil.GatherAndCount via this helper.
func count(t *testing.T, g prometheus.Gatherer, name string) int {
	t.Helper()
	n, err := testutil.GatherAndCount(g, name)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newServer maps api paths to fixture files and records the last request.
func newServer(t *testing.T, files map[string]string, status int) (*httptest.Server, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r
		if u, p, ok := r.BasicAuth(); !ok || u != "user" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f, ok := files[strings.TrimPrefix(r.URL.Path, "/api/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write(fixture(t, f))
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func newClient(t *testing.T, url string) (*Client, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	c, err := New(url, "user", "pw", 2*time.Second, false, reg)
	if err != nil {
		t.Fatal(err)
	}
	return c, reg
}

func TestSubscriptions_DecodesAndSendsAuth(t *testing.T) {
	srv, last := newServer(t, map[string]string{"status/subscriptions": "subscriptions.json"}, 200)
	c, reg := newClient(t, srv.URL)
	subs, err := c.Subscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 || subs[0].Username != "alice" || subs[1].Errors != 6 || subs[2].Username != "" {
		t.Errorf("bad decode: %+v", subs)
	}
	if last.Header.Get("Accept") != "application/json" {
		t.Error("missing Accept header")
	}
	if n := count(t, reg, "tvheadend_exporter_api_requests_total"); n != 1 {
		t.Errorf("want 1 api_requests series, got %d", n)
	}
}

func TestGrid_SendsLimitAndWarnsOnTruncation(t *testing.T) {
	srv, last := newServer(t, map[string]string{"mpegts/mux/grid": "muxes_truncated.json"}, 200)
	c, _ := newClient(t, srv.URL)

	var logBuf bytes.Buffer
	c.log = slog.New(slog.NewTextHandler(&logBuf, nil))

	muxes, err := c.Muxes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(muxes) != 1 {
		t.Fatalf("want 1 entry from the truncated fixture, got %d", len(muxes))
	}
	if last.URL.Query().Get("limit") != "10000" {
		t.Errorf("want limit=10000, got %q", last.URL.RawQuery)
	}
	if !strings.Contains(logBuf.String(), "grid truncated") {
		t.Errorf("want a 'grid truncated' warning, got log: %s", logBuf.String())
	}
}

func TestUnauthorized(t *testing.T) {
	srv, _ := newServer(t, map[string]string{"serverinfo": "serverinfo.json"}, 200)
	reg := prometheus.NewRegistry()
	c, _ := New(srv.URL, "user", "WRONG", time.Second, false, reg)
	_, err := c.ServerInfo(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

func TestServerError(t *testing.T) {
	srv, _ := newServer(t, map[string]string{"serverinfo": "serverinfo.json"}, 500)
	c, reg := newClient(t, srv.URL)
	if _, err := c.ServerInfo(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if got := testutil.ToFloat64(c.metrics.requests.WithLabelValues("serverinfo", "500")); got != 1 {
		t.Errorf("want 500 counted, got %v", got)
	}
	_ = reg
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "user", "pw", 50*time.Millisecond, false, prometheus.NewRegistry())
	if _, err := c.ServerInfo(context.Background()); err == nil {
		t.Fatal("want timeout error")
	}
}

func TestCounts(t *testing.T) {
	srv, _ := newServer(t, map[string]string{"dvr/autorec/grid": "empty_grid.json", "dvr/timerec/grid": "empty_grid.json"}, 200)
	c, _ := newClient(t, srv.URL)
	n, err := c.DVRAutorecCount(context.Background())
	if err != nil || n != 0 {
		t.Errorf("autorec: got %d %v", n, err)
	}
	n, err = c.DVRTimerecCount(context.Background())
	if err != nil || n != 0 {
		t.Errorf("timerec: got %d %v", n, err)
	}
}
