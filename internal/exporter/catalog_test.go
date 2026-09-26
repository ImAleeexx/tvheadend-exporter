package exporter

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCatalog(t *testing.T) {
	reg := newRecordingRegistry()
	reg.MustRegister(
		prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_x", Help: "X help."}, []string{"a", "b"}),
		prometheus.NewCounter(prometheus.CounterOpts{Name: "tvheadend_y_total", Help: "Y help."}),
		prometheus.NewHistogram(prometheus.HistogramOpts{Name: "tvheadend_session_duration_seconds", Help: "H."}),
	)
	md := Catalog(reg)
	for _, want := range []string{
		"| `tvheadend_session_duration_seconds` | histogram | — | H. |",
		"| `tvheadend_x` | gauge | `a`, `b` | X help. |",
		"| `tvheadend_y_total` | counter | — | Y help. |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Index(md, "tvheadend_session_duration_seconds") > strings.Index(md, "tvheadend_x") {
		t.Error("rows must be sorted by name")
	}
}
