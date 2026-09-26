package collector

import (
	"strings"
	"testing"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestTopologyCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewTopology(reg)
	c.Update(loadFixture[tvh.Network](t, "networks.json"), loadFixture[tvh.Mux](t, "muxes.json"),
		loadFixture[tvh.Service](t, "services.json"), loadFixture[tvh.Channel](t, "channels.json"),
		loadFixture[tvh.ChannelTag](t, "channeltags.json"))

	get := func(ls *labelSet, lv ...string) float64 { return testutil.ToFloat64(ls.vec.WithLabelValues(lv...)) }
	if v := get(c.networkInfo, "4d749bc77a453903128bb427afbb8a8b", "THOTH", "iptv", "true"); v != 1 {
		t.Errorf("network_info THOTH=%v", v)
	}
	if v := get(c.networkInfo, "11111111111111111111111111111111", "Satelite", "dvbs", "false"); v != 1 { //nolint:misspell // matches the fixture's network name verbatim
		t.Errorf("network_info Satelite=%v", v) //nolint:misspell // matches the fixture's network name verbatim
	}
	if v := get(c.networkMuxes, "THOTH"); v != 206 {
		t.Errorf("network_muxes=%v", v)
	}
	if v := get(c.networkScanQ, "Satelite"); v != 2 { //nolint:misspell // matches the fixture's network name verbatim
		t.Errorf("scanq=%v", v)
	}
	if v := get(c.muxes, "THOTH", "ok", "true"); v != 1 {
		t.Errorf("muxes ok=%v", v)
	}
	if v := get(c.muxes, "THOTH", "fail", "true"); v != 1 {
		t.Errorf("muxes fail=%v", v)
	}
	if v := get(c.muxes, "Satelite", "partial", "false"); v != 1 { //nolint:misspell // matches the fixture's network name verbatim
		t.Errorf("muxes partial=%v", v)
	}
	if v := get(c.services, "Satelite", "true", "true", "false"); v != 1 { //nolint:misspell // matches the fixture's network name verbatim
		t.Errorf("services enc/enabled/unmapped=%v", v)
	}
	if v := get(c.services, "THOTH", "false", "true", "true"); v != 1 {
		t.Errorf("services mapped=%v", v)
	}
	if v := get(c.channels, "true"); v != 1 {
		t.Errorf("channels enabled=%v", v)
	}
	if v := get(c.channels, "false"); v != 1 {
		t.Errorf("channels disabled=%v", v)
	}
	if v := get(c.channelsByTag, "Deportes"); v != 2 {
		t.Errorf("by tag Deportes=%v", v)
	}
	if v := get(c.channelsByTag, "Empty"); v != 0 {
		t.Errorf("by tag Empty=%v (unused tags must still appear as 0)", v)
	}
	if v := testutil.ToFloat64(c.channelTags); v != 3 {
		t.Errorf("channel_tags=%v", v)
	}
	if problems, _ := testutil.GatherAndLint(reg); len(problems) > 0 {
		t.Errorf("lint: %v", problems)
	}
}

// TestTopologyCollector_ChannelsByTagExposition pins the exposition text for
// the channels_by_tag gauge, including the pre-seeded zero series for an
// unused tag (S4: at least one exposition-text comparison per collector).
func TestTopologyCollector_ChannelsByTagExposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewTopology(reg)
	c.Update(loadFixture[tvh.Network](t, "networks.json"), loadFixture[tvh.Mux](t, "muxes.json"),
		loadFixture[tvh.Service](t, "services.json"), loadFixture[tvh.Channel](t, "channels.json"),
		loadFixture[tvh.ChannelTag](t, "channeltags.json"))

	const expected = `
# HELP tvheadend_channels_by_tag Channels carrying each tag.
# TYPE tvheadend_channels_by_tag gauge
tvheadend_channels_by_tag{tag="Deportes"} 2
tvheadend_channels_by_tag{tag="Empty"} 0
tvheadend_channels_by_tag{tag="Musica"} 1
`
	if err := testutil.CollectAndCompare(c.channelsByTag.vec, strings.NewReader(expected), "tvheadend_channels_by_tag"); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
