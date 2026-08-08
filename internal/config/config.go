// Package config owns ~/.acs/config.json: the profile registry and the frozen
// CLAUDE_CONFIG_DIR literals.
//
// It is deliberately dumb storage. It moves strings to and from disk and knows
// nothing about what a config dir literal means -- that invariant belongs to
// internal/profile, which is the only package allowed to construct one.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Version is the schema version written to disk. A file from a newer acs is an
// error rather than a guess: silently ignoring fields we do not understand would
// drop them on the next save.
//
// 2 added Entry.Model and Entry.NoContext1M. Adding fields does not break an older
// acs's ability to PARSE the file -- which is exactly the danger, and the reason
// the number moved. An acs still on 1 would read a 2 file as valid, discard both
// fields at json.Unmarshal, and marshal them away on its next write: every
// profile's pinned model gone, with nothing said. A mixed install is the normal
// case here, not a hypothetical, because the .app and the CLI are separate
// binaries that get replaced at different times. Refusing to load is the loud
// failure that data loss is not.
const Version = 2

// Identity is what `claude auth status --json` reported at login time. Cached so
// `acs ls` can show who a profile belongs to without touching the network or the
// Keychain.
type Identity struct {
	Email            string `json:"email,omitempty"`
	OrgID            string `json:"orgId,omitempty"`
	OrgName          string `json:"orgName,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
}

// Entry is one profile.
type Entry struct {
	// Dir is relative to Home() and used for file access, so moving ~/.acs
	// keeps working.
	Dir string `json:"dir"`
	// ConfigDirLiteral is the absolute string frozen at `acs add`: exactly what
	// was put into CLAUDE_CONFIG_DIR, and therefore the input to the Keychain
	// service name hash. Never recompute it.
	ConfigDirLiteral string `json:"configDirLiteral"`
	Label            string `json:"label,omitempty"`
	// Model is the default model this profile launches with, exported as
	// ANTHROPIC_MODEL. Empty means "whatever Claude Code picks", which is not the
	// same as any particular model and must stay distinguishable from one.
	//
	// It lives here rather than in the profile's settings.json because that file
	// is symlinked from ~/.claude and shared by every profile: a model written
	// there would move all of them at once.
	Model string `json:"model,omitempty"`
	// NoContext1M is stored inverted so that the default -- extended context on --
	// survives a config file written before the field existed. A positive
	// `context1m` would read as false there and silently turn 1M off for every
	// profile on upgrade. Read it through Profile.Context1M, which flips it back.
	NoContext1M bool     `json:"noContext1m,omitempty"`
	Identity    Identity `json:"identity,omitzero"`
}

// File is the whole config document.
type File struct {
	Version  int              `json:"version"`
	Profiles map[string]Entry `json:"profiles"`
}

// Home is the acs state directory. ACS_HOME overrides it, which is what tests
// use to stay away from the real one.
func Home() string {
	if h := os.Getenv("ACS_HOME"); h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".acs")
}

// Path is the config file location.
func Path() string { return filepath.Join(Home(), "config.json") }

func lockPath() string { return filepath.Join(Home(), "config.lock") }

// ErrUnknownVersion means the file was written by a newer acs.
var ErrUnknownVersion = errors.New("config was written by a newer acs")

// Load reads the config. A missing file is not an error: it is an acs that has
// never added a profile.
func Load() (File, error) {
	raw, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return File{Version: Version, Profiles: map[string]Entry{}}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}
	return decode(raw)
}

func decode(raw []byte) (File, error) {
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("parse %s: %w", Path(), err)
	}
	if f.Version > Version {
		return File{}, fmt.Errorf("%w: file says version %d, acs understands %d",
			ErrUnknownVersion, f.Version, Version)
	}
	if f.Version == 0 {
		f.Version = Version
	}
	if f.Profiles == nil {
		f.Profiles = map[string]Entry{}
	}
	return f, nil
}

// Update runs fn against the current config and saves the result.
//
// Load-modify-save happens inside an exclusive flock on ~/.acs/config.lock, so
// concurrent writers serialise instead of overwriting each other. That matters
// because v1 ships both a CLI and a UI: having one open while running the other
// is the normal case, not an edge case.
//
// The lock is what makes this safe, which is why there is no exported Save --
// reading outside the lock and writing later would reintroduce the lost update
// this function exists to prevent.
func Update(fn func(*File) error) error {
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", Home(), err)
	}
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()

	f, err := Load()
	if err != nil {
		return err
	}
	if err := fn(&f); err != nil {
		return err
	}
	f.Version = Version
	return write(f)
}

func lock() (func(), error) {
	// #nosec G304 -- path is derived from Home(), not from user input.
	fh, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	if err := syscall.Flock(int(fh.Fd()), syscall.LOCK_EX); err != nil {
		_ = fh.Close()
		return nil, fmt.Errorf("lock %s: %w", lockPath(), err)
	}
	return func() {
		_ = syscall.Flock(int(fh.Fd()), syscall.LOCK_UN)
		_ = fh.Close()
	}, nil
}

// write replaces the config atomically, so a crash mid-write cannot leave a
// truncated file where the profile registry used to be.
func write(f File) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(Home(), "config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(name, Path()); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
