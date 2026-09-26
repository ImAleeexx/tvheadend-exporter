package collector

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/geoip"
	"github.com/imaleeexx/tvheadend-exporter/internal/sessions"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeGeo struct{}

func (fakeGeo) Lookup(ip string) geoip.Location {
	if ip == "203.0.113.10" {
		return geoip.Location{Country: "ES", City: "Madrid"}
	}
	return geoip.Location{}
}

func newStatus(t *testing.T) (*Status, *prometheus.Registry, *bytes.Buffer) {
	t.Helper()
	reg := prometheus.NewRegistry()
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := NewStatus(reg, sessions.New(time.Minute), fakeGeo{}, log)
	return s, reg, buf
}

func fixtureSubs(t *testing.T) ([]tvh.Subscription, []tvh.Connection) {
	t.Helper()
	return loadFixture[tvh.Subscription](t, "subscriptions.json"), loadFixture[tvh.Connection](t, "connections.json")
}

// sessionEndLine returns the first session_end log line for user.
func sessionEndLine(t *testing.T, buf *bytes.Buffer, user string) string {
	t.Helper()
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, `"event":"session_end"`) && strings.Contains(l, `"user":"`+user+`"`) {
			return l
		}
	}
	t.Fatalf("no session_end line for %s in:\n%s", user, buf.String())
	return ""
}

func TestStatusCollector_LiveGauges(t *testing.T) {
	s, reg, _ := newStatus(t)
	subs, conns := fixtureSubs(t)
	s.Update(subs, conns, t0)

	if n := count(t, reg, "tvheadend_subscription_info"); n != 3 {
		t.Errorf("subscription_info series=%d want 3", n)
	}
	if v := testutil.ToFloat64(s.userActive.vec.WithLabelValues("alice")); v != 1 {
		t.Errorf("user_active_streams{alice}=%v", v)
	}
	if v := testutil.ToFloat64(s.userActive.vec.WithLabelValues("anonymous")); v != 1 {
		t.Errorf("user_active_streams{anonymous}=%v", v)
	}
	if v := testutil.ToFloat64(s.channelViewers.vec.WithLabelValues("La 1 HD")); v != 2 {
		t.Errorf("channel_active_viewers{La 1 HD}=%v", v)
	}
	if v := testutil.ToFloat64(s.subsActive.vec.WithLabelValues("pass", "http")); v != 2 {
		t.Errorf("subscriptions_active{pass,http}=%v", v)
	}
	if v := testutil.ToFloat64(s.subsActive.vec.WithLabelValues("htsp", "htsp")); v != 1 {
		t.Errorf("subscriptions_active{htsp,htsp}=%v", v)
	}
	if v := testutil.ToFloat64(s.connsActive.vec.WithLabelValues("http", "bob", "true")); v != 1 {
		t.Errorf("connections_active{http,bob,true}=%v", v)
	}
	if n := count(t, reg, "tvheadend_connection_info"); n != 3 {
		t.Errorf("connection_info series=%d", n)
	}
	if v := testutil.ToFloat64(s.sessionsTracked); v != 3 {
		t.Errorf("sessions_tracked=%v", v)
	}
	// GeoIP labels on the per-id gauge for alice.
	g, err := s.subErrors.GetMetricWithLabelValues("15268", "alice", "La 1 HD", "Kodi Media Center", "203.0.113.10", "htsp", "ES", "Madrid")
	if err != nil || testutil.ToFloat64(g) != 6 {
		t.Errorf("subscription_errors for alice: %v %v", err, g)
	}
}

func TestStatusCollector_TwoStreamsSameLabels(t *testing.T) {
	s, reg, _ := newStatus(t)
	a := tvh.Subscription{ID: 1, Start: tvh.FlexInt(t0.Unix()), Username: "bob", Channel: "X", Client: "VLC", Hostname: "198.51.100.7", Profile: "pass"}
	b := a
	b.ID = 2
	s.Update([]tvh.Subscription{a, b}, nil, t0)
	if n := count(t, reg, "tvheadend_subscription_info"); n != 2 {
		t.Errorf("want 2 info series, got %d", n)
	}
	if v := testutil.ToFloat64(s.userActive.vec.WithLabelValues("bob")); v != 2 {
		t.Errorf("user_active_streams{bob}=%v", v)
	}
	if v := testutil.ToFloat64(s.subsActive.vec.WithLabelValues("pass", "unknown")); v != 2 {
		t.Errorf("no connections → type unknown: %v", v)
	}
}

func TestStatusCollector_CountersAndEnd(t *testing.T) {
	s, reg, buf := newStatus(t)
	a := tvh.Subscription{ID: 1, Start: tvh.FlexInt(t0.Add(-time.Hour).Unix()), Username: "bob", Channel: "X", Client: "VLC", Hostname: "198.51.100.7", Profile: "pass", TotalIn: 100, TotalOut: 100, Errors: 1}
	s.Update([]tvh.Subscription{a}, nil, t0)
	a.TotalIn, a.TotalOut, a.Errors = 600, 700, 3
	s.Update([]tvh.Subscription{a}, nil, t0.Add(10*time.Second))
	lv := []string{"bob", "X", "VLC", "198.51.100.7", "pass", "", ""}
	if v := testutil.ToFloat64(s.sessionSeconds.WithLabelValues(lv...)); v != 10 {
		t.Errorf("session_seconds_total=%v", v)
	}
	if v := testutil.ToFloat64(s.sessionBytesOut.WithLabelValues(lv...)); v != 600 {
		t.Errorf("session_bytes_out_total=%v", v)
	}
	if v := testutil.ToFloat64(s.sessionErrors.WithLabelValues(lv...)); v != 2 {
		t.Errorf("session_errors_total=%v", v)
	}
	if v := testutil.ToFloat64(s.sessionsStarted.WithLabelValues(lv...)); v != 1 {
		t.Errorf("sessions_started_total=%v", v)
	}

	s.Update(nil, nil, t0.Add(20*time.Second))
	if n := count(t, reg, "tvheadend_subscription_info"); n != 0 {
		t.Errorf("info series after end=%d", n)
	}
	if n := count(t, reg, "tvheadend_subscription_bytes_out"); n != 0 {
		t.Errorf("bytes_out series after end=%d", n)
	}
	if v := testutil.ToFloat64(s.sessionsEnded.WithLabelValues(lv...)); v != 1 {
		t.Errorf("sessions_ended_total=%v", v)
	}
	if n := count(t, reg, "tvheadend_session_duration_seconds"); n != 1 {
		t.Errorf("histogram series=%d", n)
	}
	line := sessionEndLine(t, buf, "bob")
	for _, want := range []string{
		`"reason":"gone"`, `"duration_s":3620`, `"start":"2026-09-26T19:00:00Z"`,
		`"end":"2026-09-26T20:00:20Z"`, `"bytes_out":700`, `"bytes_in":600`, `"errors":3`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("session_end line missing %s: %s", want, line)
		}
	}
}

// Ruling S7: the connection vanishes in the same snapshot as the
// subscription, yet session_end still carries the type seen while live.
func TestStatusCollector_EndLogKeepsConnectionType(t *testing.T) {
	s, _, buf := newStatus(t)
	subs, conns := fixtureSubs(t)
	s.Update(subs, conns, t0)
	s.Update(nil, nil, t0.Add(10*time.Second))
	if l := sessionEndLine(t, buf, "bob"); !strings.Contains(l, `"type":"http"`) {
		t.Errorf("want type http on gone session: %s", l)
	}
	if l := sessionEndLine(t, buf, "alice"); !strings.Contains(l, `"type":"htsp"`) {
		t.Errorf("want type htsp on gone session: %s", l)
	}
	if len(s.sessTypes) != 0 {
		t.Errorf("per-session type map must be emptied on end: %v", s.sessTypes)
	}
}

// Ruling S8: only htsp/http pass through to subscriptions_active.type.
func TestStatusCollector_UnknownConnectionTypeMapped(t *testing.T) {
	s, _, _ := newStatus(t)
	sub := tvh.Subscription{ID: 1, Start: tvh.FlexInt(t0.Unix()), Username: "dan", Channel: "X", Hostname: "192.0.2.1", Profile: "pass"}
	conn := tvh.Connection{ID: 9, Peer: "192.0.2.1", Type: "SAT>IP", User: "dan", Streaming: 1}
	s.Update([]tvh.Subscription{sub}, []tvh.Connection{conn}, t0)
	if v := testutil.ToFloat64(s.subsActive.vec.WithLabelValues("pass", "unknown")); v != 1 {
		t.Errorf("subscriptions_active{pass,unknown}=%v", v)
	}
}

func TestStatusCollector_LostSessionsDeleteGauges(t *testing.T) {
	s, reg, buf := newStatus(t)
	subs, conns := fixtureSubs(t)
	s.Update(subs, conns, t0)
	s.Fail(t0.Add(30 * time.Second))
	if n := count(t, reg, "tvheadend_subscription_info"); n != 3 {
		t.Fatalf("inside grace gauges must stay: %d", n)
	}
	s.Fail(t0.Add(2 * time.Minute))
	for _, name := range []string{
		"tvheadend_subscription_info", "tvheadend_subscription_start_timestamp_seconds",
		"tvheadend_subscription_bitrate_in_bps", "tvheadend_subscription_bitrate_out_bps",
		"tvheadend_subscription_bytes_in", "tvheadend_subscription_bytes_out",
		"tvheadend_subscription_errors", "tvheadend_connection_info", "tvheadend_connections_active",
		"tvheadend_user_active_streams", "tvheadend_channel_active_viewers", "tvheadend_subscriptions_active",
	} {
		if n := count(t, reg, name); n != 0 {
			t.Errorf("after grace %s must be deleted: %d series", name, n)
		}
	}
	if v := testutil.ToFloat64(s.sessionsTracked); v != 0 {
		t.Errorf("sessions_tracked=%v", v)
	}
	if !strings.Contains(buf.String(), `"reason":"lost"`) {
		t.Error("want lost reason logged")
	}
}

func TestStatusCollector_ShutdownSkipsHistogram(t *testing.T) {
	s, reg, buf := newStatus(t)
	subs, _ := fixtureSubs(t)
	s.Update(subs, nil, t0)
	s.Shutdown(t0.Add(time.Second))
	if n := count(t, reg, "tvheadend_session_duration_seconds"); n != 0 {
		t.Errorf("shutdown must not observe durations: %d", n)
	}
	if !strings.Contains(buf.String(), `"reason":"shutdown"`) {
		t.Error("want shutdown reason logged")
	}
}

func TestStatusCollector_Access(t *testing.T) {
	s, _, _ := newStatus(t)
	s.UpdateAccess(loadFixture[tvh.AccessEntry](t, "access.json"))
	if v := testutil.ToFloat64(s.userConnLimit.vec.WithLabelValues("alice")); v != 4 {
		t.Errorf("user_conn_limit{alice}=%v", v)
	}
	if v := testutil.ToFloat64(s.accessEntries.vec.WithLabelValues("true")); v != 2 {
		t.Errorf("access_entries{true}=%v", v)
	}
	if v := testutil.ToFloat64(s.accessEntries.vec.WithLabelValues("false")); v != 1 {
		t.Errorf("access_entries{false}=%v", v)
	}
}

// Ruling S4: compare exposition text produced from the fixtures.
func TestStatusCollector_CompareExposition(t *testing.T) {
	s, reg, _ := newStatus(t)
	subs, conns := fixtureSubs(t)
	s.Update(subs, conns, t0)
	s.UpdateAccess(loadFixture[tvh.AccessEntry](t, "access.json"))
	const want = `
# HELP tvheadend_access_entries Access entries by enabled state.
# TYPE tvheadend_access_entries gauge
tvheadend_access_entries{enabled="false"} 1
tvheadend_access_entries{enabled="true"} 2
# HELP tvheadend_channel_active_viewers Active viewers per channel.
# TYPE tvheadend_channel_active_viewers gauge
tvheadend_channel_active_viewers{channel="La 1 HD"} 2
tvheadend_channel_active_viewers{channel="XTRM"} 1
# HELP tvheadend_subscriptions_active Active subscriptions.
# TYPE tvheadend_subscriptions_active gauge
tvheadend_subscriptions_active{profile="htsp",type="htsp"} 1
tvheadend_subscriptions_active{profile="pass",type="http"} 2
# HELP tvheadend_user_active_streams Active streams per user.
# TYPE tvheadend_user_active_streams gauge
tvheadend_user_active_streams{user="alice"} 1
tvheadend_user_active_streams{user="anonymous"} 1
tvheadend_user_active_streams{user="bob"} 1
# HELP tvheadend_user_conn_limit Configured connection limit per user (0 = unlimited).
# TYPE tvheadend_user_conn_limit gauge
tvheadend_user_conn_limit{user="alice"} 4
tvheadend_user_conn_limit{user="bob"} 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want),
		"tvheadend_access_entries", "tvheadend_channel_active_viewers", "tvheadend_subscriptions_active",
		"tvheadend_user_active_streams", "tvheadend_user_conn_limit"); err != nil {
		t.Error(err)
	}
	const wantInfo = `
# HELP tvheadend_subscription_info Live subscription; value is always 1.
# TYPE tvheadend_subscription_info gauge
tvheadend_subscription_info{channel="La 1 HD",city="",client="VLC/3.0.23 LibVLC/3.0.23",country="",id="15276",peer="198.51.100.7",profile="pass",service="IPTV #2/IPTV/LA 1 HD IPTV/Service01",state="Running",title="HTTP",user="bob"} 1
tvheadend_subscription_info{channel="La 1 HD",city="Madrid",client="Kodi Media Center",country="ES",id="15268",peer="203.0.113.10",profile="htsp",service="IPTV #2/IPTV/LA 1 HD IPTV/Service01",state="Running",title="203.0.113.10 [ alice | Kodi Media Center ]",user="alice"} 1
tvheadend_subscription_info{channel="XTRM",city="",client="VLC/3.0.20 LibVLC/3.0.20",country="",id="15280",peer="10.0.0.5",profile="pass",service="IPTV #1/THOTH/playlist.php - XTRM/Service01",state="Running",title="HTTP",user="anonymous"} 1
`
	if err := testutil.CollectAndCompare(s.subInfo, strings.NewReader(wantInfo), "tvheadend_subscription_info"); err != nil {
		t.Error(err)
	}
}

func TestSanitizeLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"IPTV #1/THOTH/playlist.php - XTRM/Service01", "IPTV #1/THOTH/playlist.php - XTRM/Service01"},
		{"IPTV/http://xt.example.com:8080/USER/PASS/123.ts", "IPTV/<url>"},
		{"IPTV/https://cdn.example.net/live/ch1.m3u8?token=s3cr3t&x=1", "IPTV/<url>"},
		{"IPTV/http://user:pw@198.51.100.9:9981/stream/channel/abc", "IPTV/<url>"},
		{"a udp://239.0.0.1:1234 b", "a <url> b"},
		{"x http://h/p?q=1 and rtsp://h2/p2", "x <url> and <url>"},
		{"IPTV #1/THOTH/http://u:p@iptv.example.net:8080/live?token=t Service01", "IPTV #1/THOTH/<url> Service01"},
		{"svc/git+ssh://host/x", "svc/<url>"},
		{"://nohost", "<url>"},
		{"DVR: Secret Programme Title", "DVR"},
		{"HTTP", "HTTP"},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeLabel(c.in); got != c.want {
			t.Errorf("sanitizeLabel(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestStatusCollector_SanitizesServiceAndTitle(t *testing.T) {
	s, reg, buf := newStatus(t)
	a := tvh.Subscription{ID: 7, Start: tvh.FlexInt(t0.Unix()), Username: "eve", Channel: "X", Hostname: "192.0.2.3", Profile: "pass",
		Service: "IPTV/http://USER:PASS@xt.example.com:8080/USER/PASS/123.ts?token=abc", Title: "DVR: My Programme"}
	s.Update([]tvh.Subscription{a}, nil, t0)
	s.Update(nil, nil, t0.Add(10*time.Second))
	s.Update([]tvh.Subscription{a}, nil, t0.Add(20*time.Second))
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, mf := range mfs {
		out.WriteString(mf.String())
	}
	out.WriteString(buf.String())
	for _, leak := range []string{"USER", "PASS", "123.ts", "token", "My Programme", "://", "xt.example.com", "8080"} {
		if strings.Contains(out.String(), leak) {
			t.Errorf("exposition or log leaks %q", leak)
		}
	}
	g, err := s.subInfo.GetMetricWithLabelValues("7", "eve", "X", "", "192.0.2.3", "pass", "", "", "IPTV/<url>", "", "DVR")
	if err != nil || testutil.ToFloat64(g) != 1 {
		t.Errorf("sanitized subscription_info missing: %v", err)
	}
}

// A poll with connections but no subscriptions, then an outage beyond grace:
// connection series must not linger (Review Focus 3).
func TestStatusCollector_LostClearsConnectionsWithoutSessions(t *testing.T) {
	s, reg, _ := newStatus(t)
	_, conns := fixtureSubs(t)
	s.Update(nil, conns, t0)
	s.Fail(t0.Add(30 * time.Second))
	if n := count(t, reg, "tvheadend_connection_info"); n != 3 {
		t.Fatalf("inside grace connection_info must stay: %d", n)
	}
	s.Fail(t0.Add(2 * time.Minute))
	for _, name := range []string{"tvheadend_connection_info", "tvheadend_connections_active"} {
		if n := count(t, reg, name); n != 0 {
			t.Errorf("after grace %s must be cleared: %d series", name, n)
		}
	}
}

func TestStatusCollector_Lint(t *testing.T) {
	s, reg, _ := newStatus(t)
	subs, conns := fixtureSubs(t)
	s.Update(subs, conns, t0)
	problems, err := testutil.GatherAndLint(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("lint: %s: %s", p.Metric, p.Text)
	}
}
