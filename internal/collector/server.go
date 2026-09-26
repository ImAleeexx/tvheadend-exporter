package collector

import (
	"strconv"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
)

// Server exports Tvheadend identity and reachability.
type Server struct {
	up                    prometheus.Gauge
	buildInfo, capability *labelSet
}

// NewServer registers server metrics on reg.
func NewServer(reg prometheus.Registerer) *Server {
	s := &Server{
		up: prometheus.NewGauge(prometheus.GaugeOpts{Name: "tvheadend_up", Help: "1 if the last status poll succeeded."}),
		buildInfo: newLabelSet(prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_build_info", Help: "Tvheadend version; value is always 1."},
			[]string{"version", "api_version", "server_name"})),
		capability: newLabelSet(prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_capability", Help: "Tvheadend build capability; value is always 1."},
			[]string{"name"})),
	}
	reg.MustRegister(s.up, s.buildInfo.vec, s.capability.vec)
	return s
}

// Update applies serverinfo.
func (s *Server) Update(info tvh.ServerInfo) {
	s.buildInfo.begin()
	s.buildInfo.set(1, info.SWVersion, strconv.FormatInt(info.APIVersion.Int64(), 10), info.Name)
	s.buildInfo.end()
	s.capability.begin()
	for _, c := range info.Capabilities {
		s.capability.set(1, c)
	}
	s.capability.end()
}

// SetUp sets tvheadend_up.
func (s *Server) SetUp(up bool) {
	if up {
		s.up.Set(1)
	} else {
		s.up.Set(0)
	}
}
