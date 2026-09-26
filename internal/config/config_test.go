package config

import (
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func noFile(string) ([]byte, error) { return nil, errors.New("no file") }

func TestLoad_DefaultsFromEnv(t *testing.T) {
	c, err := Load(nil, env(map[string]string{
		"TVH_URL": "http://tvh:9981", "TVH_USERNAME": "u", "TVH_PASSWORD": "p",
	}), noFile)
	if err != nil {
		t.Fatal(err)
	}
	if c.PollStatus != 10*time.Second || c.PollTopology != 60*time.Second ||
		c.Timeout != 5*time.Second || c.SessionGrace != 60*time.Second ||
		c.Listen != ":9429" || c.LogFormat != "json" || c.LogLevel != "info" {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestLoad_FlagOverridesEnv(t *testing.T) {
	c, err := Load([]string{"-poll-status", "3s", "-listen", ":1"}, env(map[string]string{
		"TVH_URL": "http://tvh:9981", "TVH_USERNAME": "u", "TVH_PASSWORD": "p", "TVH_POLL_STATUS": "20s",
	}), noFile)
	if err != nil {
		t.Fatal(err)
	}
	if c.PollStatus != 3*time.Second || c.Listen != ":1" {
		t.Errorf("flag did not override: %+v", c)
	}
}

func TestLoad_PasswordFileTakesPrecedence(t *testing.T) {
	c, err := Load(nil, env(map[string]string{
		"TVH_URL": "http://tvh:9981", "TVH_USERNAME": "u", "TVH_PASSWORD": "envpw", "TVH_PASSWORD_FILE": "/secret",
	}), func(p string) ([]byte, error) { return []byte("filepw\n"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != "filepw" {
		t.Errorf("want filepw got %q", c.Password)
	}
}

func TestLoad_Errors(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing url", map[string]string{"TVH_USERNAME": "u", "TVH_PASSWORD": "p"}, "TVH_URL"},
		{"bad scheme", map[string]string{"TVH_URL": "ftp://x", "TVH_USERNAME": "u", "TVH_PASSWORD": "p"}, "http"},
		{"missing user", map[string]string{"TVH_URL": "http://x", "TVH_PASSWORD": "p"}, "TVH_USERNAME"},
		{"missing password", map[string]string{"TVH_URL": "http://x", "TVH_USERNAME": "u"}, "TVH_PASSWORD"},
		{"bad log format", map[string]string{"TVH_URL": "http://x", "TVH_USERNAME": "u", "TVH_PASSWORD": "p", "TVH_LOG_FORMAT": "xml"}, "log-format"},
		{"zero poll", map[string]string{"TVH_URL": "http://x", "TVH_USERNAME": "u", "TVH_PASSWORD": "p", "TVH_POLL_STATUS": "0s"}, "poll-status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(nil, env(tc.env), noFile)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// TestLoad_InvalidEnvDuration covers preflight finding N10: a malformed
// TVH_ duration env value must not be silently ignored — Load must return
// an error identifying the offending variable.
func TestLoad_InvalidEnvDuration(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			"poll-status",
			map[string]string{"TVH_URL": "http://x", "TVH_USERNAME": "u", "TVH_PASSWORD": "p", "TVH_POLL_STATUS": "not-a-duration"},
			"TVH_POLL_STATUS",
		},
		{
			"poll-topology",
			map[string]string{"TVH_URL": "http://x", "TVH_USERNAME": "u", "TVH_PASSWORD": "p", "TVH_POLL_TOPOLOGY": "banana"},
			"TVH_POLL_TOPOLOGY",
		},
		{
			"timeout",
			map[string]string{"TVH_URL": "http://x", "TVH_USERNAME": "u", "TVH_PASSWORD": "p", "TVH_TIMEOUT": "5"},
			"TVH_TIMEOUT",
		},
		{
			"session-grace",
			map[string]string{"TVH_URL": "http://x", "TVH_USERNAME": "u", "TVH_PASSWORD": "p", "TVH_SESSION_GRACE": "xyz"},
			"TVH_SESSION_GRACE",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(nil, env(tc.env), noFile)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// TestLoad_HelpFlagPrintsUsageToStderr covers preflight finding X31/N8:
// -h must print flag usage instead of being silently discarded via
// fs.SetOutput(io.Discard). It should surface flag.ErrHelp so callers can
// exit(0), and the usage text must land on stderr.
func TestLoad_HelpFlagPrintsUsageToStderr(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	_, loadErr := Load([]string{"-h"}, env(map[string]string{
		"TVH_URL": "http://tvh:9981", "TVH_USERNAME": "u", "TVH_PASSWORD": "p",
	}), noFile)

	if cerr := w.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if !errors.Is(loadErr, flag.ErrHelp) {
		t.Errorf("want flag.ErrHelp, got %v", loadErr)
	}
	if !strings.Contains(string(out), "-url") {
		t.Errorf("want usage text on stderr, got %q", string(out))
	}
}
