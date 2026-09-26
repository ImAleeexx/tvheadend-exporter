package geoip

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNoop(t *testing.T) {
	if l := (Noop{}).Lookup("81.2.69.160"); l != (Location{}) {
		t.Errorf("got %+v", l)
	}
}

func TestMaxMind(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := Open("../testdata/GeoIP2-City-Test.mmdb", reg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if l := m.Lookup("81.2.69.160"); l.Country != "GB" || l.City != "London" {
		t.Errorf("london: %+v", l)
	}
	if l := m.Lookup("81.2.69.160"); l.City != "London" { // cached
		t.Errorf("cached: %+v", l)
	}
	if l := m.Lookup("192.168.1.5"); l.Country != "private" || l.City != "" {
		t.Errorf("private: %+v", l)
	}
	if l := m.Lookup("127.0.0.1"); l.Country != "private" {
		t.Errorf("loopback: %+v", l)
	}
	if l := m.Lookup("not-an-ip"); l != (Location{}) {
		t.Errorf("garbage: %+v", l)
	}
	if l := m.Lookup("203.0.113.1"); l != (Location{}) { // TEST-NET not in DB
		t.Errorf("miss: %+v", l)
	}
	for res, want := range map[string]float64{"hit": 1, "private": 2, "error": 1, "miss": 1} {
		if got := testutil.ToFloat64(m.lookups.WithLabelValues(res)); got != want {
			t.Errorf("lookups{%s}=%v want %v", res, got, want)
		}
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
}

func TestOpen_Missing(t *testing.T) {
	if _, err := Open("/nonexistent.mmdb", nil); err == nil {
		t.Fatal("want error")
	}
}
