// Package geoip enriches peer IPs with country/city labels.
package geoip

import (
	"net"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/oschwald/geoip2-golang"
	"github.com/prometheus/client_golang/prometheus"
)

// Location is the resolved geo labels; empty strings when unknown.
type Location struct{ Country, City string }

// Resolver maps an IP to a Location.
type Resolver interface{ Lookup(ip string) Location }

// Noop never resolves.
type Noop struct{}

// Lookup implements Resolver.
func (Noop) Lookup(string) Location { return Location{} }

// MaxMind resolves through a City mmdb with an LRU cache.
type MaxMind struct {
	path    string
	mu      sync.RWMutex
	db      *geoip2.Reader
	cache   *lru.Cache[string, Location]
	lookups *prometheus.CounterVec
}

// Open loads the database at path.
func Open(path string, reg prometheus.Registerer) (*MaxMind, error) {
	db, err := geoip2.Open(path)
	if err != nil {
		return nil, err
	}
	cache, _ := lru.New[string, Location](10000)
	m := &MaxMind{path: path, db: db, cache: cache,
		lookups: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tvheadend_exporter_geoip_lookups_total", Help: "GeoIP lookups by result.",
		}, []string{"result"}),
	}
	for _, r := range []string{"hit", "miss", "private", "error"} {
		m.lookups.WithLabelValues(r)
	}
	if reg != nil {
		reg.MustRegister(m.lookups)
	}
	return m, nil
}

// Lookup implements Resolver. Errors never propagate; they yield an empty Location.
func (m *MaxMind) Lookup(ip string) Location {
	if l, ok := m.cache.Get(ip); ok {
		return l
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		m.lookups.WithLabelValues("error").Inc()
		return Location{}
	}
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		m.lookups.WithLabelValues("private").Inc()
		l := Location{Country: "private"}
		m.cache.Add(ip, l)
		return l
	}
	m.mu.RLock()
	rec, err := m.db.City(addr)
	m.mu.RUnlock()
	if err != nil {
		m.lookups.WithLabelValues("error").Inc()
		return Location{}
	}
	l := Location{Country: rec.Country.IsoCode, City: rec.City.Names["en"]}
	if l.Country == "" {
		m.lookups.WithLabelValues("miss").Inc()
	} else {
		m.lookups.WithLabelValues("hit").Inc()
	}
	m.cache.Add(ip, l)
	return l
}

// Reload reopens the database file and clears the cache.
func (m *MaxMind) Reload() error {
	db, err := geoip2.Open(m.path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	old := m.db
	m.db = db
	m.mu.Unlock()
	m.cache.Purge()
	return old.Close()
}

// Close releases the database.
func (m *MaxMind) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.db.Close()
}
