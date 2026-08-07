package quota

import (
	"context"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// Subscribe streams results for a long-lived caller: cached values first, then
// refreshed ones, then a refresh every interval.
//
// This is the real stale-while-revalidate, and it is safe here precisely because
// the UI process outlives the fetch. Do not reach for it from the CLI.
//
// The schedule owns the cadence here, which is why the passes call Refresh rather
// than Get: the TTL exists to stop a short-lived CLI from re-fetching on every
// invocation, and applying it again inside a ticker that already paces the requests
// just means the interval and the TTL both have to elapse. That is what made the
// numbers up to twice the interval old.
func (r *Reader) Subscribe(ctx context.Context, profiles []profile.Profile, interval time.Duration) <-chan Result {
	ch := make(chan Result)
	go func() {
		defer close(ch)
		for _, p := range profiles {
			if !send(ctx, ch, Cached(p.Name, r.now())) {
				return
			}
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			// This pass runs before the first tick, so a freshly mounted dashboard
			// shows real numbers instead of waiting a whole interval for them.
			// Moving it below the select would silently undo that.
			for _, p := range profiles {
				if !send(ctx, ch, r.Refresh(ctx, p)) {
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return ch
}

func send(ctx context.Context, ch chan<- Result, res Result) bool {
	select {
	case ch <- res:
		return true
	case <-ctx.Done():
		return false
	}
}
