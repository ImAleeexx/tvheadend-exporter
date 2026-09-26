// Package fake is an httptest Tvheadend serving fixture JSON from
// internal/testdata. It is for tests only and never talks to a real server.
package fake

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

// DefaultFixtures maps every logical endpoint name (see tvh.Endpoints) to the
// internal/testdata fixture the fake serves for it by default.
var DefaultFixtures = map[string]string{
	"serverinfo":    "serverinfo.json",
	"subscriptions": "subscriptions.json",
	"connections":   "connections.json",
	"inputs":        "inputs.json",
	"networks":      "networks.json",
	"muxes":         "muxes.json",
	"services":      "services.json",
	"channels":      "channels.json",
	"channeltags":   "channeltags.json",
	"access":        "access.json",
	"dvr_entries":   "dvr_entries.json",
	"dvr_configs":   "dvr_configs.json",
	"dvr_autorec":   "empty_grid.json",
	"dvr_timerec":   "empty_grid.json",
}

// Server is a fake Tvheadend.
type Server struct {
	srv      *httptest.Server
	dir      string
	mu       sync.Mutex
	files    map[string]string // api path -> fixture file
	fails    map[string]int    // api path -> status code
	problems []string          // protocol violations seen by the handler
}

// NewServer starts the fake with all default fixtures, accepting only the
// given Basic-auth credentials. The handler never calls t from its own
// goroutine; violations (non-GET requests, passwd/entry/grid access, missing
// fixtures) are recorded and reported as test errors at cleanup, after the
// server has been closed.
func NewServer(t testing.TB, user, pass string) *Server {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	s := &Server{
		dir:   filepath.Join(filepath.Dir(self), "..", "testdata"),
		files: map[string]string{},
		fails: map[string]int{},
	}
	for name, file := range DefaultFixtures {
		s.files[tvh.Endpoints[name]] = file
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve(user, pass)))
	// Cleanups run LIFO: Close (registered second) runs first and waits for
	// in-flight requests, so the problem check sees every request.
	t.Cleanup(func() {
		for _, p := range s.Problems() {
			t.Errorf("fake tvheadend: %s", p)
		}
	})
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) serve(user, pass string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/")
		if r.Method != http.MethodGet {
			s.record("non-GET request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(path, "passwd/") {
			s.record("forbidden request to %s", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if u, p, ok := r.BasicAuth(); !ok || u != user || p != pass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		code, failing := s.fails[path]
		file, ok := s.files[path]
		s.mu.Unlock()
		if failing {
			w.WriteHeader(code)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, err := os.ReadFile(filepath.Join(s.dir, file))
		if err != nil {
			s.record("fixture %s: %v", file, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}
}

func (s *Server) record(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problems = append(s.problems, fmt.Sprintf(format, args...))
}

// URL is the base URL.
func (s *Server) URL() string { return s.srv.URL }

// Set swaps the fixture served for a logical endpoint name (see tvh.Endpoints)
// and clears any failure set for it.
func (s *Server) Set(endpoint, fixtureFile string) {
	path := s.path(endpoint)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = fixtureFile
	delete(s.fails, path)
}

// Fail makes an endpoint return code until Set is called again.
func (s *Server) Fail(endpoint string, code int) {
	path := s.path(endpoint)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails[path] = code
}

// Problems returns the protocol violations recorded so far.
func (s *Server) Problems() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.problems...)
}

// ResetProblems discards recorded violations (for tests that provoke them).
func (s *Server) ResetProblems() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problems = nil
}

func (s *Server) path(endpoint string) string {
	p, ok := tvh.Endpoints[endpoint]
	if !ok {
		panic("fake: unknown endpoint " + endpoint)
	}
	return p
}
