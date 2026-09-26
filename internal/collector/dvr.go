package collector

import (
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
)

// DVR exports recording inventory and health.
type DVR struct {
	entries, bytes, dataErrors, configInfo *labelSet
	active, upcoming, autorec, timerec     prometheus.Gauge
}

// NewDVR registers DVR metrics on reg.
func NewDVR(reg prometheus.Registerer) *DVR {
	g := func(name, help string, labels ...string) *labelSet {
		v := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_dvr_" + name, Help: help}, labels)
		reg.MustRegister(v)
		return newLabelSet(v)
	}
	s := func(name, help string) prometheus.Gauge {
		v := prometheus.NewGauge(prometheus.GaugeOpts{Name: "tvheadend_dvr_" + name, Help: help})
		reg.MustRegister(v)
		return v
	}
	return &DVR{
		entries:    g("entries", "DVR entries by schedule status, owner, creator and config.", "status", "owner", "creator", "config"),
		bytes:      g("recording_bytes", "Sum of recording file sizes.", "status", "config"),
		dataErrors: g("data_errors", "Sum of data errors across recordings.", "status"),
		configInfo: g("config_info", "DVR configuration; value is always 1.", "uuid", "name", "storage", "profile"),
		active:     s("active_recordings", "Recordings in progress."),
		upcoming:   s("upcoming_recordings", "Scheduled recordings."),
		autorec:    s("autorec_rules", "Auto-recording rules."),
		timerec:    s("timerec_rules", "Timer recording rules."),
	}
}

// Update applies one DVR poll.
func (d *DVR) Update(entries []tvh.DVREntry, configs []tvh.DVRConfig, autorec, timerec int) {
	for _, ls := range []*labelSet{d.entries, d.bytes, d.dataErrors, d.configInfo} {
		ls.begin()
	}
	name := make(map[string]string, len(configs))
	for _, c := range configs {
		name[c.UUID] = c.DisplayName()
		d.configInfo.set(1, c.UUID, c.DisplayName(), c.Storage, c.Profile)
	}
	var active, upcoming float64
	for _, e := range entries {
		cfg, ok := name[e.ConfigUUID]
		if !ok {
			cfg = e.ConfigUUID
		}
		d.entries.add(1, e.SchedStatus, e.Owner, e.Creator, cfg)
		d.bytes.add(float64(e.Filesize), e.SchedStatus, cfg)
		d.dataErrors.add(float64(e.DataErrors), e.SchedStatus)
		switch e.SchedStatus {
		case "recording":
			active++
		case "scheduled":
			upcoming++
		}
	}
	d.active.Set(active)
	d.upcoming.Set(upcoming)
	d.autorec.Set(float64(autorec))
	d.timerec.Set(float64(timerec))
	for _, ls := range []*labelSet{d.entries, d.bytes, d.dataErrors, d.configInfo} {
		ls.end()
	}
}
