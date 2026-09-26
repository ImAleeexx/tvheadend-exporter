// Package collector maps Tvheadend API data onto Prometheus metrics.
package collector

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// labelSet wraps a GaugeVec so that series not refreshed during a
// begin()/end() cycle are deleted instead of lingering with stale values.
type labelSet struct {
	vec  *prometheus.GaugeVec
	live map[string][]string
	seen map[string]bool
}

func newLabelSet(vec *prometheus.GaugeVec) *labelSet {
	return &labelSet{vec: vec, live: map[string][]string{}}
}

func (l *labelSet) begin() { l.seen = make(map[string]bool, len(l.live)) }

func (l *labelSet) set(v float64, lvs ...string) {
	k := strings.Join(lvs, "\x00")
	l.vec.WithLabelValues(lvs...).Set(v)
	l.live[k] = lvs
	l.seen[k] = true
}

// add increments the value for the label set within the cycle (for counting rows).
func (l *labelSet) add(v float64, lvs ...string) {
	k := strings.Join(lvs, "\x00")
	if !l.seen[k] {
		l.vec.WithLabelValues(lvs...).Set(0)
	}
	l.vec.WithLabelValues(lvs...).Add(v)
	l.live[k] = lvs
	l.seen[k] = true
}

func (l *labelSet) end() {
	for k, lvs := range l.live {
		if !l.seen[k] {
			l.vec.DeleteLabelValues(lvs...)
			delete(l.live, k)
		}
	}
	l.seen = nil
}
