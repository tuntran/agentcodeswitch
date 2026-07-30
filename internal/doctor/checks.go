package doctor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/config"
	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// checkLiteral verifies the frozen CLAUDE_CONFIG_DIR string.
//
// Two distinct failures land here. A non-canonical literal (a hand-added trailing
// slash, say) hashes to a different Keychain name than the one Claude Code wrote.
// A canonical literal that no longer matches where ~/.acs lives means the whole
// state dir moved, so the credential is still in the Keychain under the old name.
//
// Both are reported, neither is repaired: rewriting the string would leave the
// real credential orphaned with no trace of where it went.
func checkLiteral(name string, e config.Entry) (profile.ConfigDirLiteral, Check) {
	lit, err := profile.LoadConfigDirLiteral(e.ConfigDirLiteral)
	if err != nil {
		return lit, Check{
			Name: "literal", Status: StatusFail,
			Detail: err.Error(),
			Action: fmt.Sprintf(
				"fix configDirLiteral for %q in %s to the canonical path, then run `acs login %s`",
				name, config.Path(), name),
		}
	}
	expected := filepath.Join(config.Home(), "profiles", name)
	if lit.String() != expected {
		return lit, Check{
			Name: "literal", Status: StatusFail,
			Detail: fmt.Sprintf("frozen literal %s does not match %s", lit.String(), expected),
			Action: fmt.Sprintf(
				"~/.acs looks like it moved; run `acs login %s` to authenticate under the new path", name),
		}
	}
	return lit, Check{
		Name: "literal", Status: StatusOK,
		Detail: lit.String(),
	}
}

// checkKeychain confirms a credential exists for this literal, using an
// attributes-only lookup so no dialog appears.
func checkKeychain(cs profile.CredStore, name string, lit profile.ConfigDirLiteral) Check {
	service := cs.ServiceName(lit)
	short := strings.TrimPrefix(service, profile.ServicePrefix+"-")
	exists, err := cs.Exists(service)
	switch {
	case err != nil:
		return Check{
			Name: "keychain", Status: StatusWarn, Detail: err.Error(),
			Action: "check that the login keychain is unlocked",
		}
	case !exists:
		return Check{
			Name: "keychain", Status: StatusFail,
			Detail: fmt.Sprintf("%s MISSING", short),
			Action: fmt.Sprintf("run `acs login %s`", name),
		}
	}
	return Check{Name: "keychain", Status: StatusOK, Detail: short}
}

// checkIdentity reads <dir>/.claude.json, which is a plain file read and so stays
// prompt-free. The email is redacted: doctor output gets pasted into issues.
func checkIdentity(name string, lit profile.ConfigDirLiteral, e config.Entry) Check {
	id, err := profile.ReadIdentity(lit)
	if err != nil {
		return Check{
			Name: "identity", Status: StatusFail, Detail: err.Error(),
			Action: fmt.Sprintf("run `acs login %s`", name),
		}
	}
	if e.Identity.Email != "" && e.Identity.Email != id.Email {
		return Check{
			Name: "identity", Status: StatusWarn,
			Detail: fmt.Sprintf("config says %s, config dir says %s",
				profile.RedactEmail(e.Identity.Email), profile.RedactEmail(id.Email)),
			Action: fmt.Sprintf("run `acs login %s` to refresh the cached identity", name),
		}
	}
	return Check{Name: "identity", Status: StatusOK, Detail: profile.RedactEmail(id.Email)}
}

// deepChecks read the credential itself, which is why they are opt-in.
//
// Scope is the check that matters most. Without user:profile the usage endpoint
// answers 200 with an empty body, so quota would look like 0% on a perfectly
// healthy account -- and 0% is the number that makes someone start a big task
// right before they get blocked.
func deepChecks(cs profile.CredStore, name string, lit profile.ConfigDirLiteral) []Check {
	cred, err := profile.ReadCredential(cs, lit)
	if err != nil {
		return []Check{{
			Name: "credential", Status: StatusFail, Detail: err.Error(),
			Action: fmt.Sprintf("run `acs login %s`", name),
		}}
	}

	scope := Check{Name: "scope", Status: StatusOK, Detail: profile.ScopeProfile + " present"}
	if !cred.CanReadQuota() {
		scope = Check{
			Name: "scope", Status: StatusFail,
			Detail: "no " + profile.ScopeProfile + " scope, so quota reads as empty",
			Action: fmt.Sprintf(
				"run `acs login %s` and choose the Claude subscription, not the Console", name),
		}
	}

	return []Check{scope, checkExpiry(name, cred)}
}

func checkExpiry(name string, cred profile.Credential) Check {
	if cred.ExpiresAt.IsZero() {
		return Check{
			Name: "expiry", Status: StatusWarn, Detail: "credential has no expiry field",
			Action: "no action needed unless quota starts failing",
		}
	}
	if cred.Expired(time.Now()) {
		return Check{
			Name: "expiry", Status: StatusFail,
			Detail: "expired " + cred.ExpiresAt.Format(time.RFC3339),
			Action: fmt.Sprintf(
				"run `acs %s` (Claude Code refreshes on start) or `acs login %s`", name, name),
		}
	}
	return Check{Name: "expiry", Status: StatusOK, Detail: cred.ExpiresAt.Format(time.RFC3339)}
}
