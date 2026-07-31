// Package profile owns the acs profile registry: names, frozen config dir
// literals, cached identity, and the Keychain items that go with them.
//
// It is pure Go with no UI dependency. The CLI and the Wails app both go through
// here, which is what keeps them from drifting apart.
package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

// Profile is a config entry with its literal resolved and validated.
type Profile struct {
	Name    string
	Dir     string // absolute, for file access
	Literal ConfigDirLiteral
	Label   string
	// Cached is the identity recorded at login time. It can be empty for a
	// profile that was created but never authenticated.
	Cached config.Identity
}

// reserved names would make the positional CLI grammar ambiguous:
// `acs add ls` has to mean "add a profile called ls", which makes `acs ls`
// unreachable.
//
// "report" stays on the list even though v1 has no such command. Drop it and
// somebody creates a profile named report; adding `acs report` later then breaks
// the grammar with no fix that does not take their profile away.
var reserved = []string{
	"add", "login", "link", "ls", "rm", "report", "quota", "ui", "doctor", "help", "version",
}

// Reserved returns the names a profile may not use.
func Reserved() []string { return slices.Clone(reserved) }

// nameRe requires ASCII lowercase and a leading alphanumeric.
//
// Accented names are rejected on purpose: the Keychain hash is over NFC while
// macOS hands back NFD, so "còng" would be a footgun that only shows up as a
// credential that cannot be found. The leading-character rule keeps a profile
// name from being mistaken for a flag.
var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

const maxNameLen = 64

// ErrReservedName means the name collides with a subcommand.
var ErrReservedName = errors.New("name is reserved for an acs command")

// ValidateName applies the naming rules.
func ValidateName(name string) error {
	switch {
	case name == "":
		return errors.New("profile name is empty")
	case len(name) > maxNameLen:
		return fmt.Errorf("profile name %q is longer than %d characters", name, maxNameLen)
	case slices.Contains(reserved, name):
		return fmt.Errorf("%w: %q (reserved: %s)",
			ErrReservedName, name, strings.Join(reserved, " "))
	case !nameRe.MatchString(name):
		return fmt.Errorf(
			"profile name %q is invalid: use ASCII lowercase letters, digits, %q and %q, starting with a letter or digit",
			name, "-", "_")
	}
	return nil
}

// resolve turns a stored entry into a Profile, validating the frozen literal.
func resolve(name string, e config.Entry) (Profile, error) {
	lit, err := LoadConfigDirLiteral(e.ConfigDirLiteral)
	if err != nil {
		return Profile{}, fmt.Errorf("profile %q: %w", name, err)
	}
	dir := e.Dir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(config.Home(), dir)
	}
	return Profile{
		Name:    name,
		Dir:     dir,
		Literal: lit,
		Label:   e.Label,
		Cached:  e.Identity,
	}, nil
}

// List returns every profile, sorted by name.
//
// A profile whose stored literal is broken is returned as an error rather than
// skipped: pretending it is not there is how a credential goes missing without
// anyone noticing. `acs doctor` reports the same condition with instructions.
func List() ([]Profile, error) {
	f, err := config.Load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(f.Profiles))
	for name := range f.Profiles {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]Profile, 0, len(names))
	var errs []error
	for _, name := range names {
		p, err := resolve(name, f.Profiles[name])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	return out, errors.Join(errs...)
}

// ErrNotFound means no profile has that name.
var ErrNotFound = errors.New("profile not found")

// Get returns one profile.
func Get(name string) (Profile, error) {
	f, err := config.Load()
	if err != nil {
		return Profile{}, err
	}
	e, ok := f.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return resolve(name, e)
}

// Create registers a profile and creates its config dir. It does not
// authenticate: that is `acs login`, and the UI needs this half on its own.
//
// The literal is frozen here, before anything can put it into CLAUDE_CONFIG_DIR.
// Writing it afterwards would leave a window where the string acs recorded and
// the string Claude Code hashed are not the same.
func Create(name, label string) (Profile, error) {
	if err := ValidateName(name); err != nil {
		return Profile{}, err
	}
	rel := filepath.Join("profiles", name)
	abs, err := filepath.Abs(filepath.Join(config.Home(), rel))
	if err != nil {
		return Profile{}, fmt.Errorf("resolve profile dir: %w", err)
	}
	lit, err := NewConfigDirLiteral(abs)
	if err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(lit.String(), 0o700); err != nil {
		return Profile{}, fmt.Errorf("create %s: %w", lit.String(), err)
	}
	err = config.Update(func(f *config.File) error {
		if _, exists := f.Profiles[name]; exists {
			return fmt.Errorf("profile %q already exists", name)
		}
		f.Profiles[name] = config.Entry{
			Dir:              rel,
			ConfigDirLiteral: lit.String(),
			Label:            label,
		}
		return nil
	})
	if err != nil {
		return Profile{}, err
	}
	return Profile{Name: name, Dir: abs, Literal: lit, Label: label}, nil
}

// SaveIdentity caches who a profile belongs to, after a login verified it.
func SaveIdentity(name string, id config.Identity) error {
	return config.Update(func(f *config.File) error {
		e, ok := f.Profiles[name]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNotFound, name)
		}
		e.Identity = id
		f.Profiles[name] = e
		return nil
	})
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
