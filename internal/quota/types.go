package quota

import "time"

// SchemaVersion is the version of the JSON that `acs quota --json` prints and the
// cache stores. It is a contract: user scripts and the desktop UI both read it, so
// changing the shape without bumping this breaks them silently.
const SchemaVersion = 1

// Severity is how alarming a window's usage is.
type Severity string

const (
	// SeverityNeutral is normal usage.
	SeverityNeutral Severity = "neutral"
	// SeverityAttention means close to the limit.
	SeverityAttention Severity = "attention"
)

// AttentionThreshold is the local fallback, used only when the API sends no
// severity of its own. Anthropic knows their real thresholds; acs does not.
const AttentionThreshold = 80

// Window is one usage window, for example the rolling five hours.
type Window struct {
	Utilization int        `json:"utilization"`
	ResetsAt    *time.Time `json:"resetsAt,omitempty"`
	Severity    Severity   `json:"severity,omitempty"`
	// LimitDollars is present for extra-usage style limits and absent otherwise.
	LimitDollars *float64 `json:"limitDollars,omitempty"`
}

// Limit is an entry from the API's limits array, kept as-is so a window Anthropic
// adds later still reaches the caller.
type Limit struct {
	Kind     string     `json:"kind"`
	Group    string     `json:"group"`
	Percent  int        `json:"percent"`
	Severity Severity   `json:"severity,omitempty"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

// Snapshot is the last reading that succeeded, carried alongside a failed result.
//
// It is a separate key from Result.FiveHour on purpose. The plan asked for two
// things that cannot both be true of one field -- windows must be null when the
// state is not ok, and a failed refresh must still show the previous numbers
// labelled stale -- so the previous numbers get their own name. A consumer cannot
// render these as current by accident, because reaching them means asking for
// "last known".
type Snapshot struct {
	FetchedAt time.Time `json:"fetchedAt"`
	FiveHour  *Window   `json:"fiveHour"`
	SevenDay  *Window   `json:"sevenDay"`
}

// Result is what acs knows about one profile's quota.
//
// Two invariants hold, and together they are the whole contract:
//
//	FiveHour/SevenDay are non-nil  <=>  State == StateOK
//	LastKnown is non-nil            =>  State != StateOK, and acs succeeded before
//
// So "draw a bar" is exactly `State == StateOK`, and nothing in LastKnown can ever
// be mistaken for a current reading. When State is ok and Stale is also true, the
// numbers came from the cache and FetchedAt says how old they are.
//
// The windows are pointers so that "unknown" is representable at all. A zero-valued
// Window means 0% used, which is the one lie this package exists to prevent.
type Result struct {
	Profile string `json:"name"`
	State   State  `json:"state"`
	// Message is always set, including when State is StateOK.
	Message string `json:"message"`
	// Stale marks an ok reading served from cache past its TTL.
	Stale     bool       `json:"stale"`
	FetchedAt *time.Time `json:"fetchedAt,omitempty"`
	// RetryAfter is when it is worth trying again, set while backing off.
	RetryAfter *time.Time `json:"retryAfter,omitempty"`
	FiveHour   *Window    `json:"fiveHour"`
	SevenDay   *Window    `json:"sevenDay"`
	LastKnown  *Snapshot  `json:"lastKnown,omitempty"`
	Limits     []Limit    `json:"limits,omitempty"`
}

// HasNumbers reports whether this result carries usable utilisation figures.
//
// Callers use it to decide whether to draw a bar at all. A false here must never
// become a bar at 0%.
func (r Result) HasNumbers() bool {
	return r.State == StateOK && (r.FiveHour != nil || r.SevenDay != nil)
}

// Document is the top-level shape of `acs quota --json`.
type Document struct {
	SchemaVersion int      `json:"schemaVersion"`
	Profiles      []Result `json:"profiles"`
}

// apiResponse mirrors GET /api/oauth/usage.
//
// Every field is a pointer or a slice because the payload uses null freely --
// seven_day_opus and friends are null on most accounts. Unknown fields are ignored
// rather than rejected: the endpoint is undocumented and will change.
type apiResponse struct {
	FiveHour       *apiWindow `json:"five_hour"`
	SevenDay       *apiWindow `json:"seven_day"`
	SevenDayOpus   *apiWindow `json:"seven_day_opus"`
	SevenDaySonnet *apiWindow `json:"seven_day_sonnet"`
	SevenDayCowork *apiWindow `json:"seven_day_cowork"`
	Limits         []apiLimit `json:"limits"`
}

type apiWindow struct {
	Utilization  *float64 `json:"utilization"`
	ResetsAt     *string  `json:"resets_at"`
	LimitDollars *float64 `json:"limit_dollars"`
}

type apiLimit struct {
	Kind     string   `json:"kind"`
	Group    string   `json:"group"`
	Percent  *float64 `json:"percent"`
	Severity *string  `json:"severity"`
	ResetsAt *string  `json:"resets_at"`
}
