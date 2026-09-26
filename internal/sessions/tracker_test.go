package sessions

import (
	"testing"
	"time"

	"github.com/imaleeexx/tvheadend-exporter/internal/tvh"
)

var t0 = time.Date(2026, 9, 26, 20, 0, 0, 0, time.UTC)

func sub(id int64, user, channel string, start time.Time, totalOut, errs int64) tvh.Subscription {
	return tvh.Subscription{
		ID: tvh.FlexInt(id), Start: tvh.FlexInt(start.Unix()), Errors: tvh.FlexInt(errs),
		State: "Running", Hostname: "203.0.113.10", Username: tvh.FlexString(user), Client: "Kodi",
		Channel: tvh.FlexString(channel), Profile: "htsp", In: 900, Out: 900,
		TotalIn: tvh.FlexInt(totalOut), TotalOut: tvh.FlexInt(totalOut),
	}
}

func kinds(evs []Event) []Kind {
	out := make([]Kind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestApply_NewSessionEmitsStart(t *testing.T) {
	tr := New(time.Minute)
	evs := tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0.Add(-time.Hour), 1000, 0)}, t0)
	if len(evs) != 1 || evs[0].Kind != Start {
		t.Fatalf("want [Start], got %v", kinds(evs))
	}
	s := evs[0].Session
	if s.ID != "1" || s.User != "alice" || s.Peer != "203.0.113.10" || !s.APIStart.Equal(t0.Add(-time.Hour)) {
		t.Errorf("bad session: %+v", s)
	}
	if evs[0].DeltaSeconds != 0 || evs[0].DeltaOut != 0 {
		t.Errorf("pre-existing session on first poll must not add deltas: %+v", evs[0])
	}
	if tr.Len() != 1 {
		t.Errorf("Len=%d", tr.Len())
	}
}

func TestApply_AnonymousUser(t *testing.T) {
	tr := New(time.Minute)
	evs := tr.Apply([]tvh.Subscription{sub(1, "", "La 1", t0, 0, 0)}, t0)
	if evs[0].Session.User != "anonymous" {
		t.Errorf("want anonymous, got %q", evs[0].Session.User)
	}
}

func TestApply_SecondPollEmitsUpdateWithDeltas(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0.Add(-time.Hour), 1000, 2)}, t0)
	evs := tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0.Add(-time.Hour), 1500, 5)}, t0.Add(10*time.Second))
	if len(evs) != 1 || evs[0].Kind != Update {
		t.Fatalf("want [Update], got %v", kinds(evs))
	}
	e := evs[0]
	if e.DeltaSeconds != 10 || e.DeltaOut != 500 || e.DeltaIn != 500 || e.DeltaErrors != 3 {
		t.Errorf("bad deltas: %+v", e)
	}
}

func TestApply_NegativeErrorDeltaClamped(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0, 1000, 5)}, t0)
	evs := tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0, 1100, 1)}, t0.Add(10*time.Second))
	if evs[0].Kind != Update || evs[0].DeltaErrors != 0 {
		t.Errorf("want Update with DeltaErrors=0, got %+v", evs[0])
	}
}

func TestApply_GoneEmitsEndWithDuration(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0.Add(-time.Hour), 1000, 0)}, t0)
	evs := tr.Apply(nil, t0.Add(10*time.Second))
	if len(evs) != 1 || evs[0].Kind != End || evs[0].Reason != ReasonGone {
		t.Fatalf("want [End gone], got %+v", evs)
	}
	if evs[0].Duration != time.Hour+10*time.Second {
		t.Errorf("duration=%v", evs[0].Duration)
	}
	if tr.Len() != 0 {
		t.Errorf("Len=%d", tr.Len())
	}
}

func TestApply_FreshSessionCountsBytesSinceStart(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply(nil, t0)
	evs := tr.Apply([]tvh.Subscription{sub(2, "bob", "XTRM", t0.Add(4*time.Second), 800, 1)}, t0.Add(10*time.Second))
	if evs[0].Kind != Start || evs[0].DeltaOut != 800 || evs[0].DeltaIn != 800 || evs[0].DeltaErrors != 1 || evs[0].DeltaSeconds != 6 {
		t.Errorf("fresh session should count everything since its start: %+v", evs[0])
	}
}

func TestApply_ByteCounterDecreaseIsRestart(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0.Add(-time.Hour), 5000, 0)}, t0)
	evs := tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0.Add(5*time.Second), 300, 0)}, t0.Add(10*time.Second))
	if len(evs) != 2 || evs[0].Kind != End || evs[0].Reason != ReasonRestart || evs[1].Kind != Start {
		t.Fatalf("want [End restart, Start], got %+v", evs)
	}
	if evs[1].DeltaOut != 300 || evs[1].DeltaSeconds != 5 {
		t.Errorf("restart deltas: %+v", evs[1])
	}
}

func TestApply_ChannelChangeSameID(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0, 100, 0)}, t0)
	evs := tr.Apply([]tvh.Subscription{sub(1, "alice", "La 2", t0.Add(3*time.Second), 200, 0)}, t0.Add(10*time.Second))
	if len(evs) != 2 || evs[0].Kind != End || evs[0].Reason != ReasonGone || evs[1].Kind != Start || evs[1].Session.Channel != "La 2" {
		t.Fatalf("want [End gone, Start La 2], got %+v", evs)
	}
}

func TestApplyFailure_GraceEndsSessions(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0, 100, 0)}, t0)
	if evs := tr.Fail(t0.Add(30 * time.Second)); len(evs) != 0 {
		t.Fatalf("inside grace: want no events, got %+v", evs)
	}
	evs := tr.Fail(t0.Add(61 * time.Second))
	if len(evs) != 1 || evs[0].Kind != End || evs[0].Reason != ReasonLost {
		t.Fatalf("want [End lost], got %+v", evs)
	}
	if evs := tr.Fail(t0.Add(2 * time.Minute)); len(evs) != 0 {
		t.Errorf("already ended: got %+v", evs)
	}
}

func TestEndAll_Shutdown(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "a", "x", t0, 1, 0), sub(2, "b", "y", t0, 1, 0)}, t0)
	evs := tr.EndAll(t0.Add(time.Second), ReasonShutdown)
	if len(evs) != 2 || evs[0].Reason != ReasonShutdown || tr.Len() != 0 {
		t.Fatalf("got %+v len=%d", evs, tr.Len())
	}
}

// --- Ruling D4: UserLabel is exported and used by fromSub. ---

func TestUserLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "anonymous"},
		{"alice", "alice"},
	}
	for _, c := range cases {
		if got := UserLabel(c.in); got != c.want {
			t.Errorf("UserLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- Ruling S6: the ended session's LastSeen is the time the end was
// observed, so a later session_end log's (end - start) agrees with Duration.

func TestApply_GoneEnd_LastSeenIsNow(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0.Add(-time.Hour), 1000, 0)}, t0)
	end := t0.Add(10 * time.Second)
	evs := tr.Apply(nil, end)
	if len(evs) != 1 || evs[0].Kind != End {
		t.Fatalf("want [End], got %+v", evs)
	}
	if !evs[0].Session.LastSeen.Equal(end) {
		t.Errorf("LastSeen = %v, want %v", evs[0].Session.LastSeen, end)
	}
}

func TestFail_LostEnd_LastSeenIsNow(t *testing.T) {
	tr := New(time.Minute)
	tr.Apply([]tvh.Subscription{sub(1, "alice", "La 1", t0, 100, 0)}, t0)
	lostAt := t0.Add(61 * time.Second)
	evs := tr.Fail(lostAt)
	if len(evs) != 1 || evs[0].Kind != End || evs[0].Reason != ReasonLost {
		t.Fatalf("want [End lost], got %+v", evs)
	}
	if !evs[0].Session.LastSeen.Equal(lostAt) {
		t.Errorf("LastSeen = %v, want %v", evs[0].Session.LastSeen, lostAt)
	}
}

func TestLost_GraceExceededEvenWithoutSessions(t *testing.T) {
	tr := New(time.Minute)
	if tr.Lost(t0) {
		t.Fatal("never polled: must not be lost")
	}
	tr.Apply(nil, t0)
	if tr.Lost(t0.Add(time.Minute)) {
		t.Error("at grace boundary: must not be lost")
	}
	if !tr.Lost(t0.Add(61 * time.Second)) {
		t.Error("beyond grace with no sessions: must be lost")
	}
	tr.Apply(nil, t0.Add(2*time.Minute))
	if tr.Lost(t0.Add(2*time.Minute + time.Second)) {
		t.Error("after a successful poll: must not be lost")
	}
}
