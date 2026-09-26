package collector

import (
	"strings"
	"testing"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestDVRCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewDVR(reg)
	c.Update(loadFixture[tvh.DVREntry](t, "dvr_entries.json"), loadFixture[tvh.DVRConfig](t, "dvr_configs.json"), 2, 1)
	get := func(ls *labelSet, lv ...string) float64 { return testutil.ToFloat64(ls.vec.WithLabelValues(lv...)) }

	if v := get(c.entries, "completed", "admin", "alice", "966571e4cf855e95e5a94c4481217c4a"); v != 1 {
		t.Errorf("entries completed=%v", v)
	}
	if v := get(c.entries, "completedError", "admin", "bob", "966571e4cf855e95e5a94c4481217c4a"); v != 1 {
		t.Errorf("entries completedError=%v", v)
	}
	if v := get(c.entries, "scheduled", "admin", "bob", "! New config"); v != 1 {
		t.Errorf("entries scheduled (config name resolved)=%v", v)
	}
	if v := get(c.bytes, "completed", "966571e4cf855e95e5a94c4481217c4a"); v != 1373117220 {
		t.Errorf("bytes=%v", v)
	}
	if v := get(c.dataErrors, "completed"); v != 1383 {
		t.Errorf("data_errors=%v", v)
	}
	if v := testutil.ToFloat64(c.active); v != 1 {
		t.Errorf("active=%v", v)
	}
	if v := testutil.ToFloat64(c.upcoming); v != 1 {
		t.Errorf("upcoming=%v", v)
	}
	if v := testutil.ToFloat64(c.autorec); v != 2 {
		t.Errorf("autorec=%v", v)
	}
	if v := testutil.ToFloat64(c.timerec); v != 1 {
		t.Errorf("timerec=%v", v)
	}
	if v := get(c.configInfo, "509f31336490ae45e6927c8c00d2bb23", "! New config", "/home/hts", "41199a95a0e9a8842613d52a57efc53b"); v != 1 {
		t.Errorf("config_info=%v", v)
	}
	if problems, _ := testutil.GatherAndLint(reg); len(problems) > 0 {
		t.Errorf("lint: %v", problems)
	}
}

// TestDVRCollector_Exposition pins the collector's output against expected
// Prometheus exposition text for the two gauges that best exercise the
// config-name resolution and status pass-through (sched_status verbatim,
// including "completedError" as required by the cross-task alert/dashboard
// contract).
func TestDVRCollector_Exposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewDVR(reg)
	c.Update(loadFixture[tvh.DVREntry](t, "dvr_entries.json"), loadFixture[tvh.DVRConfig](t, "dvr_configs.json"), 2, 1)

	want := `
# HELP tvheadend_dvr_entries DVR entries by schedule status, owner, creator and config.
# TYPE tvheadend_dvr_entries gauge
tvheadend_dvr_entries{config="! New config",creator="alice",owner="admin",status="recording"} 1
tvheadend_dvr_entries{config="! New config",creator="bob",owner="admin",status="scheduled"} 1
tvheadend_dvr_entries{config="966571e4cf855e95e5a94c4481217c4a",creator="alice",owner="admin",status="completed"} 1
tvheadend_dvr_entries{config="966571e4cf855e95e5a94c4481217c4a",creator="bob",owner="admin",status="completedError"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "tvheadend_dvr_entries"); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}

// TestDVRCollector_StaleSeriesDropped confirms that a status/owner/creator/config
// combination not present in a later Update no longer appears (labelSet begin/end
// semantics keep entries from lingering with stale values).
func TestDVRCollector_StaleSeriesDropped(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewDVR(reg)
	entries := loadFixture[tvh.DVREntry](t, "dvr_entries.json")
	configs := loadFixture[tvh.DVRConfig](t, "dvr_configs.json")
	c.Update(entries, configs, 2, 1)

	// Drop everything but the first entry.
	c.Update(entries[:1], configs, 0, 0)

	if v := testutil.ToFloat64(c.entries.vec.WithLabelValues("completed", "admin", "alice", "966571e4cf855e95e5a94c4481217c4a")); v != 1 {
		t.Errorf("surviving entry=%v", v)
	}
	if v := testutil.ToFloat64(c.autorec); v != 0 {
		t.Errorf("autorec after drop=%v", v)
	}

	if n := count(t, reg, "tvheadend_dvr_entries"); n != 1 {
		t.Errorf("expected 1 surviving series, got %d", n)
	}
}

// TestDVRCollector_UnknownConfigFallsBackToUUID covers an entry whose
// config_name does not match any known DVRConfig uuid.
func TestDVRCollector_UnknownConfigFallsBackToUUID(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewDVR(reg)
	entries := []tvh.DVREntry{{
		UUID:        "d9",
		SchedStatus: "completed",
		Owner:       "admin",
		Creator:     "carol",
		ConfigUUID:  "unknown-uuid",
	}}
	c.Update(entries, nil, 0, 0)

	if v := testutil.ToFloat64(c.entries.vec.WithLabelValues("completed", "admin", "carol", "unknown-uuid")); v != 1 {
		t.Errorf("fallback config label=%v", v)
	}
}
