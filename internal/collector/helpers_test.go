package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

var t0 = time.Date(2026, 9, 26, 20, 0, 0, 0, time.UTC)

func loadFixture[T any](t *testing.T, name string) []T {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var g struct{ Entries []T }
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g.Entries
}

func TestLoadFixture(t *testing.T) {
	if subs := loadFixture[tvh.Subscription](t, "subscriptions.json"); len(subs) == 0 {
		t.Fatalf("no subscriptions decoded at %s", t0)
	}
}
