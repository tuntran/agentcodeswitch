package profile

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ConfigDirLiteral is the exact string that goes into CLAUDE_CONFIG_DIR, and
// therefore the input to the Keychain service name hash.
//
// It is a struct with an unexported field on purpose. `type ConfigDirLiteral
// string` would stop nothing: `profile.ConfigDirLiteral(rawString)` compiles from
// any package, and that bare conversion is exactly what someone in a hurry
// reaches for to make the compiler quiet. An unexported field makes the
// conversion impossible, so the two constructors below are the only way in.
//
// Getting this string wrong fails silently and points the wrong way. Claude Code
// finds no credential, treats the dir as brand new, and asks for a login --
// leaving two credentials for one account, with `acs quota` reading one while the
// live session uses the other. It presents as "I got randomly logged out".
type ConfigDirLiteral struct{ s string }

// ErrNotCanonical means a stored literal is not in the frozen canonical form.
var ErrNotCanonical = errors.New("config dir literal is not canonical")

// NewConfigDirLiteral canonicalises a path into a literal. This is the only
// place canonicalisation happens, and it happens once, at `acs add`.
//
// Normalising is safe only because acs controls both ends: the string set into
// the environment is the same string that gets hashed, whatever Claude Code does
// internally. Do not "optimise" the NFC pass away -- macOS filesystems hand back
// NFD, and the hash Claude Code computes is over NFC.
func NewConfigDirLiteral(path string) (ConfigDirLiteral, error) {
	c := canonical(path)
	if err := validate(c); err != nil {
		return ConfigDirLiteral{}, err
	}
	return ConfigDirLiteral{s: c}, nil
}

// LoadConfigDirLiteral reads a literal that was frozen in config.json.
//
// It validates the invariant and refuses anything non-canonical. It does not
// re-canonicalise: a stored literal that drifted means the Keychain item is
// under a different name, and quietly rewriting the string would hide that the
// old credential is now unreachable. Report it and let doctor explain.
func LoadConfigDirLiteral(stored string) (ConfigDirLiteral, error) {
	if err := validate(stored); err != nil {
		return ConfigDirLiteral{}, err
	}
	if c := canonical(stored); c != stored {
		return ConfigDirLiteral{}, fmt.Errorf(
			"%w: stored %q, canonical form is %q -- run `acs doctor`",
			ErrNotCanonical, stored, c)
	}
	return ConfigDirLiteral{s: stored}, nil
}

// String returns the frozen literal.
func (c ConfigDirLiteral) String() string { return c.s }

// IsZero reports whether this is the unset zero value. Callers that reached a
// literal without going through a constructor land here.
func (c ConfigDirLiteral) IsZero() bool { return c.s == "" }

func canonical(path string) string {
	if path == "" {
		return ""
	}
	return norm.NFC.String(filepath.Clean(path))
}

func validate(s string) error {
	switch {
	case s == "":
		return fmt.Errorf("%w: empty", ErrNotCanonical)
	case !filepath.IsAbs(s):
		// "~/.acs/profiles/per" lands here: an unexpanded tilde hashes to
		// something different from the directory it looks like.
		return fmt.Errorf("%w: %q is not absolute", ErrNotCanonical, s)
	case s == "/":
		return fmt.Errorf("%w: %q is not a usable config dir", ErrNotCanonical, s)
	case strings.ContainsRune(s, 0):
		return fmt.Errorf("%w: %q contains a NUL byte", ErrNotCanonical, s)
	}
	return nil
}
