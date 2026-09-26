package collector

import (
	"strconv"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
)

var inputLabels = []string{"uuid", "input"}

// inputGaugeSpec maps a metric suffix to the field that feeds it.
type inputGaugeSpec struct {
	name string
	help string
	get  func(tvh.Input) int64
}

var inputGauges = []inputGaugeSpec{
	{"subscriptions", "Subscriptions attached to the input.", func(i tvh.Input) int64 { return int64(i.Subs) }},
	{"bitrate_bps", "Input bitrate.", func(i tvh.Input) int64 { return int64(i.BPS) }},
	{"signal", "Signal strength (raw).", func(i tvh.Input) int64 { return int64(i.Signal) }},
	{"signal_scale", "Signal scale (0=none,1=relative,2=dBm).", func(i tvh.Input) int64 { return int64(i.SignalScale) }},
	{"snr", "Signal to noise ratio (raw).", func(i tvh.Input) int64 { return int64(i.SNR) }},
	{"snr_scale", "SNR scale (0=none,1=relative,2=dB).", func(i tvh.Input) int64 { return int64(i.SNRScale) }},
	{"ber", "Bit error rate.", func(i tvh.Input) int64 { return int64(i.BER) }},
	{"unc", "Uncorrected blocks (as reported).", func(i tvh.Input) int64 { return int64(i.UNC) }},
	{"transport_errors", "Transport errors (as reported).", func(i tvh.Input) int64 { return int64(i.TE) }},
	{"continuity_errors", "Continuity errors (as reported).", func(i tvh.Input) int64 { return int64(i.CC) }},
	{"ec_bit", "Error bit count.", func(i tvh.Input) int64 { return int64(i.ECBit) }},
	{"tc_bit", "Total bit count.", func(i tvh.Input) int64 { return int64(i.TCBit) }},
	{"ec_block", "Error block count.", func(i tvh.Input) int64 { return int64(i.ECBlock) }},
	{"tc_block", "Total block count.", func(i tvh.Input) int64 { return int64(i.TCBlock) }},
}

// inputCounterSpec maps a monotonic (reset-safe) counter to its raw source field.
type inputCounterSpec struct {
	name string
	help string
	get  func(tvh.Input) int64
}

var inputCounters = []inputCounterSpec{
	{"continuity_errors_total", "Continuity errors, monotonic across retunes.", func(i tvh.Input) int64 { return int64(i.CC) }},
	{"transport_errors_total", "Transport errors, monotonic across retunes.", func(i tvh.Input) int64 { return int64(i.TE) }},
	{"unc_total", "Uncorrected blocks, monotonic across retunes.", func(i tvh.Input) int64 { return int64(i.UNC) }},
}

// Inputs exports tuner/IPTV input health: current-tune info, gauges as
// reported by Tvheadend, and reset-safe cumulative error counters that
// survive retunes (spec §4.2).
type Inputs struct {
	info     *labelSet
	gauges   map[string]*labelSet
	counters map[string]*prometheus.CounterVec
	last     map[string]map[string]int64 // uuid -> counter name -> last raw value
}

// NewInputs registers input metrics on reg.
func NewInputs(reg prometheus.Registerer) *Inputs {
	c := &Inputs{
		info: newLabelSet(prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tvheadend_input_info",
			Help: "Current tune of the input; value is always 1.",
		}, []string{"uuid", "input", "stream", "weight"})),
		gauges:   map[string]*labelSet{},
		counters: map[string]*prometheus.CounterVec{},
		last:     map[string]map[string]int64{},
	}
	reg.MustRegister(c.info.vec)
	for _, g := range inputGauges {
		v := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_input_" + g.name, Help: g.help}, inputLabels)
		c.gauges[g.name] = newLabelSet(v)
		reg.MustRegister(v)
	}
	for _, k := range inputCounters {
		v := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tvheadend_input_" + k.name, Help: k.help}, inputLabels)
		c.counters[k.name] = v
		reg.MustRegister(v)
	}
	return c
}

// Update applies one api/status/inputs snapshot: it refreshes gauges and the
// current-tune info series (deleting any not present in this snapshot), and
// advances the reset-safe counters by the observed delta since the last
// snapshot. A counter value that decreases (e.g. after a retune resets the
// input's internal counters) is treated as if it started fresh from zero, so
// the cumulative total never moves backwards.
func (c *Inputs) Update(inputs []tvh.Input) {
	c.info.begin()
	for _, g := range c.gauges {
		g.begin()
	}
	seen := map[string]bool{}
	for _, in := range inputs {
		seen[in.UUID] = true
		c.info.set(1, in.UUID, in.Input, in.Stream, strconv.FormatInt(int64(in.Weight), 10))
		for _, g := range inputGauges {
			c.gauges[g.name].set(float64(g.get(in)), in.UUID, in.Input)
		}

		prev := c.last[in.UUID]
		if prev == nil {
			prev = map[string]int64{}
			c.last[in.UUID] = prev
		}
		for _, k := range inputCounters {
			cur := k.get(in)
			last, ok := prev[k.name]
			switch {
			case !ok:
				c.counters[k.name].WithLabelValues(in.UUID, in.Input).Add(0)
			case cur >= last:
				c.counters[k.name].WithLabelValues(in.UUID, in.Input).Add(float64(cur - last))
			default: // reset: the new value is all new errors
				c.counters[k.name].WithLabelValues(in.UUID, in.Input).Add(float64(cur))
			}
			prev[k.name] = cur
		}
	}
	c.info.end()
	for _, g := range c.gauges {
		g.end()
	}

	for uuid := range c.last {
		if seen[uuid] {
			continue
		}
		delete(c.last, uuid)
		for _, v := range c.counters {
			v.DeletePartialMatch(prometheus.Labels{"uuid": uuid})
		}
	}
}
