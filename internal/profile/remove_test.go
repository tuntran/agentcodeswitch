package profile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

// fakeCredStore records what was asked of it so tests can assert on Keychain
// effects without touching the real Keychain.
type fakeCredStore struct {
	items   map[string]string // service -> secret
	deleted []string
}

func newFakeCredStore() *fakeCredStore {
	return &fakeCredStore{items: map[string]string{}}
}

func (f *fakeCredStore) ServiceName(lit ConfigDirLiteral) string { return ServiceNameFor(lit) }

func (f *fakeCredStore) Exists(service string) (bool, error) {
	_, ok := f.items[service]
	return ok, nil
}

func (f *fakeCredStore) ReadSecret(service string) ([]byte, error) {
	s, ok := f.items[service]
	if !ok {
		return nil, errors.New("no such item")
	}
	return []byte(s), nil
}

func (f *fakeCredStore) Delete(service string) error {
	f.deleted = append(f.deleted, service)
	delete(f.items, service)
	return nil
}

func (f *fakeCredStore) List() ([]string, error) {
	services := make([]string, 0, len(f.items))
	for svc := range f.items {
		services = append(services, svc)
	}
	return services, nil
}

// setupProfile creates a profile with a transcript on disk, standing in for the
// Claude Code history that a purge would destroy.
func setupProfile(t *testing.T, name string) (Profile, *fakeCredStore) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)

	p, err := Create(name, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	transcripts := filepath.Join(p.Dir, "projects", "some-project")
	if err := os.MkdirAll(transcripts, 0o700); err != nil {
		t.Fatalf("mkdir transcripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(transcripts, "session.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	cs := newFakeCredStore()
	cs.items[cs.ServiceName(p.Literal)] = `{"claudeAiOauth":{"accessToken":"t"}}`
	return p, cs
}

// TestRemoveKeepsHistory is the default and the important case: removing a
// profile must not destroy the only copy of its Claude Code transcripts.
func TestRemoveKeepsHistory(t *testing.T) {
	p, cs := setupProfile(t, "per")
	service := cs.ServiceName(p.Literal)

	out, err := Remove(cs, "per", false, "")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if out.Purged {
		t.Error("Purged = true for a default removal")
	}
	if out.HistoryKept != p.Dir {
		t.Errorf("HistoryKept = %q, want %q", out.HistoryKept, p.Dir)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "projects", "some-project", "session.jsonl")); err != nil {
		t.Errorf("transcript was deleted by a non-purge removal: %v", err)
	}
	if len(cs.deleted) != 1 || cs.deleted[0] != service {
		t.Errorf("deleted = %v, want [%s]", cs.deleted, service)
	}
	if _, err := Get("per"); !errors.Is(err, ErrNotFound) {
		t.Errorf("profile still in config: %v", err)
	}
}

// TestRemovePurgeNeedsRetypedName keeps the UI from degrading the confirmation
// into a single button: the check lives here, not in either front end.
func TestRemovePurgeNeedsRetypedName(t *testing.T) {
	p, cs := setupProfile(t, "per")

	for _, typed := range []string{"", "PER", "pe", "yes"} {
		_, err := Remove(cs, "per", true, typed)
		if !errors.Is(err, ErrPurgeNotConfirmed) {
			t.Errorf("Remove(purge, typed=%q) = %v, want ErrPurgeNotConfirmed", typed, err)
		}
	}
	// Nothing may have happened yet: confirmation is checked before any delete.
	if len(cs.deleted) != 0 {
		t.Errorf("deleted %v before confirmation", cs.deleted)
	}
	if _, err := os.Stat(p.Dir); err != nil {
		t.Errorf("dir removed before confirmation: %v", err)
	}
	if _, err := Get("per"); err != nil {
		t.Errorf("config entry removed before confirmation: %v", err)
	}
}

func TestRemovePurgeDeletesWhenConfirmed(t *testing.T) {
	p, cs := setupProfile(t, "per")

	out, err := Remove(cs, "per", true, "per")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !out.Purged || out.HistoryKept != "" {
		t.Errorf("Removal = %+v, want purged with no history kept", out)
	}
	if _, err := os.Stat(p.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir still present after purge: %v", err)
	}
}

// TestRemovePurgeRefusesUnexpectedDir guards against a hand-edited config turning
// a purge into a recursive delete of something else.
func TestRemovePurgeRefusesUnexpectedDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)

	elsewhere := filepath.Join(t.TempDir(), "precious")
	if err := os.MkdirAll(elsewhere, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := config.Update(func(f *config.File) error {
		f.Profiles["per"] = config.Entry{Dir: elsewhere, ConfigDirLiteral: elsewhere}
		return nil
	})
	if err != nil {
		t.Fatalf("config.Update: %v", err)
	}

	cs := newFakeCredStore()
	if _, err := Remove(cs, "per", true, "per"); err == nil {
		t.Fatal("Remove purged a directory outside ~/.acs/profiles")
	}
	if _, err := os.Stat(elsewhere); err != nil {
		t.Errorf("directory outside ~/.acs was deleted: %v", err)
	}
}

// TestDiscardNewKeepsPreexistingHistory covers the sequence that made `acs rm`'s
// promise a lie:
//
//	acs rm per            -> keeps ~/.acs/profiles/per/projects (documented)
//	acs add per           -> MkdirAll no-ops onto the surviving directory
//	login cancelled       -> rollback used to RemoveAll that directory
//
// The transcripts are the only copy. A rollback may only remove what the failed
// command itself created.
func TestDiscardNewKeepsPreexistingHistory(t *testing.T) {
	p, cs := setupProfile(t, "per")
	transcript := filepath.Join(p.Dir, "projects", "some-project", "session.jsonl")

	// Step 1: a normal removal, which keeps the history.
	if _, err := Remove(cs, "per", false, ""); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Fatalf("precondition: transcript already gone: %v", err)
	}

	// Step 2: adding it back reuses the directory that is still there.
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Step 3: the login is cancelled.
	if err := DiscardNew("per"); err != nil {
		t.Fatalf("DiscardNew: %v", err)
	}

	if _, err := os.Stat(transcript); err != nil {
		t.Errorf("rollback destroyed pre-existing Claude Code history: %v", err)
	}
	if _, err := Get("per"); !errors.Is(err, ErrNotFound) {
		t.Errorf("config entry survived the rollback: %v", err)
	}
}

// TestDiscardNewRemovesDirectoryItCreated: with no history at stake, a cancelled
// add must not leave an empty profile directory behind.
func TestDiscardNewRemovesDirectoryItCreated(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	p, err := Create("per", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := DiscardNew("per"); err != nil {
		t.Fatalf("DiscardNew: %v", err)
	}
	if _, err := os.Stat(p.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty profile dir survived the rollback: %v", err)
	}
}

func TestInspectPurgeCountsTranscripts(t *testing.T) {
	p, _ := setupProfile(t, "per")

	plan, err := InspectPurge(p)
	if err != nil {
		t.Fatalf("InspectPurge: %v", err)
	}
	if plan.JSONLCount != 1 {
		t.Errorf("JSONLCount = %d, want 1", plan.JSONLCount)
	}
	if plan.Oldest.IsZero() || plan.Newest.IsZero() {
		t.Error("date range not reported")
	}
}

// TestInspectPurgeWithoutProjectsDir must not error: a profile can be created and
// never used.
func TestInspectPurgeWithoutProjectsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	p, err := Create("fresh", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	plan, err := InspectPurge(p)
	if err != nil {
		t.Fatalf("InspectPurge: %v", err)
	}
	if plan.JSONLCount != 0 {
		t.Errorf("JSONLCount = %d, want 0", plan.JSONLCount)
	}
}
