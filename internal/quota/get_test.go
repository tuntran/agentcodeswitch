package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// stubStore is a credential store whose contents the test controls.
type stubStore struct {
	blob    string
	missing bool
}

func (s *stubStore) ServiceName(lit profile.ConfigDirLiteral) string {
	return profile.ServiceNameFor(lit)
}

func (s *stubStore) Exists(string) (bool, error) { return !s.missing, nil }

func (s *stubStore) ReadSecret(string) ([]byte, error) { return []byte(s.blob), nil }

func (s *stubStore) Delete(string) error { return nil }

func (s *stubStore) List() ([]string, error) { return nil, nil }

// fullResponse is a real capture of the usage endpoint.
const fullResponse = `{
  "five_hour": {"utilization": 5, "resets_at": "2026-07-30T03:00:00Z", "limit_dollars": null},
  "seven_day": {"utilization": 68, "resets_at": "2026-08-01T00:00:00Z"},
  "seven_day_opus": null, "seven_day_sonnet": null, "seven_day_cowork": null,
  "limits": [
    {"kind": "session", "group": "session", "percent": 5, "severity": "normal",
     "resets_at": "2026-07-30T03:00:00Z"},
    {"kind": "weekly", "group": "weekly", "percent": 68, "severity": "normal",
     "resets_at": "2026-08-01T00:00:00Z"}
  ]
}`

// emptyResponse is what the endpoint returns when the token lacks user:profile:
// HTTP 200 with nothing in it. This is the trap the whole package guards against.
const emptyResponse = `{"five_hour": null, "seven_day": null, "limits": []}`

func credBlob(scopes string, expiresAt time.Time) string {
	return `{"claudeAiOauth":{"accessToken":"test-token","scopes":[` + scopes +
		`],"expiresAt":` + strconv.FormatInt(expiresAt.UnixMilli(), 10) + `,"subscriptionType":"max"}}`
}

// testProfile sets up ACS_HOME and returns a profile plus a store holding a healthy
// credential.
func testProfile(t *testing.T) (profile.Profile, *stubStore) {
	t.Helper()
	t.Setenv("ACS_HOME", t.TempDir())
	p, err := profile.Create("per", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return p, &stubStore{blob: credBlob(`"user:profile","user:inference"`, time.Now().Add(8*time.Hour))}
}

// newReaderFor points a Reader at a test server and pins the clock.
func newReaderFor(store profile.CredStore, url string, now time.Time) *Reader {
	r := NewReader(store, "test")
	r.client.baseURL = url
	r.now = func() time.Time { return now }
	return r
}

func serve(t *testing.T, status int, body string, headers map[string]string) (string, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		if got := req.Header.Get("anthropic-beta"); got != betaHeader {
			t.Errorf("anthropic-beta = %q, want %q", got, betaHeader)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

func TestGetMapsFullResponse(t *testing.T) {
	p, store := testProfile(t)
	url, _ := serve(t, http.StatusOK, fullResponse, nil)
	now := time.Now()

	res := newReaderFor(store, url, now).Get(context.Background(), p, false)
	if res.State != StateOK {
		t.Fatalf("State = %s, message %q", res.State, res.Message)
	}
	if res.FiveHour == nil || res.FiveHour.Utilization != 5 {
		t.Errorf("FiveHour = %+v, want 5%%", res.FiveHour)
	}
	if res.SevenDay == nil || res.SevenDay.Utilization != 68 {
		t.Errorf("SevenDay = %+v, want 68%%", res.SevenDay)
	}
	if res.FiveHour.ResetsAt == nil {
		t.Error("FiveHour.ResetsAt is nil")
	}
	if res.Stale {
		t.Error("Stale = true for a fresh fetch")
	}
	if len(res.Limits) != 2 {
		t.Errorf("Limits = %+v, want 2", res.Limits)
	}
}

// TestNeverReportsZeroWhenStateIsNotOK is the most important assertion in the
// package. Every failure path must leave the windows nil so no caller can render a
// bar at 0%.
func TestNeverReportsZeroWhenStateIsNotOK(t *testing.T) {
	tests := []struct {
		name      string
		wantState State
		setup     func(t *testing.T, store *stubStore) string // returns server URL
	}{
		{
			name:      "no credential",
			wantState: StateNoLogin,
			setup: func(t *testing.T, store *stubStore) string {
				store.missing = true
				url, _ := serve(t, http.StatusOK, fullResponse, nil)
				return url
			},
		},
		{
			// The trap: HTTP 200, empty body, healthy-looking account.
			name:      "missing user:profile scope",
			wantState: StateMissingScope,
			setup: func(t *testing.T, store *stubStore) string {
				store.blob = credBlob(`"user:inference"`, time.Now().Add(8*time.Hour))
				url, _ := serve(t, http.StatusOK, emptyResponse, nil)
				return url
			},
		},
		{
			name:      "expired token",
			wantState: StateExpired,
			setup: func(t *testing.T, store *stubStore) string {
				store.blob = credBlob(`"user:profile"`, time.Now().Add(-time.Hour))
				url, _ := serve(t, http.StatusOK, fullResponse, nil)
				return url
			},
		},
		{
			name:      "credential rejected",
			wantState: StateExpired,
			setup: func(t *testing.T, _ *stubStore) string {
				url, _ := serve(t, http.StatusUnauthorized, `{}`, nil)
				return url
			},
		},
		{
			name:      "server error",
			wantState: StateUnavailable,
			setup: func(t *testing.T, _ *stubStore) string {
				url, _ := serve(t, http.StatusInternalServerError, `{}`, nil)
				return url
			},
		},
		{
			name:      "endpoint unreachable",
			wantState: StateUnavailable,
			setup: func(_ *testing.T, _ *stubStore) string {
				return "http://127.0.0.1:1/api/oauth/usage"
			},
		},
		{
			name:      "response is not the expected shape",
			wantState: StateUnavailable,
			setup: func(t *testing.T, _ *stubStore) string {
				url, _ := serve(t, http.StatusOK, `<html>maintenance</html>`, nil)
				return url
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, store := testProfile(t)
			url := tt.setup(t, store)

			res := newReaderFor(store, url, time.Now()).Get(context.Background(), p, false)
			if res.State != tt.wantState {
				t.Errorf("State = %s, want %s (message %q)", res.State, tt.wantState, res.Message)
			}
			if res.FiveHour != nil || res.SevenDay != nil {
				t.Errorf("windows are non-nil for state %s: 5h=%+v weekly=%+v -- a caller could render 0%%",
					res.State, res.FiveHour, res.SevenDay)
			}
			if res.HasNumbers() {
				t.Error("HasNumbers() = true without usable numbers")
			}
			if res.Message == "" {
				t.Error("Message is empty; the user has no idea what to do")
			}
			// Rendering must never produce the literal "0%".
			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(raw), `"utilization":0`) {
				t.Errorf("serialised result contains a zero utilisation: %s", raw)
			}
		})
	}
}

// TestResponsesWithoutNumbersNeverBecomeZero covers the "state is ok but the number
// is absent" class -- the gap that let a limits[] entry with a null percent render as
// a 0% bar. Every one of these is a 200 that a naive mapper turns into zero.
func TestResponsesWithoutNumbersNeverBecomeZero(t *testing.T) {
	bodies := map[string]string{
		"nulled windows":               `{"five_hour": null, "seven_day": null, "limits": []}`,
		"empty object":                 `{}`,
		"zero-byte body":               ``,
		"limits with null percent":     `{"limits":[{"kind":"session","group":"session","percent":null}]}`,
		"limits with absent percent":   `{"limits":[{"kind":"session","group":"session"}]}`,
		"window with null utilization": `{"five_hour":{"utilization":null,"resets_at":null}}`,
		"unknown windows only":         `{"seven_day_opus": {"utilization": 12}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			p, store := testProfile(t)
			url, _ := serve(t, http.StatusOK, body, nil)

			res := newReaderFor(store, url, time.Now()).Get(context.Background(), p, false)
			if res.FiveHour != nil {
				t.Errorf("FiveHour = %+v, want nil", res.FiveHour)
			}
			if res.SevenDay != nil {
				t.Errorf("SevenDay = %+v, want nil", res.SevenDay)
			}
			if res.HasNumbers() {
				t.Error("HasNumbers() = true with no usable number in the response")
			}
			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(raw), `"utilization":0`) {
				t.Errorf("a zero utilisation was invented: %s", raw)
			}
		})
	}
}

// TestMeasuredZeroIsStillReported: a real 0% is data, and suppressing it would be
// the opposite mistake.
func TestMeasuredZeroIsStillReported(t *testing.T) {
	p, store := testProfile(t)
	url, _ := serve(t, http.StatusOK,
		`{"five_hour":{"utilization":0,"resets_at":"2026-08-01T00:00:00Z"}}`, nil)

	res := newReaderFor(store, url, time.Now()).Get(context.Background(), p, false)
	if res.FiveHour == nil {
		t.Fatal("FiveHour is nil for a measured zero")
	}
	if res.FiveHour.Utilization != 0 || !res.HasNumbers() {
		t.Errorf("FiveHour = %+v, want a reported 0%%", res.FiveHour)
	}
}

// TestCacheHitWithinTTL replaces the acceptance criterion that cannot be measured
// by hand: ten calls inside the TTL must produce exactly one request.
func TestCacheHitWithinTTL(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)
	reader := newReaderFor(store, url, time.Now())

	for range 10 {
		if res := reader.Get(context.Background(), p, false); res.State != StateOK {
			t.Fatalf("State = %s", res.State)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("made %d requests for 10 calls inside the TTL, want 1", got)
	}
}

// TestFetchesSynchronouslyPastTTL guards against the stale-while-revalidate bug:
// a detached goroutine would die with the CLI process and the cache would never
// update again.
func TestFetchesSynchronouslyPastTTL(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)

	start := time.Now()
	first := newReaderFor(store, url, start)
	if res := first.Get(context.Background(), p, false); res.State != StateOK {
		t.Fatalf("first Get: %s", res.State)
	}

	later := newReaderFor(store, url, start.Add(TTL+time.Minute))
	res := later.Get(context.Background(), p, false)
	if got := calls.Load(); got != 2 {
		t.Fatalf("made %d requests, want 2 -- the refresh was not synchronous", got)
	}
	if res.Stale {
		t.Error("Stale = true after a successful synchronous refresh")
	}
	if res.FiveHour == nil {
		t.Error("no numbers after a successful refresh")
	}
}

func TestForceBypassesCache(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)
	reader := newReaderFor(store, url, time.Now())

	reader.Get(context.Background(), p, false)
	reader.Get(context.Background(), p, true)
	if got := calls.Load(); got != 2 {
		t.Errorf("made %d requests, want 2 -- --force did not bypass the cache", got)
	}
}

// TestThrottleKeepsOldNumbersAndBacksOff: a 429 must not lose the numbers acs
// already has, and must not retry immediately.
func TestThrottleKeepsOldNumbersAndBacksOff(t *testing.T) {
	p, store := testProfile(t)
	okURL, _ := serve(t, http.StatusOK, fullResponse, nil)
	start := time.Now()
	if res := newReaderFor(store, okURL, start).Get(context.Background(), p, false); res.State != StateOK {
		t.Fatalf("seed fetch: %s", res.State)
	}

	throttleURL, throttleCalls := serve(t, http.StatusTooManyRequests, `{}`, nil)
	past := start.Add(TTL + time.Minute)
	res := newReaderFor(store, throttleURL, past).Get(context.Background(), p, false)

	if res.State != StateUnavailable {
		t.Errorf("State = %s, want unavailable", res.State)
	}
	// The previous reading survives, but under LastKnown -- the live windows stay nil
	// so "draw a bar" remains exactly State == StateOK.
	if res.FiveHour != nil || res.SevenDay != nil {
		t.Errorf("live windows are set on a failure: 5h=%+v weekly=%+v", res.FiveHour, res.SevenDay)
	}
	if res.LastKnown == nil {
		t.Fatal("LastKnown is nil; the cached reading was thrown away")
	}
	if res.LastKnown.FiveHour == nil || res.LastKnown.FiveHour.Utilization != 5 {
		t.Errorf("LastKnown.FiveHour = %+v, want the cached 5%%", res.LastKnown.FiveHour)
	}
	if res.LastKnown.FetchedAt.IsZero() {
		t.Error("LastKnown.FetchedAt is zero; the age of the number is unknowable")
	}
	if res.RetryAfter == nil {
		t.Fatal("RetryAfter is nil after a 429")
	}
	if !strings.Contains(res.Message, "try again at") {
		t.Errorf("message %q does not say when to retry", res.Message)
	}

	// While backing off, a further call must not hit the endpoint again.
	stillBackingOff := newReaderFor(store, throttleURL, past.Add(time.Second))
	stillBackingOff.Get(context.Background(), p, false)
	if got := throttleCalls.Load(); got != 1 {
		t.Errorf("made %d throttled requests, want 1 -- backoff was not respected", got)
	}
}

func TestThrottleHonoursRetryAfterHeader(t *testing.T) {
	p, store := testProfile(t)
	url, _ := serve(t, http.StatusTooManyRequests, `{}`, map[string]string{"Retry-After": "120"})
	now := time.Now()

	res := newReaderFor(store, url, now).Get(context.Background(), p, false)
	if res.RetryAfter == nil {
		t.Fatal("RetryAfter is nil")
	}
	// The server's own timing wins over the local ladder, which would have said 1m.
	if got := res.RetryAfter.Sub(now); got != 2*time.Minute {
		t.Errorf("RetryAfter in %s, want 2m from the header", got)
	}
}

func TestBackoffLadderClimbsAndCaps(t *testing.T) {
	tests := map[int]time.Duration{
		1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute,
		4: 8 * time.Minute, 5: 16 * time.Minute, 6: maxBackoff, 40: maxBackoff,
	}
	for step, want := range tests {
		if got := backoffDelay(step); got != want {
			t.Errorf("backoffDelay(%d) = %s, want %s", step, got, want)
		}
	}
}

func TestForceIgnoresBackoff(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusTooManyRequests, `{}`, nil)
	now := time.Now()
	reader := newReaderFor(store, url, now)

	reader.Get(context.Background(), p, false)
	reader.Get(context.Background(), p, true)
	if got := calls.Load(); got != 2 {
		t.Errorf("made %d requests, want 2 -- --force did not bypass the backoff", got)
	}
}

// TestCachedNeverHitsTheNetwork backs the promise that `acs ls` makes no requests.
func TestCachedNeverHitsTheNetwork(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)
	now := time.Now()

	if res := Cached(p.Name, now); res.State == StateOK {
		t.Error("Cached returned ok with nothing cached")
	} else if res.FiveHour != nil {
		t.Error("Cached invented numbers with an empty cache")
	}

	newReaderFor(store, url, now).Get(context.Background(), p, false)
	cached := Cached(p.Name, now)
	if cached.State != StateOK || cached.FiveHour == nil {
		t.Errorf("Cached = %+v, want the stored numbers", cached)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("made %d requests, want 1 -- Cached talked to the network", got)
	}
}

func TestCachedMarksStalePastTTL(t *testing.T) {
	p, store := testProfile(t)
	url, _ := serve(t, http.StatusOK, fullResponse, nil)
	start := time.Now()
	newReaderFor(store, url, start).Get(context.Background(), p, false)

	cached := Cached(p.Name, start.Add(TTL+time.Minute))
	if cached.State != StateOK {
		t.Errorf("State = %s, want ok -- the reading is old, not failed", cached.State)
	}
	if !cached.Stale {
		t.Error("Stale = false for a cache entry past its TTL")
	}
	// Still the live windows: `acs ls` exists to show this number, and Stale plus
	// FetchedAt is what qualifies it.
	if cached.FiveHour == nil {
		t.Error("stale numbers were dropped; old-and-labelled beats nothing")
	}
}

// TestCacheHoldsNoToken is the LBD: derived numbers only.
func TestCacheHoldsNoToken(t *testing.T) {
	p, store := testProfile(t)
	url, _ := serve(t, http.StatusOK, fullResponse, nil)
	newReaderFor(store, url, time.Now()).Get(context.Background(), p, false)

	raw, err := os.ReadFile(filepath.Join(cacheDir(), "quota-per.json"))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	for _, forbidden := range []string{"test-token", "accessToken", "refreshToken", "claudeAiOauth"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("cache file contains %q:\n%s", forbidden, raw)
		}
	}
}

func TestGetAllReturnsOnePerProfile(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	var profiles []profile.Profile
	for _, name := range []string{"com", "per"} {
		p, err := profile.Create(name, "")
		if err != nil {
			t.Fatalf("Create(%q): %v", name, err)
		}
		profiles = append(profiles, p)
	}
	store := &stubStore{blob: credBlob(`"user:profile"`, time.Now().Add(time.Hour))}
	url, _ := serve(t, http.StatusOK, fullResponse, nil)

	results := newReaderFor(store, url, time.Now()).GetAll(context.Background(), profiles, false)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Profile != "com" || results[1].Profile != "per" {
		t.Errorf("results = %s, %s", results[0].Profile, results[1].Profile)
	}
}

func TestBlocksOnlyForMissingCredential(t *testing.T) {
	if !Blocks(StateNoLogin) {
		t.Error("Blocks(no_login) = false")
	}
	for _, s := range []State{StateOK, StateMissingScope, StateExpired, StateUnavailable} {
		if Blocks(s) {
			t.Errorf("Blocks(%s) = true; a quota failure must never stop a switch", s)
		}
	}
}

// advancingClock moves forward a fixed step on every read, so each profile in a pass
// records a distinct FetchedAt. A pinned clock cannot exercise the bug where later
// profiles were skipped for a whole cycle, because it makes every FetchedAt equal.
func advancingClock(start time.Time, step time.Duration) func() time.Time {
	current := start
	return func() time.Time {
		now := current
		current = current.Add(step)
		return now
	}
}

// TestRefreshIgnoresTTL is the invariant behind the staleness fix, asserted directly
// rather than inferred from a request count: the schedule owns the cadence, so a
// cached entry still inside its TTL must not stop a scheduled refresh.
func TestRefreshIgnoresTTL(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)
	now := time.Now()
	r := newReaderFor(store, url, now)

	// Well inside the TTL, and past the churn floor.
	if err := saveEntry(p.Name, entry{State: StateOK, FetchedAt: now.Add(-60 * time.Second)}); err != nil {
		t.Fatalf("saveEntry: %v", err)
	}
	if !mustLoad(t, p.Name).fresh(now) {
		t.Fatal("test setup is wrong: the entry must be fresh for this to prove anything")
	}

	res := r.Refresh(context.Background(), p)
	if res.State != StateOK {
		t.Fatalf("State = %s, want ok", res.State)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 -- a fresh cache must not stop a scheduled refresh", got)
	}
}

// TestRefreshHonoursFloor keeps the churn guard honest. The dashboard restarts
// Subscribe on every mount, so without this a user flicking between views issues one
// full pass per flick.
func TestRefreshHonoursFloor(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)
	now := time.Now()
	r := newReaderFor(store, url, now)

	if err := saveEntry(p.Name, entry{State: StateOK, FetchedAt: now.Add(-5 * time.Second)}); err != nil {
		t.Fatalf("saveEntry: %v", err)
	}

	r.Refresh(context.Background(), p)
	if got := calls.Load(); got != 0 {
		t.Errorf("requests = %d, want 0 within the %s floor", got, refreshFloor)
	}
}

// TestRefreshHonoursBackoffButForceDoesNot pins the split that keeps the retry button
// working. The button only ever appears on an unavailable card -- the state that
// carries Retry-After -- so routing it through the scheduled path would make it a
// permanent no-op.
func TestRefreshHonoursBackoffButForceDoesNot(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)
	now := time.Now()
	r := newReaderFor(store, url, now)

	retryAt := now.Add(10 * time.Minute)
	if err := saveEntry(p.Name, entry{
		State:      StateUnavailable,
		FetchedAt:  now.Add(-2 * time.Hour),
		RetryAfter: &retryAt,
	}); err != nil {
		t.Fatalf("saveEntry: %v", err)
	}

	r.Refresh(context.Background(), p)
	if got := calls.Load(); got != 0 {
		t.Errorf("Refresh requests = %d, want 0 while backing off", got)
	}

	r.Get(context.Background(), p, true)
	if got := calls.Load(); got != 1 {
		t.Errorf("force requests = %d, want 1 -- force must still override backoff", got)
	}
}

// TestSubscribeRefreshesEveryProfileEveryPass locks both halves of the staleness bug:
// the first pass runs before the first tick (so a fresh mount shows real numbers), and
// every profile is refreshed in every pass regardless of how their FetchedAt values
// drifted apart within the previous one.
func TestSubscribeRefreshesEveryProfileEveryPass(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	var profiles []profile.Profile
	for _, name := range []string{"one", "two", "three"} {
		p, err := profile.Create(name, "")
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		profiles = append(profiles, p)
	}
	store := &stubStore{blob: credBlob(`"user:profile"`, time.Now().Add(8*time.Hour))}
	url, calls := serve(t, http.StatusOK, fullResponse, nil)

	r := NewReader(store, "test")
	r.client.baseURL = url
	// Each read advances well past the floor, mirroring real per-profile drift.
	r.now = advancingClock(time.Now(), 45*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Long enough that a tick cannot already be buffered when the ninth result
	// arrives: otherwise cancel() below races a third pass and the count is a
	// coin flip.
	results := r.Subscribe(ctx, profiles, 200*time.Millisecond)

	// 3 cached + 3 from the pre-tick pass + 3 from the pass after one tick.
	deadline := time.After(10 * time.Second)
	for i := range 9 {
		select {
		case _, ok := <-results:
			if !ok {
				t.Fatalf("channel closed after %d results", i)
			}
		case <-deadline:
			t.Fatalf("timed out after %d of 9 results", i)
		}
	}
	cancel()

	// One tick elapsed, so two passes ran: profiles x (ticks + 1).
	if got := calls.Load(); got != 6 {
		t.Errorf("requests = %d, want 6 (3 profiles x 2 passes) -- a later profile must not be skipped for a cycle", got)
	}
}

func mustLoad(t *testing.T, name string) entry {
	t.Helper()
	e, ok := loadEntry(name)
	if !ok {
		t.Fatalf("loadEntry(%s) found nothing", name)
	}
	return e
}

// TestForceDoesNotClimbTheBackoffLadder keeps a human override from making the
// schedule's penalty worse. The retry button reaches a throttled card by design, so
// if each click advanced the ladder an impatient user could stall the background loop
// for half an hour by asking for fresher numbers.
func TestForceDoesNotClimbTheBackoffLadder(t *testing.T) {
	p, store := testProfile(t)
	// No Retry-After header, so the local ladder is what decides the delay.
	url, _ := serve(t, http.StatusTooManyRequests, "", nil)
	now := time.Now()
	r := newReaderFor(store, url, now)

	if err := saveEntry(p.Name, entry{
		State:       StateUnavailable,
		FetchedAt:   now.Add(-time.Hour),
		BackoffStep: 3,
	}); err != nil {
		t.Fatalf("saveEntry: %v", err)
	}

	r.Get(context.Background(), p, true)

	got := mustLoad(t, p.Name)
	if got.BackoffStep != 3 {
		t.Errorf("BackoffStep = %d after a forced refresh, want it held at 3", got.BackoffStep)
	}
	// Still records a retry time, just without lengthening the ladder.
	if got.RetryAfter == nil {
		t.Error("RetryAfter = nil; a 429 must still park the scheduled path")
	}
}

// TestScheduledRefreshStillClimbsTheLadder is the other half: the automatic path must
// keep backing off, or the fix above would remove the backoff entirely.
func TestScheduledRefreshStillClimbsTheLadder(t *testing.T) {
	p, store := testProfile(t)
	url, _ := serve(t, http.StatusTooManyRequests, "", nil)
	now := time.Now()
	r := newReaderFor(store, url, now)

	if err := saveEntry(p.Name, entry{
		State:       StateUnavailable,
		FetchedAt:   now.Add(-time.Hour),
		BackoffStep: 3,
	}); err != nil {
		t.Fatalf("saveEntry: %v", err)
	}

	r.Refresh(context.Background(), p)

	if got := mustLoad(t, p.Name).BackoffStep; got != 4 {
		t.Errorf("BackoffStep = %d after a scheduled refresh, want 4", got)
	}
}

// TestFloorToleratesAClockInTheFuture stops a backwards clock step, or a cache file
// carried between machines, from disabling the scheduled refresh permanently.
func TestFloorToleratesAClockInTheFuture(t *testing.T) {
	p, store := testProfile(t)
	url, calls := serve(t, http.StatusOK, fullResponse, nil)
	now := time.Now()
	r := newReaderFor(store, url, now)

	if err := saveEntry(p.Name, entry{State: StateOK, FetchedAt: now.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("saveEntry: %v", err)
	}

	r.Refresh(context.Background(), p)
	if got := calls.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 -- a future timestamp must not park refreshes forever", got)
	}
}

// TestConcurrentFetchesKeepTheSuccessfulReading pins the cache against the one
// pairing that makes its read-modify-write reachable: the refresh loop and the
// card's Refresh button fetching the same profile at the same moment.
//
// The throttled path rebuilds the entire entry from the snapshot it read on entry
// to get. Unserialised, a 429 landing after a 200 writes that 200 back out of
// existence -- so the dashboard keeps painting numbers the cache no longer holds,
// and `acs quota` in a terminal reports the profile as unavailable.
func TestConcurrentFetchesKeepTheSuccessfulReading(t *testing.T) {
	p, store := testProfile(t)
	now := time.Now()

	// The first caller through succeeds; everyone after is throttled, and slowly, so
	// that an unlocked build reliably writes the stale rebuild last.
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fullResponse))
			return
		}
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Retry-After", "900")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// force: what the Refresh button sends, and the mode that skips every
			// cache check and so always reaches the fetch.
			newReaderFor(store, srv.URL, now).Get(context.Background(), p, true)
		}()
	}
	wg.Wait()

	e, ok := loadEntry(p.Name)
	if !ok {
		t.Fatal("no cache entry survived two concurrent fetches")
	}
	if e.FiveHour == nil {
		t.Error("the successful reading was lost: the throttled write rebuilt the entry " +
			"from a snapshot taken before that reading landed")
	}
	if e.RetryAfter == nil {
		t.Error("Retry-After was lost: the endpoint asked us to wait and the cache did not record it")
	}
}
