package collector

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Self exports exporter health metrics.
type Self struct {
	duration    *prometheus.HistogramVec
	errors      *prometheus.CounterVec
	lastSuccess *prometheus.GaugeVec
}

// NewSelf registers exporter self-metrics on reg.
func NewSelf(reg prometheus.Registerer, version, commit string) *Self {
	s := &Self{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tvheadend_exporter_poll_duration_seconds", Help: "Duration of one poll cycle per group.",
			Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"group"}),
		errors:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tvheadend_exporter_poll_errors_total", Help: "Failed poll cycles per group."}, []string{"group"}),
		lastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_exporter_last_success_timestamp_seconds", Help: "Unix time of the last successful poll per group."}, []string{"group"}),
	}
	build := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_exporter_build_info", Help: "Exporter build; value is always 1."}, []string{"version", "commit"})
	build.WithLabelValues(version, commit).Set(1)
	reg.MustRegister(s.duration, s.errors, s.lastSuccess, build)
	return s
}

// Observe records one poll cycle result.
func (s *Self) Observe(group string, d time.Duration, err error) {
	s.duration.WithLabelValues(group).Observe(d.Seconds())
	s.errors.WithLabelValues(group).Add(0)
	if err != nil {
		s.errors.WithLabelValues(group).Inc()
		return
	}
	s.lastSuccess.WithLabelValues(group).Set(float64(time.Now().Unix()))
}
