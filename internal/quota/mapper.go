package quota

import "time"

// mapResponse turns the API payload into a Result.
//
// limits[] is preferred over the top-level windows because it is
// forward-compatible: when Anthropic adds a usage window, it appears there while
// the top-level fields stay as they are. The top-level five_hour/seven_day are the
// fallback for responses that omit limits[].
func mapResponse(resp apiResponse, now time.Time) Result {
	res := Result{State: StateOK, FetchedAt: &now}

	for _, l := range resp.Limits {
		// An entry with no percentage is dropped rather than defaulted to zero.
		// limits[] is preferred over the top-level windows, so a null percent here
		// would otherwise become a 0% bar on an ok result -- and would hide a
		// perfectly good five_hour value sitting underneath it.
		if l.Percent == nil {
			continue
		}
		limit := Limit{
			Kind:     l.Kind,
			Group:    l.Group,
			Percent:  roundPercent(l.Percent),
			ResetsAt: parseTime(l.ResetsAt),
		}
		limit.Severity = severityFor(limit.Percent, l.Severity)
		res.Limits = append(res.Limits, limit)
	}

	res.FiveHour = windowFromLimits(res.Limits, "session")
	if res.FiveHour == nil {
		res.FiveHour = windowFrom(resp.FiveHour)
	}
	res.SevenDay = windowFromLimits(res.Limits, "weekly")
	if res.SevenDay == nil {
		res.SevenDay = windowFrom(resp.SevenDay)
	}

	res.Message = Message(StateOK, "", "")
	return res
}

// windowFromLimits picks a limits[] entry by kind or group.
func windowFromLimits(limits []Limit, kind string) *Window {
	for _, l := range limits {
		if l.Kind != kind && l.Group != kind {
			continue
		}
		return &Window{
			Utilization: l.Percent,
			ResetsAt:    l.ResetsAt,
			Severity:    l.Severity,
		}
	}
	return nil
}

// windowFrom converts a top-level window. A nil input stays nil: seven_day_opus and
// friends are null on most accounts, and null is not zero.
func windowFrom(w *apiWindow) *Window {
	if w == nil || w.Utilization == nil {
		return nil
	}
	pct := roundPercent(w.Utilization)
	return &Window{
		Utilization:  pct,
		ResetsAt:     parseTime(w.ResetsAt),
		Severity:     severityFor(pct, nil),
		LimitDollars: w.LimitDollars,
	}
}

// severityFor decides how alarming a percentage is.
//
// A severity sent by the API always wins. Anthropic knows their real thresholds;
// the local 80% line is a guess that only applies when they tell us nothing.
func severityFor(percent int, apiSeverity *string) Severity {
	if apiSeverity != nil && *apiSeverity != "" {
		if *apiSeverity == "normal" {
			return SeverityNeutral
		}
		return SeverityAttention
	}
	if percent >= AttentionThreshold {
		return SeverityAttention
	}
	return SeverityNeutral
}

// roundPercent clamps a percentage into 0..100.
//
// A nil input must be filtered out by the caller before reaching here: this returns
// 0, and a 0 that means "unknown" is the one value this package exists to prevent
// from being displayed.
func roundPercent(v *float64) int {
	if v == nil {
		return 0
	}
	switch {
	case *v < 0:
		return 0
	case *v > 100:
		return 100
	}
	return int(*v + 0.5)
}

func parseTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
