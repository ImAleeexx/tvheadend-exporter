// Package tvh is a read-only client for the Tvheadend HTTP API.
package tvh

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ErrUnauthorized is returned for HTTP 401/403.
var ErrUnauthorized = errors.New("tvheadend: unauthorized (check username/password and admin rights)")

// Endpoints maps logical names to API paths (relative to /api/). Never
// includes passwd/entry/grid: the exporter is read-only and must not expose
// or touch credential storage.
var Endpoints = map[string]string{
	"serverinfo":    "serverinfo",
	"subscriptions": "status/subscriptions",
	"connections":   "status/connections",
	"inputs":        "status/inputs",
	"networks":      "mpegts/network/grid",
	"muxes":         "mpegts/mux/grid",
	"services":      "mpegts/service/grid",
	"channels":      "channel/grid",
	"channeltags":   "channeltag/grid",
	"access":        "access/entry/grid",
	"dvr_entries":   "dvr/entry/grid",
	"dvr_configs":   "dvr/config/grid",
	"dvr_autorec":   "dvr/autorec/grid",
	"dvr_timerec":   "dvr/timerec/grid",
}

const gridLimit = 10000

type clientMetrics struct {
	duration *prometheus.HistogramVec
	requests *prometheus.CounterVec
}

// Client talks to one Tvheadend instance over GET requests with Basic auth
// only. It never logs credentials.
type Client struct {
	base    *url.URL
	http    *http.Client
	user    string
	pass    string
	metrics clientMetrics
	log     *slog.Logger
}

// New builds a client. reg may be nil to skip metric registration.
func New(baseURL, user, pass string, timeout time.Duration, insecure bool, reg prometheus.Registerer) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in flag
	}
	c := &Client{
		base: u,
		http: &http.Client{Timeout: timeout, Transport: tr},
		user: user, pass: pass,
		log: slog.Default(),
		metrics: clientMetrics{
			duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name: "tvheadend_exporter_api_request_duration_seconds", Help: "Tvheadend API request latency.",
				Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
			}, []string{"endpoint"}),
			requests: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "tvheadend_exporter_api_requests_total", Help: "Tvheadend API requests by endpoint and HTTP code (or 'error').",
			}, []string{"endpoint", "code"}),
		},
	}
	if reg != nil {
		reg.MustRegister(c.metrics.duration, c.metrics.requests)
	}
	return c, nil
}

// get performs GET /api/<path>?query and decodes JSON into out. Always GET,
// always Basic auth; never logs the request body or credentials.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.base.JoinPath("api", path)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.http.Do(req)
	c.metrics.duration.WithLabelValues(path).Observe(time.Since(start).Seconds())
	if err != nil {
		c.metrics.requests.WithLabelValues(path, "error").Inc()
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	c.metrics.requests.WithLabelValues(path, strconv.Itoa(resp.StatusCode)).Inc()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("GET %s: %w", path, ErrUnauthorized)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("GET %s: read: %w", path, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: decode: %w", path, err)
	}
	return nil
}

type grid[T any] struct {
	Entries    []T     `json:"entries"`
	Total      FlexInt `json:"total"`
	TotalCount FlexInt `json:"totalCount"`
}

func (g grid[T]) total() int64 {
	if g.Total > 0 {
		return g.Total.Int64()
	}
	return g.TotalCount.Int64()
}

func getGrid[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var g grid[T]
	q := url.Values{"limit": {strconv.Itoa(gridLimit)}, "start": {"0"}}
	if err := c.get(ctx, path, q, &g); err != nil {
		return nil, err
	}
	if t := g.total(); t > int64(len(g.Entries)) {
		c.log.Warn("grid truncated", "endpoint", path, "total", t, "returned", len(g.Entries))
	}
	return g.Entries, nil
}

func getList[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var g grid[T]
	if err := c.get(ctx, path, url.Values{}, &g); err != nil {
		return nil, err
	}
	return g.Entries, nil
}

func getCount(ctx context.Context, c *Client, path string) (int, error) {
	var g grid[json.RawMessage]
	if err := c.get(ctx, path, url.Values{"limit": {"1"}}, &g); err != nil {
		return 0, err
	}
	return int(g.total()), nil
}

// ServerInfo returns version and capabilities.
func (c *Client) ServerInfo(ctx context.Context) (ServerInfo, error) {
	var s ServerInfo
	err := c.get(ctx, Endpoints["serverinfo"], url.Values{}, &s)
	return s, err
}

// Subscriptions returns the current live subscriptions.
func (c *Client) Subscriptions(ctx context.Context) ([]Subscription, error) {
	return getList[Subscription](ctx, c, Endpoints["subscriptions"])
}

// Connections returns the current live HTSP/HTTP connections.
func (c *Client) Connections(ctx context.Context) ([]Connection, error) {
	return getList[Connection](ctx, c, Endpoints["connections"])
}

// Inputs returns current tuner/input status.
func (c *Client) Inputs(ctx context.Context) ([]Input, error) {
	return getList[Input](ctx, c, Endpoints["inputs"])
}

// Networks returns the mpegts network topology.
func (c *Client) Networks(ctx context.Context) ([]Network, error) {
	return getGrid[Network](ctx, c, Endpoints["networks"])
}

// Muxes returns the mpegts mux topology.
func (c *Client) Muxes(ctx context.Context) ([]Mux, error) {
	return getGrid[Mux](ctx, c, Endpoints["muxes"])
}

// Services returns the mpegts service topology.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	return getGrid[Service](ctx, c, Endpoints["services"])
}

// Channels returns configured channels.
func (c *Client) Channels(ctx context.Context) ([]Channel, error) {
	return getGrid[Channel](ctx, c, Endpoints["channels"])
}

// ChannelTags returns configured channel tags.
func (c *Client) ChannelTags(ctx context.Context) ([]ChannelTag, error) {
	return getGrid[ChannelTag](ctx, c, Endpoints["channeltags"])
}

// AccessEntries returns access-control entries (never passwd/entry/grid, no secrets).
func (c *Client) AccessEntries(ctx context.Context) ([]AccessEntry, error) {
	return getGrid[AccessEntry](ctx, c, Endpoints["access"])
}

// DVREntries returns DVR entries (titles intentionally not decoded).
func (c *Client) DVREntries(ctx context.Context) ([]DVREntry, error) {
	return getGrid[DVREntry](ctx, c, Endpoints["dvr_entries"])
}

// DVRConfigs returns DVR profile configs.
func (c *Client) DVRConfigs(ctx context.Context) ([]DVRConfig, error) {
	return getGrid[DVRConfig](ctx, c, Endpoints["dvr_configs"])
}

// DVRAutorecCount returns the number of autorec rules.
func (c *Client) DVRAutorecCount(ctx context.Context) (int, error) {
	return getCount(ctx, c, Endpoints["dvr_autorec"])
}

// DVRTimerecCount returns the number of timerec rules.
func (c *Client) DVRTimerecCount(ctx context.Context) (int, error) {
	return getCount(ctx, c, Endpoints["dvr_timerec"])
}
