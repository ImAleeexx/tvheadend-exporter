package collector

import (
	"strconv"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
)

// Topology exports network/mux/service/channel inventory counts.
type Topology struct {
	networkInfo, networkMuxes, networkServices, networkChannels, networkScanQ *labelSet
	muxes, services, channels, channelsByTag                                  *labelSet
	channelTags                                                               prometheus.Gauge
}

// NewTopology registers topology metrics on reg.
func NewTopology(reg prometheus.Registerer) *Topology {
	g := func(name, help string, labels ...string) *labelSet {
		v := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_" + name, Help: help}, labels)
		reg.MustRegister(v)
		return newLabelSet(v)
	}
	t := &Topology{
		networkInfo:     g("network_info", "Network; value is always 1.", "uuid", "name", "type", "enabled"),
		networkMuxes:    g("network_muxes", "Muxes in the network (as reported).", "name"),
		networkServices: g("network_services", "Services in the network (as reported).", "name"),
		networkChannels: g("network_channels", "Channels mapped from the network (as reported).", "name"),
		networkScanQ:    g("network_scan_queue_length", "Muxes queued for scanning.", "name"),
		muxes:           g("muxes", "Muxes by network, scan result and enabled state.", "network", "scan_result", "enabled"),
		services:        g("services", "Services by network, encryption, enabled state and channel mapping.", "network", "encrypted", "enabled", "mapped"),
		channels:        g("channels", "Channels by enabled state.", "enabled"),
		channelsByTag:   g("channels_by_tag", "Channels carrying each tag.", "tag"),
		channelTags:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "tvheadend_channel_tags", Help: "Number of channel tags."}),
	}
	reg.MustRegister(t.channelTags)
	return t
}

// Update applies one topology poll.
func (t *Topology) Update(nets []tvh.Network, muxes []tvh.Mux, svcs []tvh.Service, chans []tvh.Channel, tags []tvh.ChannelTag) {
	b := strconv.FormatBool
	all := []*labelSet{t.networkInfo, t.networkMuxes, t.networkServices, t.networkChannels, t.networkScanQ, t.muxes, t.services, t.channels, t.channelsByTag}
	for _, ls := range all {
		ls.begin()
	}
	for _, n := range nets {
		t.networkInfo.set(1, n.UUID, n.Name, n.Type(), b(n.Enabled.Bool()))
		t.networkMuxes.set(float64(n.NumMux), n.Name)
		t.networkServices.set(float64(n.NumSvc), n.Name)
		t.networkChannels.set(float64(n.NumChn), n.Name)
		t.networkScanQ.set(float64(n.ScanQLength), n.Name)
	}
	for _, m := range muxes {
		t.muxes.add(1, m.Network, m.ScanResultName(), b(m.Enabled.Bool()))
	}
	for _, s := range svcs {
		t.services.add(1, s.Network, b(s.Encrypted.Bool()), b(s.Enabled.Bool()), b(len(s.Channel) > 0))
	}
	t.channels.add(0, "true")
	t.channels.add(0, "false")
	tagName := make(map[string]string, len(tags))
	for _, tg := range tags {
		tagName[tg.UUID] = tg.Name
		t.channelsByTag.add(0, tg.Name)
	}
	for _, ch := range chans {
		t.channels.add(1, b(ch.Enabled.Bool()))
		for _, id := range ch.Tags {
			if name, ok := tagName[id]; ok {
				t.channelsByTag.add(1, name)
			}
		}
	}
	t.channelTags.Set(float64(len(tags)))
	for _, ls := range all {
		ls.end()
	}
}
