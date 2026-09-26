package collector

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestServerCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewServer(reg)
	c.Update(tvh.ServerInfo{SWVersion: "4.3-2070", APIVersion: 19, Name: "Tvheadend", Capabilities: []string{"timeshift", "trace"}})
	c.SetUp(true)
	if v := testutil.ToFloat64(c.buildInfo.vec.WithLabelValues("4.3-2070", "19", "Tvheadend")); v != 1 {
		t.Errorf("build_info=%v", v)
	}
	if v := testutil.ToFloat64(c.capability.vec.WithLabelValues("timeshift")); v != 1 {
		t.Errorf("capability=%v", v)
	}
	if v := testutil.ToFloat64(c.up); v != 1 {
		t.Errorf("up=%v", v)
	}
	c.SetUp(false)
	if v := testutil.ToFloat64(c.up); v != 0 {
		t.Errorf("up=%v", v)
	}
}

// TestServerCollector_UpExposition checks tvheadend_up's exposition text
// verbatim (ruling S4: at least one CollectAndCompare test per task).
// c.up is a prometheus.Gauge, which satisfies prometheus.Collector directly,
// so CollectAndCompare can be used without the Registry-is-not-a-Collector
// pitfall from ruling B1.
func TestServerCollector_UpExposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewServer(reg)
	c.SetUp(true)
	want := `
# HELP tvheadend_up 1 if the last status poll succeeded.
# TYPE tvheadend_up gauge
tvheadend_up 1
`
	if err := testutil.CollectAndCompare(c.up, strings.NewReader(want), "tvheadend_up"); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}

func TestServerCollector_UpdateReplacesStaleSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewServer(reg)
	c.Update(tvh.ServerInfo{SWVersion: "4.3-2070", APIVersion: 19, Name: "Tvheadend", Capabilities: []string{"timeshift", "trace"}})
	c.Update(tvh.ServerInfo{SWVersion: "4.3-2071", APIVersion: 19, Name: "Tvheadend", Capabilities: []string{"trace"}})
	if n := count(t, reg, "tvheadend_build_info"); n != 1 {
		t.Errorf("build_info series=%d, want 1", n)
	}
	if n := count(t, reg, "tvheadend_capability"); n != 1 {
		t.Errorf("capability series=%d, want 1 (timeshift dropped)", n)
	}
	if v := testutil.ToFloat64(c.buildInfo.vec.WithLabelValues("4.3-2071", "19", "Tvheadend")); v != 1 {
		t.Errorf("build_info=%v", v)
	}
}

func TestSelfCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := NewSelf(reg, "1.2.3", "abc")
	s.Observe("status", 20*time.Millisecond, nil)
	s.Observe("status", 5*time.Millisecond, errors.New("boom"))
	if v := testutil.ToFloat64(s.errors.WithLabelValues("status")); v != 1 {
		t.Errorf("poll_errors=%v", v)
	}
	if v := testutil.ToFloat64(s.lastSuccess.WithLabelValues("status")); v == 0 {
		t.Error("last_success not set")
	}
	if n := count(t, reg, "tvheadend_exporter_build_info"); n != 1 {
		t.Errorf("build_info=%d", n)
	}
	if problems, _ := testutil.GatherAndLint(reg); len(problems) > 0 {
		t.Errorf("lint: %v", problems)
	}
}

func TestSelfCollector_BuildInfoExposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewSelf(reg, "1.2.3", "abc")
	want := `
# HELP tvheadend_exporter_build_info Exporter build; value is always 1.
# TYPE tvheadend_exporter_build_info gauge
tvheadend_exporter_build_info{commit="abc",version="1.2.3"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "tvheadend_exporter_build_info"); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
