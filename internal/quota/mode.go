package quota

import "time"

// mode says which of the two guards on a read may be skipped.
//
// The guards used to be one boolean, which conflated two unrelated jobs: the TTL
// stops a short-lived CLI from re-fetching on every invocation, while the backoff
// honours what the endpoint asked for. A caller that paces itself needs the second
// but not the first, and there was no way to say so -- so the ticker in Subscribe
// waited out the interval and then the TTL, making the numbers up to twice the
// interval old.
type mode int

const (
	// modeCache honours both guards. The short-lived CLI default.
	modeCache mode = iota
	// modeRefresh is for a caller whose own schedule sets the cadence: the TTL is
	// redundant there, but Retry-After still wins.
	modeRefresh
	// modeForce skips both. Reserved for an explicit human override -- the
	// documented meaning of `acs quota --force`, and what the buttons on a card do,
	// since someone looking at a stalled card and clicking retry means it.
	modeForce
)

// refreshFloor is the shortest gap between two scheduled network passes.
//
// It is not a second TTL. The dashboard restarts Subscribe on every mount, and
// modeRefresh ignores the TTL, so without a floor a user flicking between views
// issues a full pass per flick against an endpoint that rate limits. The floor sits
// far below any real interval, so it never delays a scheduled refresh -- it only
// collapses churn.
const refreshFloor = 30 * time.Second

// servesCache reports whether the cached entry answers this read.
//
// One switch, so that "which guard applies to which caller" has exactly one
// definition rather than being spread across call sites.
func servesCache(cached entry, m mode, now time.Time) bool {
	switch m {
	case modeRefresh:
		// fresh() is deliberately absent: the caller's schedule already paces this.
		return cached.backingOff(now) || withinFloor(cached.FetchedAt, now)
	case modeForce:
		return false
	default: // modeCache, and anything added later
		// Failing closed: a new mode that nobody taught this switch about honours
		// both guards rather than silently gaining the right to ignore Retry-After.
		return cached.fresh(now) || cached.backingOff(now)
	}
}

// withinFloor reports whether a reading is too recent to replace.
//
// The distance is absolute so a timestamp in the future -- a backwards clock step, or
// a cache file carried between machines -- reads as "just taken" for 30 seconds
// rather than disabling the scheduled refresh forever.
func withinFloor(fetchedAt, now time.Time) bool {
	return now.Sub(fetchedAt).Abs() < refreshFloor
}
