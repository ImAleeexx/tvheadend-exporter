// Package sessions turns point-in-time subscription snapshots into
// start/update/end events with per-poll deltas.
package sessions

import (
	"sort"
	"strconv"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

// Kind is the event type.
type Kind int

const (
	Start Kind = iota
	Update
	End
)

func (k Kind) String() string { return [...]string{"start", "update", "end"}[k] }

// Reason explains why a session ended.
type Reason string

const (
	ReasonGone     Reason = "gone"
	ReasonRestart  Reason = "restart"
	ReasonLost     Reason = "lost"
	ReasonShutdown Reason = "shutdown"
)

// Session is the tracked state of one subscription.
type Session struct {
	ID, User, Channel, Client, Peer, Profile, Service, Title, State string
	APIStart, FirstSeen, LastSeen                                   time.Time
	TotalIn, TotalOut, Errors, BitrateIn, BitrateOut                int64
}

// Event is emitted by Apply/Fail/EndAll.
type Event struct {
	Kind         Kind
	Session      Session
	DeltaSeconds float64
	DeltaIn      int64
	DeltaOut     int64
	DeltaErrors  int64
	Reason       Reason
	Duration     time.Duration
}

// Tracker holds live sessions.
type Tracker struct {
	sessions map[string]*Session
	lastOK   time.Time
	grace    time.Duration
}

// New returns an empty tracker; grace bounds how long polls may fail before
// sessions are declared lost.
func New(grace time.Duration) *Tracker {
	return &Tracker{sessions: map[string]*Session{}, grace: grace}
}

// Len is the number of live sessions.
func (t *Tracker) Len() int { return len(t.sessions) }

// UserLabel returns the label value for a subscription's username: the
// Tvheadend API reports an empty username for anonymous access, which we
// surface as "anonymous" per the global user-label constraint.
func UserLabel(name string) string {
	if name == "" {
		return "anonymous"
	}
	return name
}

func fromSub(s tvh.Subscription, now time.Time) Session {
	return Session{
		ID: strconv.FormatInt(s.ID.Int64(), 10), User: UserLabel(s.Username.String()), Channel: s.Channel.String(),
		Client: s.Client.String(), Peer: s.Hostname.String(), Profile: s.Profile.String(),
		Service: s.Service.String(), Title: s.Title.String(), State: s.State.String(),
		APIStart: time.Unix(s.Start.Int64(), 0).UTC(), FirstSeen: now, LastSeen: now,
		TotalIn: s.TotalIn.Int64(), TotalOut: s.TotalOut.Int64(), Errors: s.Errors.Int64(),
		BitrateIn: s.In.Int64(), BitrateOut: s.Out.Int64(),
	}
}

func clamp(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// endEvent builds the End event for s. Per ruling S6, the ended session's
// LastSeen in the returned Event.Session is the time the end was observed
// (now), so a session_end log line's (end - start) agrees with Duration.
func (t *Tracker) endEvent(s *Session, now time.Time, r Reason) Event {
	start := s.APIStart
	if start.IsZero() || start.After(now) {
		start = s.FirstSeen
	}
	s2 := *s
	s2.LastSeen = now
	return Event{Kind: End, Session: s2, Reason: r, Duration: now.Sub(start)}
}

// startEvent computes deltas for a session first seen now. If it began after
// our previous successful poll (or is a restart), everything it has done so
// far is new; otherwise nothing is attributed (counters begin at exporter
// start).
func (t *Tracker) startEvent(s Session, now time.Time, since time.Time) Event {
	ev := Event{Kind: Start, Session: s}
	if since.IsZero() {
		return ev
	}
	from := s.APIStart
	if from.Before(since) {
		from = since
	}
	if from.After(now) {
		from = now
	}
	fresh := !s.APIStart.Before(since)
	if fresh {
		ev.DeltaSeconds = now.Sub(from).Seconds()
		ev.DeltaIn, ev.DeltaOut, ev.DeltaErrors = s.TotalIn, s.TotalOut, s.Errors
	}
	return ev
}

// Apply diffs a successful snapshot against tracked state.
func (t *Tracker) Apply(subs []tvh.Subscription, now time.Time) []Event {
	var evs []Event
	seen := make(map[string]bool, len(subs))
	for _, raw := range subs {
		ns := fromSub(raw, now)
		seen[ns.ID] = true
		prev, ok := t.sessions[ns.ID]
		switch {
		case !ok:
			evs = append(evs, t.startEvent(ns, now, t.lastOK))
			t.sessions[ns.ID] = &ns
		case ns.Channel != prev.Channel:
			evs = append(evs, t.endEvent(prev, now, ReasonGone))
			evs = append(evs, t.startEvent(ns, now, prev.LastSeen))
			t.sessions[ns.ID] = &ns
		case ns.TotalIn < prev.TotalIn || ns.TotalOut < prev.TotalOut:
			evs = append(evs, t.endEvent(prev, now, ReasonRestart))
			ev := Event{Kind: Start, Session: ns, DeltaIn: ns.TotalIn, DeltaOut: ns.TotalOut, DeltaErrors: ns.Errors}
			from := ns.APIStart
			if from.Before(prev.LastSeen) || from.After(now) {
				from = prev.LastSeen
			}
			ev.DeltaSeconds = now.Sub(from).Seconds()
			evs = append(evs, ev)
			t.sessions[ns.ID] = &ns
		default:
			ev := Event{
				Kind:         Update,
				DeltaSeconds: now.Sub(prev.LastSeen).Seconds(),
				DeltaIn:      clamp(ns.TotalIn - prev.TotalIn),
				DeltaOut:     clamp(ns.TotalOut - prev.TotalOut),
				DeltaErrors:  clamp(ns.Errors - prev.Errors),
			}
			if ev.DeltaSeconds < 0 {
				ev.DeltaSeconds = 0
			}
			ns.FirstSeen = prev.FirstSeen
			*prev = ns
			ev.Session = *prev
			evs = append(evs, ev)
		}
	}
	for _, id := range t.sortedIDs() {
		if !seen[id] {
			evs = append(evs, t.endEvent(t.sessions[id], now, ReasonGone))
			delete(t.sessions, id)
		}
	}
	t.lastOK = now
	return evs
}

// Fail records a failed poll; after the grace period all sessions end as
// lost.
func (t *Tracker) Fail(now time.Time) []Event {
	if !t.Lost(now) {
		return nil
	}
	return t.EndAll(now, ReasonLost)
}

// Lost reports whether more than the grace period has passed since the last
// successful poll, regardless of whether any sessions are tracked.
func (t *Tracker) Lost(now time.Time) bool {
	return !t.lastOK.IsZero() && now.Sub(t.lastOK) > t.grace
}

// EndAll ends every session with the given reason.
func (t *Tracker) EndAll(now time.Time, r Reason) []Event {
	var evs []Event
	for _, id := range t.sortedIDs() {
		evs = append(evs, t.endEvent(t.sessions[id], now, r))
		delete(t.sessions, id)
	}
	return evs
}

func (t *Tracker) sortedIDs() []string {
	ids := make([]string, 0, len(t.sessions))
	for id := range t.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
