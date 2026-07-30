package quota

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

// TTL is how long a fetched result stays fresh. Five minutes keeps `acs quota`
// responsive while staying far away from the endpoint's rate limits.
const TTL = 5 * time.Minute

// maxBackoff caps the retry ladder. Beyond half an hour the numbers are stale
// enough that waiting longer buys nothing.
const maxBackoff = 30 * time.Minute

// entry is the cache file's contents.
//
// It holds derived numbers only -- never the token. The token lives in memory for
// the duration of one fetch. scripts/check-no-token-in-cache.sh enforces this from
// the outside, because a leak here would be silent.
type entry struct {
	SchemaVersion int        `json:"schemaVersion"`
	FetchedAt     time.Time  `json:"fetchedAt"`
	State         State      `json:"state"`
	Message       string     `json:"message"`
	FiveHour      *Window    `json:"fiveHour"`
	SevenDay      *Window    `json:"sevenDay"`
	Limits        []Limit    `json:"limits,omitempty"`
	RetryAfter    *time.Time `json:"retryAfter,omitempty"`
	// BackoffStep persists so the ladder keeps climbing across CLI invocations.
	// Held in memory only, every short-lived `acs quota` would restart at step one
	// and hammer an endpoint that just asked us to slow down.
	BackoffStep int `json:"backoffStep,omitempty"`
}

// cacheDir is where per-profile cache files live.
//
// One file per profile means concurrent writes touch different files, so unlike
// config.json this needs no lock: the worst case is one profile's numbers being
// rewritten twice.
func cacheDir() string { return filepath.Join(config.Home(), "cache") }

func cachePath(profileName string) string {
	return filepath.Join(cacheDir(), "quota-"+profileName+".json")
}

// loadEntry reads a cached result. A missing or unreadable cache is not an error:
// it just means nothing is known yet.
func loadEntry(profileName string) (entry, bool) {
	raw, err := os.ReadFile(cachePath(profileName)) // #nosec G304 -- path built from Home()
	if err != nil {
		return entry{}, false
	}
	var e entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return entry{}, false
	}
	// A cache written by a newer acs may mean something different by the same
	// fields. Discard rather than misread it; it costs one fetch.
	if e.SchemaVersion != SchemaVersion {
		return entry{}, false
	}
	return e, true
}

// saveEntry writes the cache atomically.
func saveEntry(profileName string, e entry) error {
	e.SchemaVersion = SchemaVersion
	if err := os.MkdirAll(cacheDir(), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", cacheDir(), err)
	}
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}

	tmp, err := os.CreateTemp(cacheDir(), "quota-*.json")
	if err != nil {
		return fmt.Errorf("create temp cache: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp cache: %w", err)
	}
	return os.Rename(name, cachePath(profileName))
}

// fresh reports whether an entry is still within its TTL.
func (e entry) fresh(now time.Time) bool {
	return e.State == StateOK && now.Sub(e.FetchedAt) < TTL
}

// backingOff reports whether the endpoint asked us to wait.
func (e entry) backingOff(now time.Time) bool {
	return e.RetryAfter != nil && now.Before(*e.RetryAfter)
}

// result converts a cache entry into a Result, upholding the contract on Result:
// the windows carry numbers only for an ok state, and anything older surfaces as
// LastKnown where it cannot be mistaken for current.
func (e entry) result(profileName string, now time.Time) Result {
	fetchedAt := e.FetchedAt
	res := Result{
		Profile:    profileName,
		State:      e.State,
		Message:    e.Message,
		FetchedAt:  &fetchedAt,
		RetryAfter: e.RetryAfter,
	}
	if e.State == StateOK {
		// Past its TTL this is still the answer -- `acs ls` exists to show it -- but
		// it must be labelled rather than presented as fresh.
		res.Stale = !e.fresh(now)
		res.FiveHour = e.FiveHour
		res.SevenDay = e.SevenDay
		res.Limits = e.Limits
		return res
	}
	res.LastKnown = snapshotOf(e)
	return res
}

// snapshotOf lifts a cache entry's stored numbers into a LastKnown snapshot, or nil
// when acs has never had a successful reading for that profile.
func snapshotOf(e entry) *Snapshot {
	if e.FiveHour == nil && e.SevenDay == nil {
		return nil
	}
	return &Snapshot{FetchedAt: e.FetchedAt, FiveHour: e.FiveHour, SevenDay: e.SevenDay}
}

// entryFrom converts a fresh Result into a cache entry.
func entryFrom(r Result) entry {
	e := entry{
		State:    r.State,
		Message:  r.Message,
		FiveHour: r.FiveHour,
		SevenDay: r.SevenDay,
		Limits:   r.Limits,
	}
	if r.FetchedAt != nil {
		e.FetchedAt = *r.FetchedAt
	}
	return e
}

// backoffDelay is the ladder: 1, 2, 4, 8, 16 minutes, then capped.
func backoffDelay(step int) time.Duration {
	if step < 1 {
		step = 1
	}
	delay := time.Minute << (step - 1)
	if delay > maxBackoff || delay <= 0 {
		return maxBackoff
	}
	return delay
}

// ClearCache removes all cached numbers. Safe at any time: everything in there is
// derived and can be fetched again.
func ClearCache() error {
	err := os.RemoveAll(cacheDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear %s: %w", cacheDir(), err)
	}
	return nil
}
