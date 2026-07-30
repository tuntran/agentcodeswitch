package profile

import (
	"errors"
	"testing"
)

// TestServiceNameFor pins the Keychain namespacing formula.
//
// The 79a4535b vector is not synthetic: it is the name Claude Code actually
// created for ~/.ccs/instances/personal on the development machine. If this test
// ever fails, Claude Code changed how it namespaces credentials and the whole acs
// design needs revisiting -- do not adjust the expected values to match.
func TestServiceNameFor(t *testing.T) {
	tests := []struct {
		literal string
		want    string
	}{
		{"/Users/tuntran/.ccs/instances/personal", "Claude Code-credentials-79a4535b"},
		{"/Users/tuntran/.acs/profiles/per", "Claude Code-credentials-707f7e46"},
		{"/Users/tuntran/.acs/profiles/com", "Claude Code-credentials-2aee0b67"},
	}
	for _, tt := range tests {
		t.Run(tt.literal, func(t *testing.T) {
			lit, err := NewConfigDirLiteral(tt.literal)
			if err != nil {
				t.Fatalf("NewConfigDirLiteral(%q): %v", tt.literal, err)
			}
			if got := ServiceNameFor(lit); got != tt.want {
				t.Errorf("ServiceNameFor(%q) = %q, want %q", tt.literal, got, tt.want)
			}
		})
	}
}

// TestSpellingsHashDifferently is the reason ConfigDirLiteral exists: four ways of
// writing one directory are four different credentials.
func TestSpellingsHashDifferently(t *testing.T) {
	spellings := []string{
		"/Users/tuntran/.acs/profiles/per",
		"/Users/tuntran/.acs/profiles/per/",
		"~/.acs/profiles/per",
		"/Users/tuntran/.acs/profiles/./per",
	}
	seen := map[string]string{}
	for _, s := range spellings {
		// Hash the raw spelling, bypassing canonicalisation, to show what would
		// happen if acs passed these through unchanged.
		raw := ConfigDirLiteral{s: s}
		name := ServiceNameFor(raw)
		if prev, dup := seen[name]; dup {
			t.Errorf("%q and %q hash to the same service name %q", prev, s, name)
		}
		seen[name] = s
	}
	if len(seen) != len(spellings) {
		t.Errorf("got %d distinct service names, want %d", len(seen), len(spellings))
	}
}

// TestNewConfigDirLiteralCanonicalises checks that the three fixable spellings
// collapse to one, and the unfixable one is refused.
func TestNewConfigDirLiteralCanonicalises(t *testing.T) {
	const want = "/Users/tuntran/.acs/profiles/per"
	for _, in := range []string{
		want,
		want + "/",
		"/Users/tuntran/.acs/profiles/./per",
		"/Users/tuntran/.acs/profiles/com/../per",
	} {
		lit, err := NewConfigDirLiteral(in)
		if err != nil {
			t.Fatalf("NewConfigDirLiteral(%q): %v", in, err)
		}
		if lit.String() != want {
			t.Errorf("NewConfigDirLiteral(%q) = %q, want %q", in, lit.String(), want)
		}
	}
}

func TestNewConfigDirLiteralRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"~/.acs/profiles/per", // an unexpanded tilde is not the directory it looks like
		"profiles/per",        // relative
		"/",
	} {
		if _, err := NewConfigDirLiteral(in); err == nil {
			t.Errorf("NewConfigDirLiteral(%q) succeeded, want error", in)
		}
	}
}

// TestLoadConfigDirLiteralRejectsNonCanonical is the important half: a stored
// literal that drifted must be reported, never silently repaired. Repairing it
// would leave the real credential orphaned with no trace.
func TestLoadConfigDirLiteralRejectsNonCanonical(t *testing.T) {
	for _, stored := range []string{
		"/Users/tuntran/.acs/profiles/per/",
		"/Users/tuntran/.acs/profiles/./per",
		"~/.acs/profiles/per",
		"",
	} {
		_, err := LoadConfigDirLiteral(stored)
		if err == nil {
			t.Errorf("LoadConfigDirLiteral(%q) succeeded, want error", stored)
			continue
		}
		if !errors.Is(err, ErrNotCanonical) {
			t.Errorf("LoadConfigDirLiteral(%q) error = %v, want ErrNotCanonical", stored, err)
		}
	}
}

func TestLoadConfigDirLiteralAcceptsCanonical(t *testing.T) {
	const stored = "/Users/tuntran/.acs/profiles/per"
	lit, err := LoadConfigDirLiteral(stored)
	if err != nil {
		t.Fatalf("LoadConfigDirLiteral(%q): %v", stored, err)
	}
	if lit.String() != stored {
		t.Errorf("literal = %q, want %q", lit.String(), stored)
	}
	if lit.IsZero() {
		t.Error("IsZero() = true for a loaded literal")
	}
}

func TestZeroLiteralIsZero(t *testing.T) {
	var lit ConfigDirLiteral
	if !lit.IsZero() {
		t.Error("IsZero() = false for the zero value")
	}
}
