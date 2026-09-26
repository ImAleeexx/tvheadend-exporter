// Package config loads exporter configuration from flags and environment.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	URL, Username, Password  string
	PollStatus, PollTopology time.Duration
	Timeout, SessionGrace    time.Duration
	GeoIPDB, Listen          string
	LogFormat, LogLevel      string
	TLSInsecureSkipVerify    bool
	DumpMetrics              bool
}

// Load parses args (flags) with environment defaults (TVH_ prefix).
// Flags win over env. TVH_PASSWORD_FILE wins over TVH_PASSWORD.
func Load(args []string, getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	var c Config
	fs := flag.NewFlagSet("tvheadend-exporter", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	// envErrs holds a parse error per flag name for a malformed TVH_* duration
	// env var. It is only surfaced for flags the caller did NOT explicitly set
	// on the command line (checked via fs.Visit after Parse), since a valid
	// flag override should win over a bad env default rather than fail Load.
	envErrs := map[string]error{}

	str := func(p *string, name, env, def, usage string) {
		if v := getenv(env); v != "" {
			def = v
		}
		fs.StringVar(p, name, def, usage+" ["+env+"]")
	}
	dur := func(p *time.Duration, name, env string, def time.Duration, usage string) {
		if v := getenv(env); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				envErrs[name] = fmt.Errorf("%s: invalid duration %q: %w", env, v, err)
			} else {
				def = d
			}
		}
		fs.DurationVar(p, name, def, usage+" ["+env+"]")
	}
	boolean := func(p *bool, name, env string, usage string) {
		def, _ := strconv.ParseBool(getenv(env))
		fs.BoolVar(p, name, def, usage+" ["+env+"]")
	}

	var passwordFile string
	str(&c.URL, "url", "TVH_URL", "", "Tvheadend base URL")
	str(&c.Username, "username", "TVH_USERNAME", "", "Tvheadend username")
	str(&c.Password, "password", "TVH_PASSWORD", "", "Tvheadend password")
	str(&passwordFile, "password-file", "TVH_PASSWORD_FILE", "", "file containing the password")
	dur(&c.PollStatus, "poll-status", "TVH_POLL_STATUS", 10*time.Second, "status poll interval")
	dur(&c.PollTopology, "poll-topology", "TVH_POLL_TOPOLOGY", 60*time.Second, "topology/DVR poll interval")
	dur(&c.Timeout, "timeout", "TVH_TIMEOUT", 5*time.Second, "HTTP request timeout")
	dur(&c.SessionGrace, "session-grace", "TVH_SESSION_GRACE", 60*time.Second, "end sessions after polls fail this long")
	str(&c.GeoIPDB, "geoip-db", "TVH_GEOIP_DB", "", "path to MaxMind City mmdb (empty = off)")
	str(&c.Listen, "listen", "TVH_LISTEN", ":9429", "listen address")
	str(&c.LogFormat, "log-format", "TVH_LOG_FORMAT", "json", "log format: json|text")
	str(&c.LogLevel, "log-level", "TVH_LOG_LEVEL", "info", "log level: debug|info|warn|error")
	boolean(&c.TLSInsecureSkipVerify, "tls-insecure-skip-verify", "TVH_TLS_INSECURE_SKIP_VERIFY", "skip TLS verification")
	fs.BoolVar(&c.DumpMetrics, "dump-metrics", false, "print metric catalogue as markdown and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stderr)
			fs.Usage()
		}
		return c, err
	}
	// A flag explicitly set on the command line overrides its env default,
	// including a malformed one, so drop any envErrs for flags that were set.
	fs.Visit(func(f *flag.Flag) {
		delete(envErrs, f.Name)
	})
	if len(envErrs) > 0 {
		errs := make([]error, 0, len(envErrs))
		for _, e := range envErrs {
			errs = append(errs, e)
		}
		return c, errors.Join(errs...)
	}
	if passwordFile != "" {
		b, err := readFile(passwordFile)
		if err != nil {
			return c, fmt.Errorf("read -password-file: %w", err)
		}
		c.Password = strings.TrimSpace(string(b))
	}
	return c, c.validate()
}

func (c Config) validate() error {
	var errs []error
	if c.URL == "" {
		errs = append(errs, errors.New("-url / TVH_URL is required"))
	} else if u, err := url.Parse(c.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, errors.New("-url must be an http(s) URL"))
	}
	if c.Username == "" {
		errs = append(errs, errors.New("-username / TVH_USERNAME is required"))
	}
	if c.Password == "" {
		errs = append(errs, errors.New("-password / TVH_PASSWORD or TVH_PASSWORD_FILE is required"))
	}
	for name, d := range map[string]time.Duration{
		"poll-status": c.PollStatus, "poll-topology": c.PollTopology, "timeout": c.Timeout, "session-grace": c.SessionGrace,
	} {
		if d <= 0 {
			errs = append(errs, fmt.Errorf("-%s must be > 0", name))
		}
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, errors.New("-log-format must be json or text"))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, errors.New("-log-level must be debug|info|warn|error"))
	}
	return errors.Join(errs...)
}
