package collector

import (
	"strings"
	"testing"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInputsCollector_Gauges(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewInputs(reg)
	c.Update(loadFixture[tvh.Input](t, "inputs.json"))
	if v := testutil.ToFloat64(c.gauges["bitrate_bps"].vec.WithLabelValues("a83ead2b96370a88f68dffe6382d32d9", "IPTV #1")); v != 2092064 {
		t.Errorf("bitrate=%v", v)
	}
	if v := testutil.ToFloat64(c.gauges["subscriptions"].vec.WithLabelValues("a83ead2b96370a88f68dffe6382d32d9", "IPTV #1")); v != 2 {
		t.Errorf("subs=%v", v)
	}
	if v := testutil.ToFloat64(c.info.vec.WithLabelValues("a83ead2b96370a88f68dffe6382d32d9", "IPTV #1", "playlist.php - LA 1 UHD in THOTH", "150")); v != 1 {
		t.Errorf("info=%v", v)
	}
	if n := count(t, reg, "tvheadend_input_continuity_errors"); n != 2 {
		t.Errorf("cc series=%d", n)
	}
	if problems, _ := testutil.GatherAndLint(reg); len(problems) > 0 {
		t.Errorf("lint: %v", problems)
	}
}

func TestInputsCollector_RetuneKeepsNumericSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewInputs(reg)
	in := tvh.Input{UUID: "u1", Input: "IPTV #1", Stream: "A", BPS: 10, CC: 5}
	c.Update([]tvh.Input{in})
	in.Stream = "B"
	in.BPS = 20
	c.Update([]tvh.Input{in})
	if n := count(t, reg, "tvheadend_input_bitrate_bps"); n != 1 {
		t.Errorf("want 1 bitrate series across retune, got %d", n)
	}
	if n := count(t, reg, "tvheadend_input_info"); n != 1 {
		t.Errorf("want old info series deleted, got %d", n)
	}
	if v := testutil.ToFloat64(c.info.vec.WithLabelValues("u1", "IPTV #1", "B", "0")); v != 1 {
		t.Errorf("new info=%v", v)
	}
}

// An unnamed IPTV mux can report its URL as the stream name; it must never be
// exported (spec §2/§12).
func TestInputsCollector_SanitizesStream(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewInputs(reg)
	c.Update([]tvh.Input{{UUID: "u1", Input: "IPTV #1", Stream: "http://USER:PASS@xt.example.com:8080/USER/PASS/123.ts?token=abc", Weight: 150}})
	if v := testutil.ToFloat64(c.info.vec.WithLabelValues("u1", "IPTV #1", "<url>", "150")); v != 1 {
		t.Errorf("sanitized input_info missing: %v", v)
	}
	if n := count(t, reg, "tvheadend_input_info"); n != 1 {
		t.Errorf("input_info series=%d want 1", n)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, mf := range mfs {
		out.WriteString(mf.String())
	}
	for _, leak := range []string{"USER", "PASS", "token", "://", "xt.example.com"} {
		if strings.Contains(out.String(), leak) {
			t.Errorf("exposition leaks %q", leak)
		}
	}
}

func TestInputsCollector_CounterResetDetection(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewInputs(reg)
	in := tvh.Input{UUID: "u1", Input: "IPTV #1", CC: 5, TE: 1, UNC: 2}
	c.Update([]tvh.Input{in})
	in.CC, in.TE, in.UNC = 8, 1, 2 // +3 cc
	c.Update([]tvh.Input{in})
	in.CC = 2 // retune reset -> counts as +2
	c.Update([]tvh.Input{in})
	cc := testutil.ToFloat64(c.counters["continuity_errors_total"].WithLabelValues("u1", "IPTV #1"))
	if cc != 5 {
		t.Errorf("continuity_errors_total=%v want 5 (3 + 2 after reset)", cc)
	}
}

func TestInputsCollector_RemovedInputDeleted(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewInputs(reg)
	c.Update([]tvh.Input{{UUID: "u1", Input: "A"}, {UUID: "u2", Input: "B"}})
	c.Update([]tvh.Input{{UUID: "u1", Input: "A"}})
	if n := count(t, reg, "tvheadend_input_bitrate_bps"); n != 1 {
		t.Errorf("want 1 series, got %d", n)
	}
}

// TestInputsCollector_Exposition pins the exact exposition text for a single
// input snapshot (ruling S4: at least one CollectAndCompare test).
func TestInputsCollector_Exposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewInputs(reg)
	c.Update([]tvh.Input{{UUID: "u1", Input: "IPTV #1", Stream: "A", Weight: 150, BPS: 2092064, Subs: 2}})

	const want = `
# HELP tvheadend_input_bitrate_bps Input bitrate.
# TYPE tvheadend_input_bitrate_bps gauge
tvheadend_input_bitrate_bps{input="IPTV #1",uuid="u1"} 2.092064e+06
`
	if err := testutil.CollectAndCompare(c.gauges["bitrate_bps"].vec, strings.NewReader(want), "tvheadend_input_bitrate_bps"); err != nil {
		t.Errorf("bitrate_bps exposition mismatch: %v", err)
	}

	const wantInfo = `
# HELP tvheadend_input_info Current tune of the input; value is always 1.
# TYPE tvheadend_input_info gauge
tvheadend_input_info{input="IPTV #1",stream="A",uuid="u1",weight="150"} 1
`
	if err := testutil.CollectAndCompare(c.info.vec, strings.NewReader(wantInfo), "tvheadend_input_info"); err != nil {
		t.Errorf("info exposition mismatch: %v", err)
	}
}
