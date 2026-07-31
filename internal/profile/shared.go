package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CLAUDE_CONFIG_DIR relocates the whole config directory, not just credentials.
// A fresh profile therefore starts with no skills, agents, hooks, plugins or
// settings -- a bare Claude Code, which is not what anyone means by "switch
// account". The plan only ever asked whether credentials were isolated and never
// asked what else lives in that directory.
//
// The split is by ownership. Identity and history belong to the account and must
// stay separate. Tooling is account-independent and is shared with the default
// config dir by symlink: one copy, no drift, and editing either path edits the
// same file. Copying is not an option -- skills alone run to hundreds of
// megabytes, and a copy diverges the moment either side changes.

// SharedAssets are the parts of a config dir that belong to the tool rather than to
// the account.
//
// settings.json is included because hooks live there, and on this setup the hooks
// are what load the user's rules. Sharing it also shares any secret it contains,
// such as an MCP Authorization header -- see LinkShared's docs.
var SharedAssets = []string{
	"skills",
	"agents",
	"commands",
	"hooks",
	"rules",
	"plugins",
	"settings.json",
}

// perProfileAssets never get shared. Listed for documentation and for the guard in
// LinkShared: linking any of these would merge two accounts' identity or history,
// which is the exact failure this whole tool exists to prevent.
var perProfileAssets = []string{
	".claude.json", "projects", "todos", "history.jsonl",
	"sessions", "session-states", "shell-snapshots", "statsig",
}

// DefaultConfigDir is the config dir Claude Code uses when CLAUDE_CONFIG_DIR is
// unset, and the source of shared assets.
//
// Note it cannot itself be an acs profile: setting CLAUDE_CONFIG_DIR to this exact
// path selects the hashed Keychain name rather than the unsuffixed one, so the
// credential there becomes unreachable.
func DefaultConfigDir() string {
	return filepath.Join(os.Getenv("HOME"), ".claude")
}

// LinkOutcome is what happened to one asset.
type LinkOutcome string

const (
	// LinkCreated means the symlink was created or retargeted.
	LinkCreated LinkOutcome = "linked"
	// LinkAlreadyCorrect means it already pointed at the right place.
	LinkAlreadyCorrect LinkOutcome = "already linked"
	// LinkSourceMissing means the default config dir has no such asset.
	LinkSourceMissing LinkOutcome = "not present in the default config dir"
	// LinkBlocked means real content is in the way and was left alone.
	LinkBlocked LinkOutcome = "blocked by existing content"
	// LinkReplaced means existing content was moved aside and then linked.
	LinkReplaced LinkOutcome = "moved aside and linked"
)

// LinkResult reports one asset.
type LinkResult struct {
	Name    string
	Outcome LinkOutcome
	// MovedTo is where existing content went, for LinkReplaced.
	MovedTo string
}

// LinkShared points a profile's tooling at the default config dir.
//
// Call it before the first login. Claude Code creates settings.json during login,
// and a symlink already in place means it writes through to the shared file instead
// of creating a competing copy.
//
// replace decides what happens when real content is already there. Without it such
// an asset is reported and left untouched, because silently moving a file someone
// put there is not this function's call to make. Nothing is ever deleted: replace
// renames, it does not remove.
//
// Sharing settings.json shares everything in it, including any secret such as an MCP
// Authorization header. For one person's own profiles that is usually what they
// want; for a profile meant to stay separate from personal tooling it may not be.
func LinkShared(p Profile, replace bool) ([]LinkResult, error) {
	source := DefaultConfigDir()
	if p.Dir == source {
		return nil, fmt.Errorf("refusing to link %s onto itself", source)
	}

	out := make([]LinkResult, 0, len(SharedAssets))
	for _, name := range SharedAssets {
		if err := assertShareable(name); err != nil {
			return nil, err
		}
		result, err := linkOne(filepath.Join(source, name), filepath.Join(p.Dir, name), name, replace)
		if err != nil {
			return out, err
		}
		out = append(out, result)
	}
	return out, nil
}

// assertShareable is a guard against a future edit adding identity or history to
// SharedAssets. Sharing projects/ would merge two accounts' transcripts, and sharing
// .claude.json would give both profiles one identity.
func assertShareable(name string) error {
	for _, reserved := range perProfileAssets {
		if name == reserved {
			return fmt.Errorf(
				"%q holds per-account identity or history and must never be shared", name)
		}
	}
	return nil
}

func linkOne(source, dest, name string, replace bool) (LinkResult, error) {
	if _, err := os.Stat(source); err != nil {
		return LinkResult{Name: name, Outcome: LinkSourceMissing}, nil
	}

	info, err := os.Lstat(dest)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		if current, err := os.Readlink(dest); err == nil && current == source {
			return LinkResult{Name: name, Outcome: LinkAlreadyCorrect}, nil
		}
		if err := os.Remove(dest); err != nil {
			return LinkResult{}, fmt.Errorf("replace stale link %s: %w", dest, err)
		}
	case err == nil:
		if !replace {
			return LinkResult{Name: name, Outcome: LinkBlocked}, nil
		}
		movedTo := fmt.Sprintf("%s.acs-replaced-%s", dest, time.Now().Format("20060102-150405"))
		if err := os.Rename(dest, movedTo); err != nil {
			return LinkResult{}, fmt.Errorf("move %s aside: %w", dest, err)
		}
		if err := os.Symlink(source, dest); err != nil {
			return LinkResult{}, fmt.Errorf("link %s: %w", dest, err)
		}
		return LinkResult{Name: name, Outcome: LinkReplaced, MovedTo: movedTo}, nil
	}

	if err := os.Symlink(source, dest); err != nil {
		return LinkResult{}, fmt.Errorf("link %s: %w", dest, err)
	}
	return LinkResult{Name: name, Outcome: LinkCreated}, nil
}

// SharedStatus reports which assets are currently linked, for doctor.
func SharedStatus(p Profile) []LinkResult {
	source := DefaultConfigDir()
	out := make([]LinkResult, 0, len(SharedAssets))
	for _, name := range SharedAssets {
		dest := filepath.Join(p.Dir, name)
		if _, err := os.Stat(filepath.Join(source, name)); err != nil {
			out = append(out, LinkResult{Name: name, Outcome: LinkSourceMissing})
			continue
		}
		info, err := os.Lstat(dest)
		switch {
		case err != nil:
			out = append(out, LinkResult{Name: name, Outcome: LinkBlocked})
		case info.Mode()&os.ModeSymlink == 0:
			out = append(out, LinkResult{Name: name, Outcome: LinkBlocked})
		default:
			current, _ := os.Readlink(dest)
			if current == filepath.Join(source, name) {
				out = append(out, LinkResult{Name: name, Outcome: LinkAlreadyCorrect})
			} else {
				out = append(out, LinkResult{Name: name, Outcome: LinkBlocked})
			}
		}
	}
	return out
}
