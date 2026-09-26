package tvh_test

// External test package: fake imports tvh, so only tvh_test may import fake.
// This keeps the endpoint -> fixture map in one place (fake.DefaultFixtures).

import (
	"context"
	"testing"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/fake"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
)

func TestAllEndpointsDecodeFixtures(t *testing.T) {
	srv := fake.NewServer(t, "user", "pw")
	c, err := tvh.New(srv.URL(), "user", "pw", 2*time.Second, false, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if v, err := c.Networks(ctx); err != nil || v[0].Type() != "iptv" || v[2].Type() != "dvbs" {
		t.Errorf("networks: %v %v", v, err)
	}
	if v, err := c.Muxes(ctx); err != nil || v[1].ScanResultName() != "fail" || v[2].Enabled {
		t.Errorf("muxes: %v %v", v, err)
	}
	if v, err := c.Services(ctx); err != nil || len(v[0].Channel) != 1 || !v[1].Encrypted {
		t.Errorf("services: %v %v", v, err)
	}
	if v, err := c.Channels(ctx); err != nil || len(v) != 2 {
		t.Errorf("channels: %v %v", v, err)
	}
	if v, err := c.ChannelTags(ctx); err != nil || len(v) != 3 {
		t.Errorf("tags: %v %v", v, err)
	}
	if v, err := c.AccessEntries(ctx); err != nil || v[0].ConnLimit != 4 {
		t.Errorf("access: %v %v", v, err)
	}
	if v, err := c.DVREntries(ctx); err != nil || v[0].Filesize != 1373117220 || v[3].SchedStatus != "recording" {
		t.Errorf("dvr: %v %v", v, err)
	}
	if v, err := c.DVRConfigs(ctx); err != nil || v[1].DisplayName() != "966571e4cf855e95e5a94c4481217c4a" {
		t.Errorf("dvrcfg: %v %v", v, err)
	}
	if v, err := c.Inputs(ctx); err != nil || v[1].CC != 387 {
		t.Errorf("inputs: %v %v", v, err)
	}
	if v, err := c.Connections(ctx); err != nil || v[2].User != "" || v[0].Type != "HTSP" {
		t.Errorf("conns: %v %v", v, err)
	}
	if v, err := c.ServerInfo(ctx); err != nil || v.APIVersion != 19 {
		t.Errorf("serverinfo: %v %v", v, err)
	}
	if n, err := c.DVRAutorecCount(ctx); err != nil || n != 0 {
		t.Errorf("autorec: %d %v", n, err)
	}
	if n, err := c.DVRTimerecCount(ctx); err != nil || n != 0 {
		t.Errorf("timerec: %d %v", n, err)
	}
}
