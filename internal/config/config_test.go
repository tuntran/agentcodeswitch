package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())

	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.Version != Version {
		t.Errorf("Version = %d, want %d", f.Version, Version)
	}
	if f.Profiles == nil {
		t.Error("Profiles is nil; callers would panic on assignment")
	}
}

func TestUpdateRoundTrips(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())

	err := Update(func(f *File) error {
		f.Profiles["per"] = Entry{
			Dir:              "profiles/per",
			ConfigDirLiteral: "/Users/x/.acs/profiles/per",
			Label:            "Personal",
			Identity:         Identity{Email: "a@x.com", SubscriptionType: "max"},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e, ok := got.Profiles["per"]
	if !ok {
		t.Fatal("profile per missing after Update")
	}
	if e.ConfigDirLiteral != "/Users/x/.acs/profiles/per" || e.Identity.Email != "a@x.com" {
		t.Errorf("round trip lost data: %+v", e)
	}
}

// TestUpdateErrorDoesNotWrite checks that a failed mutation leaves the previous
// config alone rather than half-applying it.
func TestUpdateErrorDoesNotWrite(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if err := Update(func(f *File) error {
		f.Profiles["keep"] = Entry{Dir: "profiles/keep"}
		return nil
	}); err != nil {
		t.Fatalf("seed Update: %v", err)
	}

	sentinel := errors.New("no")
	err := Update(func(f *File) error {
		f.Profiles["gone"] = Entry{Dir: "profiles/gone"}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update error = %v, want sentinel", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, exists := got.Profiles["gone"]; exists {
		t.Error("a failed Update was written to disk")
	}
	if _, exists := got.Profiles["keep"]; !exists {
		t.Error("a failed Update lost the previous config")
	}
}

// TestConcurrentUpdatesDoNotLoseEntries is the flock test.
//
// Atomic rename alone prevents a truncated file but not a lost update: two
// writers that both loaded the old config and then saved would each drop the
// other's change. v1 ships a CLI and a UI, so this is the everyday case rather
// than an edge case.
func TestConcurrentUpdatesDoNotLoseEntries(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())

	const writers = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("p%02d", i)
			errs <- Update(func(f *File) error {
				f.Profiles[name] = Entry{Dir: "profiles/" + name}
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Update: %v", err)
		}
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Profiles) != writers {
		t.Errorf("got %d profiles, want %d -- an update was lost", len(got.Profiles), writers)
	}
}

func TestLoadRejectsNewerVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	raw := fmt.Sprintf(`{"version": %d, "profiles": {}}`, Version+1)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(); !errors.Is(err, ErrUnknownVersion) {
		t.Errorf("Load = %v, want ErrUnknownVersion", err)
	}
}

func TestLoadRejectsCorruptJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(); err == nil {
		t.Error("Load accepted corrupt JSON")
	}
}

// TestWritePermissions keeps the config out of other users' reach: it holds email
// addresses and org identifiers.
func TestWritePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	if err := Update(func(*File) error { return nil }); err != nil {
		t.Fatalf("Update: %v", err)
	}

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}
}
