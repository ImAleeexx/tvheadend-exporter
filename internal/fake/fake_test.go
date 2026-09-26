package fake_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/imaleeexx/tvheadend-exporter/internal/fake"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

func do(t *testing.T, method, url, user, pass string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestDefaultFixturesCoverEveryEndpoint(t *testing.T) {
	for name := range tvh.Endpoints {
		if fake.DefaultFixtures[name] == "" {
			t.Errorf("no default fixture for endpoint %q", name)
		}
	}
	for name := range fake.DefaultFixtures {
		if _, ok := tvh.Endpoints[name]; !ok {
			t.Errorf("fixture for unknown endpoint %q", name)
		}
	}
}

func TestServer_AuthSetFail(t *testing.T) {
	s := fake.NewServer(t, "u", "p")
	url := s.URL() + "/api/" + tvh.Endpoints["subscriptions"]

	if code, _ := do(t, http.MethodGet, url, "u", "wrong"); code != http.StatusUnauthorized {
		t.Errorf("bad credentials: code %d, want 401", code)
	}
	if code, body := do(t, http.MethodGet, url, "u", "p"); code != 200 || !strings.Contains(body, `"id":15268`) {
		t.Errorf("default fixture: %d %q", code, body)
	}
	s.Set("subscriptions", "empty_grid.json")
	if code, body := do(t, http.MethodGet, url, "u", "p"); code != 200 || strings.Contains(body, "15268") {
		t.Errorf("after Set: %d %q", code, body)
	}
	s.Fail("subscriptions", http.StatusBadGateway)
	if code, _ := do(t, http.MethodGet, url, "u", "p"); code != http.StatusBadGateway {
		t.Errorf("after Fail: code %d, want 502", code)
	}
	s.Set("subscriptions", "subscriptions.json")
	if code, _ := do(t, http.MethodGet, url, "u", "p"); code != 200 {
		t.Errorf("Set must clear Fail: code %d", code)
	}
	if code, _ := do(t, http.MethodGet, s.URL()+"/api/nope", "u", "p"); code != http.StatusNotFound {
		t.Errorf("unknown path: code %d, want 404", code)
	}
}

func TestServer_MissingFixtureIs500AndRecorded(t *testing.T) {
	s := fake.NewServer(t, "u", "p")
	s.Set("channels", "does-not-exist.json")
	if code, _ := do(t, http.MethodGet, s.URL()+"/api/"+tvh.Endpoints["channels"], "u", "p"); code != http.StatusInternalServerError {
		t.Errorf("missing fixture: code %d, want 500", code)
	}
	if p := s.Problems(); len(p) != 1 || !strings.Contains(p[0], "does-not-exist.json") {
		t.Errorf("problems = %q", p)
	}
	s.ResetProblems() // expected; keep the cleanup check from failing the test
}

func TestServer_RecordsForbiddenRequests(t *testing.T) {
	s := fake.NewServer(t, "u", "p")
	if code, _ := do(t, http.MethodGet, s.URL()+"/api/passwd/entry/grid", "u", "p"); code != http.StatusForbidden {
		t.Errorf("passwd grid: code %d, want 403", code)
	}
	if code, _ := do(t, http.MethodPost, s.URL()+"/api/"+tvh.Endpoints["channels"], "u", "p"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST: code %d, want 405", code)
	}
	if p := s.Problems(); len(p) != 2 {
		t.Errorf("want 2 recorded problems, got %q", p)
	}
	s.ResetProblems()
}
