package main

import (
	"time"

	"github.com/tuntran/agentcodeswitch/internal/doctor"
	"github.com/tuntran/agentcodeswitch/internal/profile"
	"github.com/tuntran/agentcodeswitch/internal/quota"
)

// The *View types are the wire format between Go and the frontend. They are DTOs,
// deliberately not the business types: the UI gets a shape chosen for rendering,
// and internal/ stays free to change without breaking TypeScript.
//
// No business rules live here. Deciding that 80% deserves attention belongs in
// internal/quota, so the CLI and the UI cannot disagree about it.

// ProfileView is one row of the accounts table.
type ProfileView struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Email string `json:"email"`
	Plan  string `json:"plan"`
	// KeychainHash is the 8-character suffix, which is what doctor and the spike
	// script show too.
	KeychainHash string `json:"keychainHash"`
	LoggedIn     bool   `json:"loggedIn"`
	// KeychainError is set when the store could not be read at all, which is
	// different from having no credential: the fix is to unlock the keychain, not
	// to log in again.
	KeychainError string `json:"keychainError"`
	OrgID         string `json:"orgId"`
	OrgName       string `json:"orgName"`
	// ConfigDirLiteral is exposed because it is the thing to copy into a bug
	// report when credentials go missing.
	ConfigDirLiteral string `json:"configDirLiteral"`
}

// WindowView is one usage window.
type WindowView struct {
	Utilization int    `json:"utilization"`
	ResetsAt    string `json:"resetsAt"`
	Severity    string `json:"severity"`
}

// SnapshotView is the last successful reading, shown on a failed result.
type SnapshotView struct {
	FetchedAt string      `json:"fetchedAt"`
	FiveHour  *WindowView `json:"fiveHour"`
	SevenDay  *WindowView `json:"sevenDay"`
}

// QuotaView is a profile's quota.
//
// FiveHour and SevenDay are pointers so that "unknown" survives the trip to
// TypeScript as null. Flattening them to 0 here would recreate, at the boundary,
// the exact lie internal/quota is built to prevent.
//
// Same contract as quota.Result: the windows are non-null exactly when state is
// "ok", and LastKnown appears only when it is not. The frontend therefore draws a
// bar on `state === "ok"` alone.
type QuotaView struct {
	Profile    string        `json:"name"`
	State      string        `json:"state"`
	Message    string        `json:"message"`
	Stale      bool          `json:"stale"`
	FetchedAt  string        `json:"fetchedAt"`
	RetryAfter string        `json:"retryAfter"`
	FiveHour   *WindowView   `json:"fiveHour"`
	SevenDay   *WindowView   `json:"sevenDay"`
	LastKnown  *SnapshotView `json:"lastKnown"`
}

// CheckView is one doctor check.
type CheckView struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Action string `json:"action"`
}

// DoctorProfileView groups a profile's checks.
type DoctorProfileView struct {
	Name   string      `json:"name"`
	Checks []CheckView `json:"checks"`
}

// DoctorView is a whole doctor run.
type DoctorView struct {
	Deep        bool                `json:"deep"`
	OK          bool                `json:"ok"`
	Profiles    []DoctorProfileView `json:"profiles"`
	Orphans     []string            `json:"orphans"`
	ConfigError string              `json:"configError"`
}

// PurgeView is what a purge would destroy, shown before asking for confirmation.
type PurgeView struct {
	Dir        string `json:"dir"`
	JSONLCount int    `json:"jsonlCount"`
	Oldest     string `json:"oldest"`
	Newest     string `json:"newest"`
}

func toProfileView(p profile.Profile, cs profile.CredStore) ProfileView {
	service := cs.ServiceName(p.Literal)
	// A lookup failure (a locked keychain, say) is not the same as "no credential".
	// Reporting it as not-logged-in would send the user to log in again for no
	// reason, and could talk them into a second credential for one account.
	exists, err := cs.Exists(service)
	keychainError := ""
	if err != nil {
		keychainError = err.Error()
	}
	id := p.ResolvedIdentity()
	return ProfileView{
		Name:             p.Name,
		Label:            p.Label,
		Email:            id.Email,
		Plan:             id.SubscriptionType,
		KeychainHash:     shortService(service),
		LoggedIn:         exists,
		KeychainError:    keychainError,
		OrgID:            id.OrgID,
		OrgName:          id.OrgName,
		ConfigDirLiteral: p.Literal.String(),
	}
}

func shortService(service string) string {
	if len(service) > len(profile.ServicePrefix)+1 {
		return service[len(profile.ServicePrefix)+1:]
	}
	return service
}

func toQuotaView(r quota.Result) QuotaView {
	return QuotaView{
		Profile:    r.Profile,
		State:      string(r.State),
		Message:    r.Message,
		Stale:      r.Stale,
		FetchedAt:  formatTime(r.FetchedAt),
		RetryAfter: formatTime(r.RetryAfter),
		FiveHour:   toWindowView(r.FiveHour),
		SevenDay:   toWindowView(r.SevenDay),
		LastKnown:  toSnapshotView(r.LastKnown),
	}
}

func toSnapshotView(s *quota.Snapshot) *SnapshotView {
	if s == nil {
		return nil
	}
	return &SnapshotView{
		FetchedAt: s.FetchedAt.Format(time.RFC3339),
		FiveHour:  toWindowView(s.FiveHour),
		SevenDay:  toWindowView(s.SevenDay),
	}
}

// toWindowView keeps nil as nil. This is the load-bearing line of the whole DTO
// layer: a zero-valued WindowView would render as a bar at 0%.
func toWindowView(w *quota.Window) *WindowView {
	if w == nil {
		return nil
	}
	return &WindowView{
		Utilization: w.Utilization,
		ResetsAt:    formatTime(w.ResetsAt),
		Severity:    string(w.Severity),
	}
}

func toDoctorView(r doctor.Report) DoctorView {
	// Slices stay non-nil so the frontend can read .length unconditionally; a nil
	// slice crosses the binding as null and `report.orphans.length` would throw.
	orphans := r.Orphans
	if orphans == nil {
		orphans = []string{}
	}
	out := DoctorView{
		Deep:        r.Deep,
		OK:          r.OK(),
		Orphans:     orphans,
		ConfigError: r.ConfigError,
		Profiles:    make([]DoctorProfileView, 0, len(r.Profiles)),
	}
	for _, p := range r.Profiles {
		view := DoctorProfileView{Name: p.Name, Checks: make([]CheckView, 0, len(p.Checks))}
		for _, c := range p.Checks {
			view.Checks = append(view.Checks, CheckView{
				Name:   c.Name,
				Status: string(c.Status),
				Detail: c.Detail,
				Action: c.Action,
			})
		}
		out.Profiles = append(out.Profiles, view)
	}
	return out
}

// formatTime renders a timestamp as RFC 3339, or an empty string when unknown.
// The frontend treats "" as absent rather than as the Unix epoch.
func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
