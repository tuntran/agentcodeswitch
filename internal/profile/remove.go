package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

// PurgePlan is what --purge is about to destroy, so a caller can show it before
// asking for confirmation.
//
// <dir>/projects/*.jsonl is the only copy of Claude Code's sessions and
// transcripts for that account. Nothing regenerates it.
type PurgePlan struct {
	Dir            string
	JSONLCount     int
	Oldest, Newest time.Time
}

// Removal reports what Remove actually did.
type Removal struct {
	KeychainService string
	// HistoryKept is where the transcripts still live, empty after a purge.
	HistoryKept string
	Purged      bool
}

// ErrPurgeNotConfirmed means the caller did not retype the profile name.
var ErrPurgeNotConfirmed = errors.New("purge not confirmed")

// InspectPurge counts the history a purge would delete.
func InspectPurge(p Profile) (PurgePlan, error) {
	plan := PurgePlan{Dir: p.Dir}
	root := filepath.Join(p.Dir, "projects")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil // nothing recorded yet
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		plan.JSONLCount++
		mt := info.ModTime()
		if plan.Oldest.IsZero() || mt.Before(plan.Oldest) {
			plan.Oldest = mt
		}
		if mt.After(plan.Newest) {
			plan.Newest = mt
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return plan, fmt.Errorf("inspect %s: %w", root, err)
	}
	return plan, nil
}

// Remove deletes a profile's Keychain item and config entry.
//
// The config dir is kept by default and only deleted when purge is set. Purging
// additionally requires typedName to equal name, so both the CLI and the UI have
// to obtain a retyped name -- the UI cannot soften this into a one-button
// dialog, because the check lives here rather than in either front end.
func Remove(cs CredStore, name string, purge bool, typedName string) (Removal, error) {
	p, err := Get(name)
	if err != nil {
		return Removal{}, err
	}
	if purge && typedName != name {
		return Removal{}, fmt.Errorf(
			"%w: retype %q to delete its Claude Code history", ErrPurgeNotConfirmed, name)
	}
	// Verify before deleting, never after.
	if purge {
		if err := assertRemovable(p); err != nil {
			return Removal{}, err
		}
	}

	service := cs.ServiceName(p.Literal)
	if err := cs.Delete(service); err != nil {
		return Removal{}, err
	}
	err = config.Update(func(f *config.File) error {
		if _, ok := f.Profiles[name]; !ok {
			return fmt.Errorf("%w: %q", ErrNotFound, name)
		}
		delete(f.Profiles, name)
		return nil
	})
	if err != nil {
		return Removal{}, err
	}

	out := Removal{KeychainService: service, HistoryKept: p.Dir}
	if purge {
		if err := os.RemoveAll(p.Dir); err != nil {
			return out, fmt.Errorf("delete %s: %w", p.Dir, err)
		}
		out.HistoryKept = ""
		out.Purged = true
	}
	return out, nil
}

// DiscardNew rolls back a profile whose add never completed.
//
// It removes the config entry, and removes the directory ONLY when that directory
// holds no Claude Code transcripts.
//
// The transcript check is the load-bearing part, not a nicety. `acs rm` keeps the
// directory on purpose, so `acs add <same name>` afterwards reuses it -- MkdirAll
// simply no-ops onto history that is already there. A rollback that deleted the
// directory would then destroy the only copy of that account's sessions, on the
// strength of a cancelled login. A rollback may only remove what the failed command
// itself created.
//
// The Keychain is left alone regardless. A partly finished login can have produced
// a real credential for a real account, and deleting that to tidy up would take
// away something the user has. An unreferenced item is harmless, and `acs doctor`
// lists it.
//
// Only for the add path. A failed `acs login` on an existing profile must leave
// everything exactly as it was.
func DiscardNew(name string) error {
	p, err := Get(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // nothing was registered yet
		}
		return err
	}
	if err := assertRemovable(p); err != nil {
		return err
	}

	// Checked before the entry is dropped, so a failure here leaves a consistent
	// state rather than an orphaned directory with no config entry.
	plan, err := InspectPurge(p)
	if err != nil {
		return err
	}

	if err := config.Update(func(f *config.File) error {
		delete(f.Profiles, name)
		return nil
	}); err != nil {
		return err
	}

	if plan.JSONLCount > 0 {
		return nil // history predates this add; keep every byte of it
	}
	if err := os.RemoveAll(p.Dir); err != nil {
		return fmt.Errorf("remove %s: %w", p.Dir, err)
	}
	return nil
}

// assertRemovable refuses to recursively delete anything that is not exactly the
// profile dir acs created.
//
// A hand-edited or corrupted config entry must not turn into an rm -rf of
// something else, and ~/.acs/profiles as a whole is never a valid target.
func assertRemovable(p Profile) error {
	base := filepath.Join(config.Home(), "profiles")
	expected := filepath.Join(base, p.Name)
	if p.Dir != expected {
		return fmt.Errorf(
			"refusing to purge %s: expected %s -- fix config.json or remove the directory yourself",
			p.Dir, expected)
	}
	if p.Literal.String() != expected {
		return fmt.Errorf(
			"refusing to purge %s: frozen literal is %s -- run `acs doctor`",
			expected, p.Literal.String())
	}
	return nil
}
