package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// onboardingKey is the flag Claude Code checks before running its first-run wizard.
const onboardingKey = "hasCompletedOnboarding"

// MarkOnboarded records that a profile's config dir has been through onboarding.
//
// Without it the first `acs <profile>` run opens the first-run wizard, whose opening
// step is "Select login method" -- so a profile that is perfectly authenticated looks
// like a failed login, and the obvious response is to log in again when nothing is
// wrong. Same family of mistake as rendering an unknown quota as 0%: the interface
// asserts something untrue and pushes the user toward an unnecessary action.
//
// This is the one place acs writes to .claude.json, and it writes exactly one key.
// The file is Claude Code's, and everything else in it is read-only to acs; the
// credential itself lives in the Keychain and is never touched here. `acs doctor`
// reports the flag rather than assuming it, so a Claude Code change that stops
// honouring it shows up as a report instead of a silent no-op.
func MarkOnboarded(lit ConfigDirLiteral) error {
	if lit.IsZero() {
		return errors.New("no config dir literal")
	}
	path := filepath.Join(lit.String(), ".claude.json")

	doc := map[string]any{}
	// #nosec G304 -- path comes from a validated ConfigDirLiteral.
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &doc); err != nil {
			// Claude Code owns this file. Rewriting something we cannot parse would
			// risk destroying state we do not understand.
			return fmt.Errorf("parse %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read %s: %w", path, err)
	}

	if done, ok := doc[onboardingKey].(bool); ok && done {
		return nil
	}
	doc[onboardingKey] = true
	return writeClaudeConfig(path, doc)
}

// IsOnboarded reports whether the wizard will be skipped, for doctor.
func IsOnboarded(lit ConfigDirLiteral) bool {
	if lit.IsZero() {
		return false
	}
	// #nosec G304 -- path comes from a validated ConfigDirLiteral.
	raw, err := os.ReadFile(filepath.Join(lit.String(), ".claude.json"))
	if err != nil {
		return false
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	done, ok := doc[onboardingKey].(bool)
	return ok && done
}

// writeClaudeConfig replaces the file atomically, preserving mode 0600.
//
// Atomic because a crash mid-write would leave Claude Code's own state truncated,
// and that state includes the oauthAccount that identifies the profile.
func writeClaudeConfig(path string, doc map[string]any) error {
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".claude-*.json")
	if err != nil {
		return fmt.Errorf("create temp beside %s: %w", path, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.Write(append(encoded, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp beside %s: %w", path, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp beside %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp beside %s: %w", path, err)
	}
	return os.Rename(name, path)
}
