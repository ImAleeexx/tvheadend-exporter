// Package poller runs a function on a fixed interval with timeout and reporting.
package poller

import (
	"context"
	"log/slog"
	"time"
)

// Observer receives the outcome of every cycle.
type Observer interface {
	Observe(group string, d time.Duration, err error)
}

// Run calls fn now and then every interval until ctx ends. Each call is bounded
// by a deadline of one interval so a hung request never overlaps the next cycle.
func Run(ctx context.Context, group string, every time.Duration, obs Observer, log *slog.Logger, fn func(ctx context.Context) error) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		start := time.Now()
		cctx, cancel := context.WithTimeout(ctx, every)
		err := fn(cctx)
		cancel()
		obs.Observe(group, time.Since(start), err)
		if err != nil && ctx.Err() == nil {
			log.Warn("poll failed", "group", group, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
