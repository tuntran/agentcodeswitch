package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

// Identity is who a config dir currently belongs to, read from
// <dir>/.claude.json. This is a plain file read: no Keychain, no prompt, which is
// what lets `acs ls` and `acs doctor` stay silent.
type Identity struct {
	Email       string
	AccountUUID string
	OrgUUID     string
	OrgName     string
	// OrgType is Claude Code's plan marker, e.g. "claude_max". The
	// subscriptionType cached at login is the authoritative one; this is the
	// fallback when config has no cached identity yet.
	OrgType string
}

// ErrNoIdentity means the config dir has no usable oauthAccount: either nothing
// has ever logged in there, or the login did not complete.
var ErrNoIdentity = errors.New("no oauth account in config dir")

// claudeConfig mirrors just the part of .claude.json acs cares about. Everything
// else in that file belongs to Claude Code and is left untouched -- acs only ever
// reads it.
type claudeConfig struct {
	OAuthAccount *struct {
		AccountUUID      string `json:"accountUuid"`
		EmailAddress     string `json:"emailAddress"`
		OrganizationUUID string `json:"organizationUuid"`
		OrganizationName string `json:"organizationName"`
		OrganizationType string `json:"organizationType"`
	} `json:"oauthAccount"`
}

// ReadIdentity loads the identity for a profile's config dir.
//
// Parsing is deliberately tolerant: Claude Code owns this file's shape and adds
// fields freely, so unknown ones are ignored rather than treated as corruption.
func ReadIdentity(lit ConfigDirLiteral) (Identity, error) {
	if lit.IsZero() {
		return Identity{}, fmt.Errorf("%w: no config dir literal", ErrNoIdentity)
	}
	path := filepath.Join(lit.String(), ".claude.json")
	// #nosec G304 -- path comes from a validated ConfigDirLiteral.
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Identity{}, fmt.Errorf("%w: %s does not exist", ErrNoIdentity, path)
	}
	if err != nil {
		return Identity{}, fmt.Errorf("read %s: %w", path, err)
	}

	var cc claudeConfig
	if err := json.Unmarshal(raw, &cc); err != nil {
		return Identity{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if cc.OAuthAccount == nil || cc.OAuthAccount.EmailAddress == "" {
		return Identity{}, fmt.Errorf("%w: %s has no oauthAccount", ErrNoIdentity, path)
	}
	return Identity{
		Email:       cc.OAuthAccount.EmailAddress,
		AccountUUID: cc.OAuthAccount.AccountUUID,
		OrgUUID:     cc.OAuthAccount.OrganizationUUID,
		OrgName:     cc.OAuthAccount.OrganizationName,
		OrgType:     cc.OAuthAccount.OrganizationType,
	}, nil
}

// ResolvedIdentity is who this profile belongs to, for display.
//
// It prefers the identity cached at login, which came from `claude auth status` and is
// authoritative. When nothing is cached -- a profile registered outside acs, or one
// created by the UI before its terminal login finished -- it falls back to reading
// <dir>/.claude.json.
//
// The fallback matters because the alternative is showing an em dash next to a profile
// that is plainly logged in, with the answer one prompt-free file read away. Reading
// that file raises no Keychain dialog, so `acs ls` keeps its promise.
//
// The plan string differs by source: `auth status` says "max" while .claude.json says
// "claude_max". The cached value wins whenever it exists, so the difference only shows
// up for a profile acs has never authenticated itself.
func (p Profile) ResolvedIdentity() config.Identity {
	if p.Cached.Email != "" {
		return p.Cached
	}
	id, err := ReadIdentity(p.Literal)
	if err != nil {
		return p.Cached
	}
	return config.Identity{
		Email:            id.Email,
		OrgID:            id.OrgUUID,
		OrgName:          id.OrgName,
		SubscriptionType: id.OrgType,
	}
}

// SameAccount reports profiles that share an email with another profile.
//
// Worth warning about, never worth blocking: two config dirs on one account is a
// legitimate setup, for instance different settings per project.
func SameAccount(profiles []Profile) map[string][]string {
	byEmail := map[string][]string{}
	for _, p := range profiles {
		if p.Cached.Email == "" {
			continue
		}
		byEmail[p.Cached.Email] = append(byEmail[p.Cached.Email], p.Name)
	}
	dupes := map[string][]string{}
	for email, names := range byEmail {
		if len(names) > 1 {
			dupes[email] = names
		}
	}
	return dupes
}

// RedactEmail shortens an address to "a***@example.com".
//
// Used everywhere acs writes an email somewhere it might outlive the terminal --
// doctor output, spike reports, logs. A domain is enough to tell two accounts
// apart, which is the only thing acs needs it for.
func RedactEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		if email == "" {
			return "(none)"
		}
		return "***"
	}
	return email[:1] + "***" + email[at:]
}
