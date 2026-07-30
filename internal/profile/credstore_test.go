package profile

import (
	"errors"
	"slices"
	"testing"
)

func TestOrphansIgnoresKnownAndDefault(t *testing.T) {
	per := mustLit(t, "/Users/x/.acs/profiles/per")
	com := mustLit(t, "/Users/x/.acs/profiles/com")
	stale := mustLit(t, "/Users/x/.acs/profiles/old")

	cs := newFakeCredStore()
	for _, lit := range []ConfigDirLiteral{per, com, stale} {
		cs.items[ServiceNameFor(lit)] = "{}"
	}
	// Plain Claude Code's own credential for ~/.claude. acs does not manage it, so
	// it must never be reported as something to clean up.
	cs.items[ServicePrefix] = "{}"

	got, err := Orphans(cs, []string{ServiceNameFor(per), ServiceNameFor(com)})
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	want := []string{ServiceNameFor(stale)}
	if !slices.Equal(got, want) {
		t.Errorf("Orphans = %v, want %v", got, want)
	}
}

func TestOrphansEmptyWhenAllKnown(t *testing.T) {
	per := mustLit(t, "/Users/x/.acs/profiles/per")
	cs := newFakeCredStore()
	cs.items[ServiceNameFor(per)] = "{}"

	got, err := Orphans(cs, []string{ServiceNameFor(per)})
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Orphans = %v, want none", got)
	}
}

func TestOrphansPropagatesListError(t *testing.T) {
	cs := &failingCredStore{}
	if _, err := Orphans(cs, nil); err == nil {
		t.Error("Orphans swallowed a store failure")
	}
}

type failingCredStore struct{ fakeCredStore }

func (failingCredStore) List() ([]string, error) { return nil, errors.New("keychain locked") }

func mustLit(t *testing.T, s string) ConfigDirLiteral {
	t.Helper()
	lit, err := NewConfigDirLiteral(s)
	if err != nil {
		t.Fatalf("NewConfigDirLiteral(%q): %v", s, err)
	}
	return lit
}
