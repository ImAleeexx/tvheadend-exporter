package poller

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type rec struct {
	mu   sync.Mutex
	errs []error
}

func (r *rec) Observe(_ string, _ time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

// TestRun_ImmediateThenPeriodic uses a generous window (ruling D14: ctx
// ~300ms, interval 30ms, assert >=3 calls) so the assertion is robust to
// scheduling jitter under -race on a loaded CI box.
func TestRun_ImmediateThenPeriodic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var calls int
	var mu sync.Mutex
	r := &rec{}
	Run(ctx, "g", 30*time.Millisecond, r, slog.Default(), func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 2 {
			return errors.New("fail")
		}
		return nil
	})
	mu.Lock()
	defer mu.Unlock()
	if calls < 3 {
		t.Errorf("want >=3 calls, got %d", calls)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.errs) != calls || r.errs[1] == nil || r.errs[0] != nil {
		t.Errorf("observer errs=%v calls=%d", r.errs, calls)
	}
}

func TestRun_TimeoutBoundsCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var deadlineOK bool
	Run(ctx, "g", 20*time.Millisecond, &rec{}, slog.Default(), func(c context.Context) error {
		_, deadlineOK = c.Deadline()
		cancel()
		return nil
	})
	if !deadlineOK {
		t.Error("fn context must carry a deadline")
	}
}
