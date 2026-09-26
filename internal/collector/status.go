package collector

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/geoip"
	"github.com/imaleeexx/tvheadend-exporter/internal/sessions"
	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
	"github.com/prometheus/client_golang/prometheus"
)

var sessionLabels = []string{"user", "channel", "client", "peer", "profile", "country", "city"}

func withID(l []string) []string { return append([]string{"id"}, l...) }

const typeUnknown = "unknown"

// Status exports per-user/per-stream metrics driven by the session tracker.
type Status struct {
	tracker *sessions.Tracker
	geo     geoip.Resolver
	log     *slog.Logger

	subInfo, subStart, subBitrateIn, subBitrateOut, subBytesIn, subBytesOut, subErrors *prometheus.GaugeVec

	subsActive, userActive, channelViewers, connsActive, connInfo, userConnLimit, accessEntries *labelSet

	sessionSeconds, sessionBytesOut, sessionBytesIn, sessionsStarted, sessionsEnded, sessionErrors *prometheus.CounterVec
	sessionDuration                                                                            *prometheus.HistogramVec
	sessionsTracked                                                                            prometheus.Gauge

	// types joins subscriptions to the current poll's connections:
	// "user\x00peer" -> htsp|http|unknown.
	types map[string]string
	// sessTypes remembers the type last observed per live session id so a
	// session_end line keeps it after the connection has vanished.
	sessTypes map[string]string
}

// NewStatus registers all status metrics on reg.
func NewStatus(reg prometheus.Registerer, tr *sessions.Tracker, geo geoip.Resolver, log *slog.Logger) *Status {
	g := func(name, help string, labels []string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tvheadend_" + name, Help: help}, labels)
	}
	c := func(name, help string) *prometheus.CounterVec {
		return prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tvheadend_" + name, Help: help}, sessionLabels)
	}
	s := &Status{tracker: tr, geo: geo, log: log, types: map[string]string{}, sessTypes: map[string]string{},
		subInfo:       g("subscription_info", "Live subscription; value is always 1.", append(withID(sessionLabels), "service", "state", "title")),
		subStart:      g("subscription_start_timestamp_seconds", "Subscription start time (unix).", withID(sessionLabels)),
		subBitrateIn:  g("subscription_bitrate_in_bps", "Current input bitrate of the subscription.", withID(sessionLabels)),
		subBitrateOut: g("subscription_bitrate_out_bps", "Current output bitrate of the subscription.", withID(sessionLabels)),
		subBytesIn:    g("subscription_bytes_in", "Bytes received for this subscription so far.", withID(sessionLabels)),
		subBytesOut:   g("subscription_bytes_out", "Bytes sent for this subscription so far.", withID(sessionLabels)),
		subErrors:     g("subscription_errors", "Errors counted by Tvheadend for this subscription.", withID(sessionLabels)),

		subsActive:     newLabelSet(g("subscriptions_active", "Active subscriptions.", []string{"profile", "type"})),
		userActive:     newLabelSet(g("user_active_streams", "Active streams per user.", []string{"user"})),
		channelViewers: newLabelSet(g("channel_active_viewers", "Active viewers per channel.", []string{"channel"})),
		connsActive:    newLabelSet(g("connections_active", "Active client connections.", []string{"type", "user", "streaming"})),
		connInfo:       newLabelSet(g("connection_info", "Client connection; value is the start time (unix).", []string{"id", "type", "user", "peer", "server_port"})),
		userConnLimit:  newLabelSet(g("user_conn_limit", "Configured connection limit per user (0 = unlimited).", []string{"user"})),
		accessEntries:  newLabelSet(g("access_entries", "Access entries by enabled state.", []string{"enabled"})),

		sessionSeconds:  c("session_seconds_total", "Watch time accumulated by sessions."),
		sessionBytesOut: c("session_bytes_out_total", "Bytes sent to clients by sessions."),
		sessionBytesIn:  c("session_bytes_in_total", "Bytes received from sources for sessions."),
		sessionsStarted: c("sessions_started_total", "Sessions started."),
		sessionsEnded:   c("sessions_ended_total", "Sessions ended."),
		sessionErrors:   c("session_errors_total", "Errors accumulated by sessions."),
		sessionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "tvheadend_session_duration_seconds", Help: "Duration of ended sessions.",
			Buckets: []float64{30, 60, 300, 900, 1800, 3600, 7200, 14400, 28800},
		}, []string{"user", "client"}),
		sessionsTracked: prometheus.NewGauge(prometheus.GaugeOpts{Name: "tvheadend_exporter_sessions_tracked", Help: "Sessions currently tracked by the exporter."}),
	}
	reg.MustRegister(s.subInfo, s.subStart, s.subBitrateIn, s.subBitrateOut, s.subBytesIn, s.subBytesOut, s.subErrors,
		s.subsActive.vec, s.userActive.vec, s.channelViewers.vec, s.connsActive.vec, s.connInfo.vec, s.userConnLimit.vec, s.accessEntries.vec,
		s.sessionSeconds, s.sessionBytesOut, s.sessionBytesIn, s.sessionsStarted, s.sessionsEnded, s.sessionErrors, s.sessionDuration, s.sessionsTracked)
	return s
}

func (s *Status) labels(sess sessions.Session) []string {
	loc := s.geo.Lookup(sess.Peer)
	return []string{sess.User, sess.Channel, sess.Client, sess.Peer, sess.Profile, loc.Country, loc.City}
}

// sanitizeLabel strips secrets from free-text API values (service, title):
// every URL is reduced to scheme://host (userinfo, path and query dropped;
// a URL ends at whitespace, since IPTV mux URLs sit inside "net/mux/service"
// names and their path cannot be told apart from what follows), and DVR
// subscription titles ("DVR: <programme>") collapse to "DVR".
func sanitizeLabel(v string) string {
	if strings.HasPrefix(v, "DVR:") {
		return "DVR"
	}
	var b strings.Builder
	for {
		i := strings.Index(v, "://")
		if i < 0 {
			b.WriteString(v)
			return b.String()
		}
		b.WriteString(v[:i+3])
		rest := v[i+3:]
		end := strings.IndexAny(rest, " \t\r\n")
		if end < 0 {
			end = len(rest)
		}
		host := rest[:end]
		if j := strings.IndexAny(host, "/?#"); j >= 0 {
			host = host[:j]
		}
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
		b.WriteString(host)
		v = rest[end:]
	}
}

// subType maps a connection type onto the subscriptions_active.type domain.
func subType(connType string) string {
	switch t := strings.ToLower(connType); t {
	case "htsp", "http":
		return t
	default:
		return typeUnknown
	}
}

// typeOf joins a session to the current poll's connections on (user, peer).
func (s *Status) typeOf(sess sessions.Session) string {
	if t, ok := s.types[sess.User+"\x00"+sess.Peer]; ok {
		return t
	}
	return typeUnknown
}

// Update applies one successful status poll.
func (s *Status) Update(subs []tvh.Subscription, conns []tvh.Connection, now time.Time) {
	s.types = make(map[string]string, len(conns))
	s.connsActive.begin()
	s.connInfo.begin()
	for _, c := range conns {
		user := sessions.UserLabel(c.User.String())
		typ := strings.ToLower(c.Type.String())
		s.types[user+"\x00"+c.Peer.String()] = subType(typ)
		s.connsActive.add(1, typ, user, strconv.FormatBool(c.Streaming != 0))
		s.connInfo.set(float64(c.Started), strconv.FormatInt(c.ID.Int64(), 10), typ, user, c.Peer.String(), strconv.FormatInt(c.ServerPort.Int64(), 10))
	}
	s.connsActive.end()
	s.connInfo.end()

	s.handle(s.tracker.Apply(subs, now), true)

	s.subsActive.begin()
	s.userActive.begin()
	s.channelViewers.begin()
	for _, raw := range subs {
		sess := sessionFrom(raw)
		s.subsActive.add(1, sess.Profile, s.typeOf(sess))
		s.userActive.add(1, sess.User)
		s.channelViewers.add(1, sess.Channel)
	}
	s.subsActive.end()
	s.userActive.end()
	s.channelViewers.end()
	s.sessionsTracked.Set(float64(s.tracker.Len()))
}

// sessionFrom extracts just the labels needed for aggregates.
func sessionFrom(raw tvh.Subscription) sessions.Session {
	return sessions.Session{
		User: sessions.UserLabel(raw.Username.String()), Channel: raw.Channel.String(),
		Peer: raw.Hostname.String(), Profile: raw.Profile.String(),
	}
}

// Fail records a failed status poll. Once the tracker declares sessions lost,
// every series derived from the last good snapshot is dropped too — even
// when that snapshot held connections but no subscriptions.
func (s *Status) Fail(now time.Time) {
	s.handle(s.tracker.Fail(now), true)
	if s.tracker.Lost(now) {
		s.clearSnapshot()
	}
	s.sessionsTracked.Set(float64(s.tracker.Len()))
}

// clearSnapshot deletes the aggregate and connection series of the status poll.
func (s *Status) clearSnapshot() {
	for _, l := range []*labelSet{s.subsActive, s.userActive, s.channelViewers, s.connsActive, s.connInfo} {
		l.begin()
		l.end()
	}
	s.types = map[string]string{}
}

// Shutdown ends all sessions without recording their durations.
func (s *Status) Shutdown(now time.Time) {
	s.handle(s.tracker.EndAll(now, sessions.ReasonShutdown), false)
	s.sessionsTracked.Set(0)
}

func (s *Status) handle(evs []sessions.Event, observe bool) {
	for _, ev := range evs {
		lv := s.labels(ev.Session)
		switch ev.Kind {
		case sessions.Start:
			typ := s.typeOf(ev.Session)
			s.sessTypes[ev.Session.ID] = typ
			s.sessionsStarted.WithLabelValues(lv...).Inc()
			s.addDeltas(ev, lv)
			s.setGauges(ev.Session, lv)
			s.logEvent("session_start", typ, ev, lv)
		case sessions.Update:
			s.sessTypes[ev.Session.ID] = s.typeOf(ev.Session)
			s.addDeltas(ev, lv)
			s.setGauges(ev.Session, lv)
		case sessions.End:
			typ, ok := s.sessTypes[ev.Session.ID]
			if !ok {
				typ = typeUnknown
			}
			delete(s.sessTypes, ev.Session.ID)
			s.deleteGauges(ev.Session.ID)
			s.sessionsEnded.WithLabelValues(lv...).Inc()
			if observe {
				s.sessionDuration.WithLabelValues(ev.Session.User, ev.Session.Client).Observe(ev.Duration.Seconds())
			}
			s.logEvent("session_end", typ, ev, lv)
		}
	}
}

func (s *Status) addDeltas(ev sessions.Event, lv []string) {
	s.sessionSeconds.WithLabelValues(lv...).Add(ev.DeltaSeconds)
	s.sessionBytesIn.WithLabelValues(lv...).Add(float64(ev.DeltaIn))
	s.sessionBytesOut.WithLabelValues(lv...).Add(float64(ev.DeltaOut))
	s.sessionErrors.WithLabelValues(lv...).Add(float64(ev.DeltaErrors))
}

func (s *Status) setGauges(sess sessions.Session, lv []string) {
	idlv := append([]string{sess.ID}, lv...)
	info := append(append(make([]string, 0, len(idlv)+3), idlv...), sanitizeLabel(sess.Service), sess.State, sanitizeLabel(sess.Title))
	s.subInfo.WithLabelValues(info...).Set(1)
	s.subStart.WithLabelValues(idlv...).Set(float64(sess.APIStart.Unix()))
	s.subBitrateIn.WithLabelValues(idlv...).Set(float64(sess.BitrateIn))
	s.subBitrateOut.WithLabelValues(idlv...).Set(float64(sess.BitrateOut))
	s.subBytesIn.WithLabelValues(idlv...).Set(float64(sess.TotalIn))
	s.subBytesOut.WithLabelValues(idlv...).Set(float64(sess.TotalOut))
	s.subErrors.WithLabelValues(idlv...).Set(float64(sess.Errors))
}

func (s *Status) deleteGauges(id string) {
	m := prometheus.Labels{"id": id}
	for _, v := range []*prometheus.GaugeVec{s.subInfo, s.subStart, s.subBitrateIn, s.subBitrateOut, s.subBytesIn, s.subBytesOut, s.subErrors} {
		v.DeletePartialMatch(m)
	}
}

func (s *Status) logEvent(event, typ string, ev sessions.Event, lv []string) {
	attrs := []any{
		"event", event, "user", lv[0], "channel", lv[1], "client", lv[2], "peer", lv[3],
		"profile", lv[4], "country", lv[5], "city", lv[6], "type", typ,
		"start", ev.Session.APIStart.UTC().Format(time.RFC3339),
	}
	if ev.Kind == sessions.End {
		attrs = append(attrs,
			"end", ev.Session.LastSeen.UTC().Format(time.RFC3339),
			"duration_s", int64(ev.Duration/time.Second),
			"bytes_out", ev.Session.TotalOut, "bytes_in", ev.Session.TotalIn,
			"errors", ev.Session.Errors, "reason", string(ev.Reason))
		s.log.Info(event, attrs...)
		return
	}
	s.log.Debug(event, attrs...)
}

// UpdateAccess exports per-user connection limits (from the access grid).
func (s *Status) UpdateAccess(entries []tvh.AccessEntry) {
	s.userConnLimit.begin()
	s.accessEntries.begin()
	s.accessEntries.add(0, "true")
	s.accessEntries.add(0, "false")
	for _, e := range entries {
		s.accessEntries.add(1, strconv.FormatBool(e.Enabled.Bool()))
		if e.Enabled.Bool() && e.Username != "" && e.Username != "*" {
			s.userConnLimit.set(float64(e.ConnLimit), e.Username)
		}
	}
	s.userConnLimit.end()
	s.accessEntries.end()
}
