package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSeverityThresholds(t *testing.T) {
	tests := []struct {
		percent int
		want    Severity
	}{
		{0, SeverityNeutral},
		{79, SeverityNeutral},
		{80, SeverityAttention},
		{99, SeverityAttention},
		{100, SeverityAttention},
	}
	for _, tt := range tests {
		if got := severityFor(tt.percent, nil); got != tt.want {
			t.Errorf("severityFor(%d, nil) = %s, want %s", tt.percent, got, tt.want)
		}
	}
}

// TestAPISeverityWinsOverLocalThreshold: Anthropic knows their real thresholds and
// acs does not. The local 80% line is only a fallback.
func TestAPISeverityWinsOverLocalThreshold(t *testing.T) {
	normal := "normal"
	warning := "warning"

	// The API says this is fine even though it is over the local threshold.
	if got := severityFor(95, &normal); got != SeverityNeutral {
		t.Errorf("severityFor(95, \"normal\") = %s, want neutral", got)
	}
	// And it can raise the alarm below the local threshold.
	if got := severityFor(10, &warning); got != SeverityAttention {
		t.Errorf("severityFor(10, \"warning\") = %s, want attention", got)
	}
}

// TestLimitsArrayPreferredOverTopLevel: limits[] is where a newly added usage window
// shows up, so it wins when both are present.
func TestLimitsArrayPreferredOverTopLevel(t *testing.T) {
	sessionPct, weeklyPct := 42.0, 91.0
	topFive, topSeven := 5.0, 68.0
	resp := apiResponse{
		FiveHour: &apiWindow{Utilization: &topFive},
		SevenDay: &apiWindow{Utilization: &topSeven},
		Limits: []apiLimit{
			{Kind: "session", Group: "session", Percent: &sessionPct},
			{Kind: "weekly", Group: "weekly", Percent: &weeklyPct},
		},
	}

	res := mapResponse(resp, time.Now())
	if res.FiveHour.Utilization != 42 {
		t.Errorf("FiveHour = %d, want 42 from limits[]", res.FiveHour.Utilization)
	}
	if res.SevenDay.Utilization != 91 {
		t.Errorf("SevenDay = %d, want 91 from limits[]", res.SevenDay.Utilization)
	}
	if res.SevenDay.Severity != SeverityAttention {
		t.Errorf("SevenDay severity = %s, want attention at 91%%", res.SevenDay.Severity)
	}
}

func TestTopLevelUsedWhenLimitsAbsent(t *testing.T) {
	five, seven := 5.0, 68.0
	resetsAt := "2026-07-30T03:00:00Z"
	resp := apiResponse{
		FiveHour: &apiWindow{Utilization: &five, ResetsAt: &resetsAt},
		SevenDay: &apiWindow{Utilization: &seven},
	}

	res := mapResponse(resp, time.Now())
	if res.FiveHour == nil || res.FiveHour.Utilization != 5 {
		t.Errorf("FiveHour = %+v, want 5%%", res.FiveHour)
	}
	if res.FiveHour.ResetsAt == nil || !res.FiveHour.ResetsAt.Equal(
		time.Date(2026, 7, 30, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("ResetsAt = %v", res.FiveHour.ResetsAt)
	}
	if res.SevenDay == nil || res.SevenDay.Utilization != 68 {
		t.Errorf("SevenDay = %+v, want 68%%", res.SevenDay)
	}
}

// TestNullWindowsStayNil: seven_day_opus and friends are null on most accounts, and
// null is not zero.
func TestNullWindowsStayNil(t *testing.T) {
	res := mapResponse(apiResponse{}, time.Now())
	if res.FiveHour != nil || res.SevenDay != nil {
		t.Errorf("nil input produced windows: 5h=%+v weekly=%+v", res.FiveHour, res.SevenDay)
	}
	if res.HasNumbers() {
		t.Error("HasNumbers() = true for an empty response")
	}
}

// TestLimitWithNullPercentIsNotZero is the invariant-1 hole that slipped through:
// limits[] is preferred over the top-level windows, so an entry whose percent is
// null used to produce a non-nil Window at 0 -- a rendered 0% bar on an ok result,
// and one that masked a perfectly good five_hour value underneath.
func TestLimitWithNullPercentIsNotZero(t *testing.T) {
	topFive := 42.0
	resp := apiResponse{
		FiveHour: &apiWindow{Utilization: &topFive},
		Limits: []apiLimit{
			{Kind: "session", Group: "session", Percent: nil},
		},
	}

	res := mapResponse(resp, time.Now())
	if res.FiveHour == nil {
		t.Fatal("FiveHour is nil; the usable top-level value was discarded")
	}
	if res.FiveHour.Utilization != 42 {
		t.Errorf("FiveHour = %d%%, want the top-level 42%% rather than a zero from limits[]",
			res.FiveHour.Utilization)
	}
}

// TestLimitWithNullPercentAndNoFallbackStaysNil: with nothing usable anywhere, the
// window must be absent rather than zero.
func TestLimitWithNullPercentAndNoFallbackStaysNil(t *testing.T) {
	resp := apiResponse{
		Limits: []apiLimit{{Kind: "session", Group: "session", Percent: nil}},
	}

	res := mapResponse(resp, time.Now())
	if res.FiveHour != nil {
		t.Errorf("FiveHour = %+v, want nil for a null percent", res.FiveHour)
	}
	if res.HasNumbers() {
		t.Error("HasNumbers() = true with no usable percentage anywhere")
	}
}

func TestWindowWithNullUtilizationStaysNil(t *testing.T) {
	resp := apiResponse{FiveHour: &apiWindow{Utilization: nil}}
	if got := mapResponse(resp, time.Now()).FiveHour; got != nil {
		t.Errorf("FiveHour = %+v, want nil for a null utilization", got)
	}
}

func TestPercentClampedAndRounded(t *testing.T) {
	tests := map[float64]int{
		-5: 0, 0: 0, 4.4: 4, 4.6: 5, 99.5: 100, 140: 100,
	}
	for in, want := range tests {
		v := in
		if got := roundPercent(&v); got != want {
			t.Errorf("roundPercent(%v) = %d, want %d", in, got, want)
		}
	}
	if got := roundPercent(nil); got != 0 {
		t.Errorf("roundPercent(nil) = %d, want 0", got)
	}
}

func TestUnparseableTimestampBecomesNil(t *testing.T) {
	bad := "next tuesday"
	if got := parseTime(&bad); got != nil {
		t.Errorf("parseTime(%q) = %v, want nil", bad, got)
	}
	empty := ""
	if got := parseTime(&empty); got != nil {
		t.Errorf("parseTime(\"\") = %v, want nil", got)
	}
}

// TestJSONContractMatchesGolden pins the shape `acs quota --json` prints.
//
// It is a published surface: user scripts branch on state, and the desktop UI reads
// the same fields. Changing the shape without meaning to should fail here rather
// than in somebody's script.
func TestJSONContractMatchesGolden(t *testing.T) {
	fetchedAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	fiveReset := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	weekReset := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	doc := Document{
		SchemaVersion: SchemaVersion,
		Profiles: []Result{
			{
				Profile:   "per",
				State:     StateOK,
				Message:   Message(StateOK, "per", ""),
				FetchedAt: &fetchedAt,
				FiveHour:  &Window{Utilization: 5, ResetsAt: &fiveReset, Severity: SeverityNeutral},
				SevenDay:  &Window{Utilization: 91, ResetsAt: &weekReset, Severity: SeverityAttention},
				Limits: []Limit{
					{Kind: "session", Group: "session", Percent: 5,
						Severity: SeverityNeutral, ResetsAt: &fiveReset},
				},
			},
			{
				// A failed profile still appears, with null windows so nothing can
				// render it as 0%. Its previous reading rides along under lastKnown,
				// where it cannot be read as current.
				Profile:   "com",
				State:     StateUnavailable,
				Message:   Message(StateUnavailable, "com", "connection refused"),
				FetchedAt: &fetchedAt,
				LastKnown: &Snapshot{
					FetchedAt: fetchedAt.Add(-time.Hour),
					FiveHour:  &Window{Utilization: 12, Severity: SeverityNeutral},
					SevenDay:  nil,
				},
			},
		},
	}

	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "golden-quota.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with UPDATE_GOLDEN=1): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("JSON contract changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFailedProfileSerialisesNullWindows: the JSON a script reads must not be able
// to look like zero usage.
func TestFailedProfileSerialisesNullWindows(t *testing.T) {
	raw, err := json.Marshal(Result{Profile: "per", State: StateUnavailable, Message: "nope"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"fiveHour", "sevenDay"} {
		value, present := decoded[key]
		if !present {
			t.Errorf("%s is absent; a consumer cannot tell unknown from zero", key)
		}
		if value != nil {
			t.Errorf("%s = %v, want null", key, value)
		}
	}
}
