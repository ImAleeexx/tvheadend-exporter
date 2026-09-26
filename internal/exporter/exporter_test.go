package exporter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/config"
	"github.com/imaleeexx/tvheadend-exporter/internal/fake"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

// newFakeTVH starts a fake Tvheadend accepting user/pw. It fails the test
// (from the test goroutine, at cleanup) on any non-GET or passwd request.
func newFakeTVH(t *testing.T) *fake.Server {
	t.Helper()
	return fake.NewServer(t, "user", "pw")
}

// down makes every Tvheadend endpoint return 500.
func down(f *fake.Server) {
	for name := range tvh.Endpoints {
		f.Fail(name, http.StatusInternalServerError)
	}
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
	e := newTestExporter(t, f.URL())
	if err := e.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, e.Handler(), "/metrics")
	if !strings.Contains(body, "tvheadend_build_info{") {
		t.Error("Check should populate tvheadend_build_info")
	}

	bad := testConfig(f.URL())
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
	e := newTestExporter(t, f.URL())
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
	e := newTestExporter(t, f.URL())
	if err := e.pollStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	down(f)
	if err := e.pollStatus(context.Background()); err == nil {
		t.Fatal("want error while tvheadend is down")
	}
	if _, body := get(t, e.Handler(), "/metrics"); !strings.Contains(body, "tvheadend_up 0") {
		t.Error("want tvheadend_up 0 after failed status poll")
	}
}

func TestPollTopology(t *testing.T) {
	f := newFakeTVH(t)
	e := newTestExporter(t, f.URL())
	if err := e.pollTopology(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, e.Handler(), "/metrics")
	for _, want := range []string{"tvheadend_channel_tags", "tvheadend_dvr_"} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q after topology poll", want)
		}
	}
	down(f)
	if err := e.pollTopology(context.Background()); err == nil {
		t.Fatal("want error while tvheadend is down")
	}
}

func TestRun_StopsOnCancel(t *testing.T) {
	f := newFakeTVH(t)
	e := newTestExporter(t, f.URL())
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
	cfg := testConfig(f.URL())
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

// syncBuffer is a goroutine-safe log sink: pollers write while the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// e2ePassword is distinctive so the test can prove it never leaks.
const e2ePassword = "pw-E2E-s3cret-credential"

func newE2EExporter(t *testing.T, srvURL string, logSink io.Writer) *Exporter {
	t.Helper()
	env := map[string]string{
		"TVH_URL": srvURL, "TVH_USERNAME": "u", "TVH_PASSWORD": e2ePassword,
		"TVH_POLL_STATUS": "50ms", "TVH_POLL_TOPOLOGY": "200ms", "TVH_SESSION_GRACE": "100ms",
		"TVH_LISTEN": "127.0.0.1:0",
	}
	cfg, err := config.Load(nil, func(k string) string { return env[k] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewJSONHandler(logSink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e, err := New(cfg, log, "test", "abc")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// eventually polls cond until it returns "" (success) or the deadline passes,
// then fails with the last reported problem. It replaces bare fixed sleeps so
// slow -race runs do not flake, without weakening any assertion.
func eventually(t *testing.T, what string, cond func() string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		msg := cond()
		if msg == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %s", what, msg)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// missing reports the wanted substrings absent from out ("" = all present).
func missing(out string, wants ...string) string {
	var miss []string
	for _, w := range wants {
		if !strings.Contains(out, w) {
			miss = append(miss, w)
		}
	}
	if len(miss) == 0 {
		return ""
	}
	return "missing:\n  " + strings.Join(miss, "\n  ")
}

// present reports the first forbidden substring found in out ("" = none).
func present(out string, bads ...string) string {
	for _, b := range bads {
		if strings.Contains(out, b) {
			return "unexpectedly contains " + b
		}
	}
	return ""
}

func TestEndToEnd(t *testing.T) {
	srv := fake.NewServer(t, "u", e2ePassword)
	var logBuf syncBuffer
	e := newE2EExporter(t, srv.URL(), &logBuf)
	h := e.Handler()
	metricsBody := func() string { _, b := get(t, h, "/metrics"); return b }

	if err := e.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()

	// Wait at least one topology interval (200ms) before the first look.
	time.Sleep(250 * time.Millisecond)
	var out string
	eventually(t, "initial scrape", func() string {
		out = metricsBody()
		return missing(out,
			"tvheadend_up 1",
			`tvheadend_build_info{api_version="19",server_name="Tvheadend",version="4.3-2070~g2beb6c9-dirty"} 1`,
			`tvheadend_subscription_info{channel="La 1 HD",city="",client="Kodi Media Center",country="",id="15268",peer="203.0.113.10",profile="htsp",service="IPTV #2/IPTV/LA 1 HD IPTV/Service01",state="Running",title="203.0.113.10 [ alice | Kodi Media Center ]",user="alice"} 1`,
			`tvheadend_user_active_streams{user="anonymous"} 1`,
			`tvheadend_subscriptions_active{profile="pass",type="http"} 2`,
			`tvheadend_input_bitrate_bps{input="IPTV #1",uuid="a83ead2b96370a88f68dffe6382d32d9"} 2.092064e+06`,
			`tvheadend_muxes{enabled="true",network="THOTH",scan_result="fail"} 1`,
			`tvheadend_dvr_entries{config="! New config",creator="bob",owner="admin",status="scheduled"} 1`,
			`tvheadend_user_conn_limit{user="alice"} 4`,
			`tvheadend_exporter_last_success_timestamp_seconds{group="topology"}`,
			`tvheadend_capability{name="timeshift"} 1`,
			"go_goroutines",
		)
	})
	// Security: no URLs, tokens or credentials in the exposition.
	if msg := present(out, "passwd", "token=", "http://", "https://", e2ePassword); msg != "" {
		t.Error("/metrics " + msg)
	}
	if code, body := get(t, h, "/healthz"); code != 200 {
		t.Errorf("healthz=%d %s", code, body)
	}

	// Sessions disappear -> per-id gauges gone, ended counter + session_end log line.
	srv.Set("subscriptions", "empty_grid.json")
	eventually(t, "sessions gone", func() string {
		out = metricsBody()
		if msg := present(out, `tvheadend_subscription_info{`); msg != "" {
			return msg
		}
		if msg := missing(out, `tvheadend_sessions_ended_total{channel="La 1 HD",city="",client="Kodi Media Center",country="",peer="203.0.113.10",profile="htsp",user="alice"} 1`); msg != "" {
			return msg
		}
		return missing(logBuf.String(), `"event":"session_end"`, `"reason":"gone"`)
	})

	// Secrets in free-text fields: service URLs collapse to "<url>" and DVR
	// titles to "DVR"; no URL part (scheme, host, credentials, path, token)
	// nor programme title may appear in /metrics or the log.
	srv.Set("subscriptions", "subscriptions_secrets.json")
	eventually(t, "sanitised subscription", func() string {
		out = metricsBody()
		return missing(out, `tvheadend_subscription_info{channel="XTRM",city="",client="tvh DVR",country="",id="15300",peer="192.0.2.44",profile="pass",service="IPTV #1/THOTH/<url> Service01",state="Running",title="DVR",user="dave"} 1`)
	})
	secrets := []string{"hunter2", "dave:", "token=", "s3cr3tT0ken", "/live/", "123.ts", "Secret Programme", e2ePassword, "passwd",
		"http://", "https://", "iptv.example.net"}
	if msg := present(out, secrets...); msg != "" {
		t.Error("/metrics " + msg)
	}

	// Review Focus 3: sessions live, then the status poll fails for longer
	// than the grace -> up 0, healthz 503, sessions end with reason=lost and
	// no stale per-id gauges remain.
	srv.Set("subscriptions", "subscriptions.json")
	eventually(t, "sessions restored", func() string {
		return missing(metricsBody(), `tvheadend_subscription_info{channel="La 1 HD",city="",client="Kodi Media Center",country="",id="15268"`)
	})
	srv.Fail("subscriptions", http.StatusInternalServerError)
	time.Sleep(250 * time.Millisecond) // > grace (100ms) and > 3x poll (150ms)
	eventually(t, "status poll failing", func() string {
		out = metricsBody()
		if msg := missing(out, "tvheadend_up 0"); msg != "" {
			return msg
		}
		if msg := present(out, `tvheadend_subscription_info{`); msg != "" {
			return msg
		}
		if code, _ := get(t, h, "/healthz"); code != http.StatusServiceUnavailable {
			return "healthz still 200"
		}
		return missing(logBuf.String(), `"reason":"lost"`)
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if msg := present(logBuf.String(), secrets...); msg != "" {
		t.Error("log " + msg)
	}
}

func TestCheck_Unauthorized(t *testing.T) {
	srv := fake.NewServer(t, "u", "OTHER")
	e := newE2EExporter(t, srv.URL(), io.Discard)
	if err := e.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("want unauthorized error, got %v", err)
	}
}

func TestHandler_MetricsBasicAuth(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:1")
	cfg.MetricsUsername, cfg.MetricsPassword = "prom", "s3cret"
	e, err := New(cfg, discard(), "v-test", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	h := e.Handler()
	req := func(path, user, pass string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if user != "" || pass != "" {
			r.SetBasicAuth(user, pass)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	for _, path := range []string{"/metrics", "/"} {
		if rec := req(path, "", ""); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
			t.Errorf("%s without credentials: code %d, WWW-Authenticate %q", path, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
		if rec := req(path, "prom", "wrong"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s wrong password: code %d", path, rec.Code)
		}
		if rec := req(path, "other", "s3cret"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s wrong user: code %d", path, rec.Code)
		}
		if rec := req(path, "prom", "s3cret"); rec.Code != http.StatusOK {
			t.Errorf("%s good credentials: code %d", path, rec.Code)
		}
	}
	if rec := req("/metrics", "prom", "s3cret"); !strings.Contains(rec.Body.String(), "tvheadend_exporter_build_info") {
		t.Error("authenticated /metrics did not return metrics")
	}
	// Probes stay open so orchestrators need no credentials.
	if rec := req("/-/ready", "", ""); rec.Code != http.StatusOK {
		t.Errorf("/-/ready should not require auth, got %d", rec.Code)
	}
	if rec := req("/healthz", "", ""); rec.Code == http.StatusUnauthorized {
		t.Error("/healthz should not require auth")
	}
}

func TestHandler_NoAuthByDefault(t *testing.T) {
	if code, _ := get(t, newTestExporter(t, "http://127.0.0.1:1").Handler(), "/metrics"); code != http.StatusOK {
		t.Errorf("/metrics without auth configured: code %d", code)
	}
}
