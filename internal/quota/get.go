package quota

import (
	"context"
	"errors"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// Reader fetches quota for profiles.
type Reader struct {
	store  profile.CredStore
	client *client
	// now is injectable so tests can move time without sleeping.
	now func() time.Time
}

// NewReader builds a Reader. version goes into the User-Agent.
func NewReader(store profile.CredStore, version string) *Reader {
	return &Reader{store: store, client: newClient(version), now: time.Now}
}

// Cached returns what is on disk without any network call or secret read.
//
// This is what `acs ls` uses. It has to stay silent and instant: `ls` is the
// command people run constantly, and one that pops a Keychain dialog or waits on
// HTTP is one they stop running. No cache means "unknown", never 0%.
func Cached(profileName string, now time.Time) Result {
	e, ok := loadEntry(profileName)
	if !ok {
		return Result{
			Profile: profileName,
			State:   StateUnavailable,
			Message: "no quota cached yet -- run `acs quota`",
		}
	}
	return e.result(profileName, now)
}

// Get returns quota for one profile, fetching synchronously when the cache is
// past its TTL.
//
// Synchronous on purpose. The tempting alternative -- return the stale value and
// refresh in a goroutine -- is broken in a CLI: the process exits a few hundred
// milliseconds later, the goroutine dies before it can write, and the cache stays
// stale forever. Nothing obvious fails; `acs quota` just quietly stops updating
// until someone discovers --force. Subscribe is where stale-while-revalidate
// belongs, because the UI process actually lives long enough for it.
//
// force keeps its published meaning: ignore the cache and any backoff.
func (r *Reader) Get(ctx context.Context, p profile.Profile, force bool) Result {
	if force {
		return r.get(ctx, p, modeForce)
	}
	return r.get(ctx, p, modeCache)
}

// Refresh is what a long-lived process's schedule uses.
//
// Deliberately not what the UI buttons call. Routing a button here would make it a
// no-op exactly when it matters: the retry button only appears on an unavailable
// card, which is the state that carries Retry-After, so honouring the backoff would
// mean the click returns the same cached failure and the card redraws identically.
func (r *Reader) Refresh(ctx context.Context, p profile.Profile) Result {
	return r.get(ctx, p, modeRefresh)
}

func (r *Reader) get(ctx context.Context, p profile.Profile, m mode) Result {
	now := r.now()
	service := r.store.ServiceName(p.Literal)

	exists, err := r.store.Exists(service)
	if err != nil {
		return r.stateResult(p.Name, StateUnavailable, err.Error(), now)
	}
	if !exists {
		return r.stateResult(p.Name, StateNoLogin, "", now)
	}

	// Everything from here to the last saveEntry is one read-modify-write on this
	// profile's cache file, so it happens under that profile's lock. Unserialised,
	// the scheduled refresh and a click on Refresh both read the same entry and
	// whichever finishes last writes a whole entry rebuilt from what it read at the
	// top -- silently dropping either a reading that is already on screen or a
	// Retry-After the endpoint asked us to honour.
	//
	// The error is intentionally ignored: it means the cache directory is unusable,
	// saveEntry is about to fail on the same directory, and a caller waiting on
	// quota is better served by live numbers than by an error about a cache.
	unlock, _ := lockProfile(p.Name)
	defer unlock()

	cached, hasCache := loadEntry(p.Name)
	if hasCache && servesCache(cached, m, now) {
		return cached.result(p.Name, now)
	}

	// Reading the secret is what makes macOS prompt, so it happens only after the
	// cache has been ruled out.
	cred, err := profile.ReadCredential(r.store, p.Literal)
	if err != nil {
		return r.stateResult(p.Name, StateUnavailable, err.Error(), now)
	}
	if !cred.CanReadQuota() {
		return r.stateResult(p.Name, StateMissingScope, "", now)
	}
	if cred.Expired(now) {
		// No refresh attempt: writing a new token back could break a Claude Code
		// session that is running perfectly well.
		return r.stateResult(p.Name, StateExpired, "", now)
	}

	resp, err := r.client.fetch(ctx, cred.AccessToken)
	if err != nil {
		return r.fetchFailure(p.Name, err, cached, hasCache, now, m)
	}

	res := mapResponse(resp, now)
	res.Profile = p.Name
	// A successful call clears the backoff ladder.
	_ = saveEntry(p.Name, entryFrom(res))
	return res
}

// fetchFailure decides what to show when the endpoint does not answer usefully.
//
// The previous numbers are worth keeping -- knowing the weekly window was at 91%
// five minutes ago is useful -- but they go into LastKnown, never into the live
// windows. The windows staying nil is what makes "draw a bar" reduce to
// `State == StateOK` for every consumer.
func (r *Reader) fetchFailure(profileName string, err error, cached entry, hasCache bool, now time.Time, m mode) Result {
	if errors.Is(err, errUnauthorized) {
		// The credential was rejected outright. The fix is the same as an expiry:
		// authenticate again.
		return r.stateResult(profileName, StateExpired, "", now)
	}

	res := Result{Profile: profileName, State: StateUnavailable}
	detail := err.Error()

	var throttled throttleError
	if errors.As(err, &throttled) {
		// A human override does not lengthen the schedule's penalty. Someone clicking
		// refresh on a throttled card is allowed through the backoff, so letting each
		// click climb the ladder would mean their impatience stalls the background
		// loop for up to half an hour -- the opposite of what they asked for.
		step := cached.BackoffStep
		if m != modeForce {
			step++
		}
		delay := throttled.retryAfter
		if delay <= 0 {
			delay = backoffDelay(step)
		}
		retryAt := now.Add(delay)
		res.RetryAfter = &retryAt
		res.Message = Message(StateUnavailable, profileName, detail) +
			" -- try again at " + retryAt.Format(time.TimeOnly)

		next := cached
		next.State = StateUnavailable
		next.Message = res.Message
		next.RetryAfter = &retryAt
		next.BackoffStep = step
		if !hasCache {
			next.FetchedAt = now
		}
		_ = saveEntry(profileName, next)
	} else {
		res.Message = Message(StateUnavailable, profileName, detail)
	}

	// Keep whatever numbers we had, under a name that cannot read as current.
	if hasCache {
		res.LastKnown = snapshotOf(cached)
	}
	return res
}

// stateResult builds a result for a state that carries no numbers.
//
// FiveHour and SevenDay stay nil, which is the invariant this whole package is
// built around: a caller cannot accidentally render 0% from a nil pointer.
func (r *Reader) stateResult(profileName string, state State, detail string, now time.Time) Result {
	return Result{
		Profile:   profileName,
		State:     state,
		Message:   Message(state, profileName, detail),
		FetchedAt: &now,
	}
}

// GetAll returns quota for every profile.
//
// Profiles are fetched sequentially: two accounts means two requests, and there is
// no reason to look like a burst to an endpoint that rate limits.
func (r *Reader) GetAll(ctx context.Context, profiles []profile.Profile, force bool) []Result {
	out := make([]Result, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, r.Get(ctx, p, force))
	}
	return out
}
