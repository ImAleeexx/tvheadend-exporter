package collector

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestLabelSet_DeletesStale(t *testing.T) {
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "x"}, []string{"a", "b"})
	ls := newLabelSet(vec)
	ls.begin()
	ls.set(1, "p", "q")
	ls.set(2, "r", "s")
	ls.end()
	if n := testutil.CollectAndCount(vec); n != 2 {
		t.Fatalf("want 2 series, got %d", n)
	}
	ls.begin()
	ls.set(3, "p", "q")
	ls.end()
	if n := testutil.CollectAndCount(vec); n != 1 {
		t.Fatalf("want 1 series after sync, got %d", n)
	}
	if v := testutil.ToFloat64(vec.WithLabelValues("p", "q")); v != 3 {
		t.Errorf("want 3 got %v", v)
	}
}

func TestLabelSet_AddAccumulatesAndDeletesStale(t *testing.T) {
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "y"}, []string{"a", "b"})
	ls := newLabelSet(vec)
	ls.begin()
	ls.add(1, "p", "q")
	ls.add(2, "p", "q")
	ls.end()
	if v := testutil.ToFloat64(vec.WithLabelValues("p", "q")); v != 3 {
		t.Fatalf("want accumulated 3, got %v", v)
	}
	ls.begin()
	ls.end()
	if n := testutil.CollectAndCount(vec); n != 0 {
		t.Fatalf("want 0 series after untouched cycle, got %d", n)
	}
}

// count is a shared test helper used by all collector tests to gather a
// metric by name from any prometheus.Gatherer (e.g. a *prometheus.Registry).
func count(t *testing.T, g prometheus.Gatherer, name string) int {
	t.Helper()
	n, err := testutil.GatherAndCount(g, name)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCount_Helper exercises the shared count() helper directly so it is not
// flagged as unused within this file.
func TestCount_Helper(t *testing.T) {
	reg := prometheus.NewRegistry()
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_count_helper_test"}, []string{"a"})
	reg.MustRegister(vec)
	vec.WithLabelValues("p").Set(1)
	vec.WithLabelValues("q").Set(2)
	if n := count(t, reg, "tvheadend_count_helper_test"); n != 2 {
		t.Fatalf("want 2 series, got %d", n)
	}
}
