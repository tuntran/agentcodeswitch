package main

import (
	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// The profile half of the wire format. Split from views.go only because every Go
// file here stays under 200 non-blank lines; the DTO contract is the same one
// documented there.

// ProfileView is one row of the accounts table.
type ProfileView struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Email string `json:"email"`
	Plan  string `json:"plan"`
	// Model is the profile's default model WITHOUT the "[1m]" suffix, or "" when
	// Claude Code decides. Empty is a real state here, not a missing value: it is
	// what the UI has to show as "default" rather than as an unset field.
	Model string `json:"model"`
	// Context1M is the extended-context option, on by default. The UI renders it
	// as a checkbox beside Model rather than as part of the id, so turning it off
	// gives the plain name back.
	Context1M bool `json:"context1m"`
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
		Model:            p.Model,
		Context1M:        p.Context1M,
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
