package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// setupShared builds a temp HOME with a default config dir holding tooling, plus one
// profile.
func setupShared(t *testing.T) Profile {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ACS_HOME", filepath.Join(home, ".acs"))

	root := DefaultConfigDir()
	for _, name := range []string{"skills", "agents", "hooks", "rules", "plugins"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "marker.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"model":"x"}`), 0o600); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	p, err := Create("per", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return p
}

func TestLinkSharedMakesToolingReachable(t *testing.T) {
	p := setupShared(t)

	results, err := LinkShared(p, false)
	if err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	for _, r := range results {
		if r.Outcome != LinkCreated && r.Outcome != LinkSourceMissing {
			t.Errorf("%s = %s, want linked", r.Name, r.Outcome)
		}
	}

	// The point of the exercise: a file in the default config dir is reachable
	// through the profile's path.
	got, err := os.ReadFile(filepath.Join(p.Dir, "skills", "marker.txt"))
	if err != nil {
		t.Fatalf("read through link: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("content = %q", got)
	}
}

// TestLinkSharedNeverSharesIdentityOrHistory is the guard that matters most. Sharing
// projects/ would merge two accounts' transcripts; sharing .claude.json would give
// both profiles one identity. Either would undo the whole tool.
func TestLinkSharedNeverSharesIdentityOrHistory(t *testing.T) {
	for _, name := range []string{".claude.json", "projects", "todos", "history.jsonl"} {
		if slices.Contains(SharedAssets, name) {
			t.Errorf("SharedAssets contains %q, which is per-account", name)
		}
	}

	p := setupShared(t)
	if _, err := LinkShared(p, false); err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	for _, name := range []string{".claude.json", "projects", "todos"} {
		info, err := os.Lstat(filepath.Join(p.Dir, name))
		if err != nil {
			continue // absent is fine; a link is not
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is a symlink into the shared config dir", name)
		}
	}
}

// TestAssertShareableRejectsPerProfile keeps a future edit from adding history to
// the shared list. The list is data, so only a check makes it enforceable.
func TestAssertShareableRejectsPerProfile(t *testing.T) {
	for _, name := range perProfileAssets {
		if err := assertShareable(name); err == nil {
			t.Errorf("assertShareable(%q) = nil, want an error", name)
		}
	}
	if err := assertShareable("skills"); err != nil {
		t.Errorf("assertShareable(skills) = %v, want nil", err)
	}
}

// TestLinkSharedLeavesRealContentAlone: silently moving something the user put there
// is not this function's call to make.
func TestLinkSharedLeavesRealContentAlone(t *testing.T) {
	p := setupShared(t)
	local := filepath.Join(p.Dir, "settings.json")
	if err := os.WriteFile(local, []byte(`{"mine":true}`), 0o600); err != nil {
		t.Fatalf("write local settings: %v", err)
	}

	results, err := LinkShared(p, false)
	if err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	if outcome := outcomeFor(results, "settings.json"); outcome != LinkBlocked {
		t.Errorf("settings.json = %s, want blocked", outcome)
	}
	raw, err := os.ReadFile(local)
	if err != nil || !strings.Contains(string(raw), "mine") {
		t.Errorf("local settings.json was disturbed: %q, %v", raw, err)
	}
}

// TestLinkSharedReplaceKeepsOldContent: --replace renames, it never deletes.
func TestLinkSharedReplaceKeepsOldContent(t *testing.T) {
	p := setupShared(t)
	local := filepath.Join(p.Dir, "settings.json")
	if err := os.WriteFile(local, []byte(`{"mine":true}`), 0o600); err != nil {
		t.Fatalf("write local settings: %v", err)
	}

	results, err := LinkShared(p, true)
	if err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	var moved string
	for _, r := range results {
		if r.Name == "settings.json" {
			if r.Outcome != LinkReplaced {
				t.Fatalf("settings.json = %s, want replaced", r.Outcome)
			}
			moved = r.MovedTo
		}
	}
	if moved == "" {
		t.Fatal("no MovedTo reported")
	}
	raw, err := os.ReadFile(moved)
	if err != nil || !strings.Contains(string(raw), "mine") {
		t.Errorf("previous content was not preserved at %s: %q, %v", moved, raw, err)
	}
	raw, err = os.ReadFile(local)
	if err != nil || !strings.Contains(string(raw), `"model"`) {
		t.Errorf("link does not resolve to the shared file: %q, %v", raw, err)
	}
}

func TestLinkSharedIsIdempotent(t *testing.T) {
	p := setupShared(t)
	if _, err := LinkShared(p, false); err != nil {
		t.Fatalf("first LinkShared: %v", err)
	}

	results, err := LinkShared(p, false)
	if err != nil {
		t.Fatalf("second LinkShared: %v", err)
	}
	for _, r := range results {
		if r.Outcome != LinkAlreadyCorrect && r.Outcome != LinkSourceMissing {
			t.Errorf("%s = %s on a repeat run, want already linked", r.Name, r.Outcome)
		}
	}
}

// TestLinkSharedRetargetsStaleLink covers ~/.claude having moved.
func TestLinkSharedRetargetsStaleLink(t *testing.T) {
	p := setupShared(t)
	stale := filepath.Join(p.Dir, "skills")
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), stale); err != nil {
		t.Fatalf("seed stale link: %v", err)
	}

	results, err := LinkShared(p, false)
	if err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	if outcome := outcomeFor(results, "skills"); outcome != LinkCreated {
		t.Errorf("skills = %s, want relinked", outcome)
	}
	target, err := os.Readlink(stale)
	if err != nil || target != filepath.Join(DefaultConfigDir(), "skills") {
		t.Errorf("skills points at %q, %v", target, err)
	}
}

func TestSharedStatusReportsUnlinked(t *testing.T) {
	p := setupShared(t)

	for _, r := range SharedStatus(p) {
		if r.Outcome == LinkAlreadyCorrect {
			t.Errorf("%s reported linked before linking", r.Name)
		}
	}
	if _, err := LinkShared(p, false); err != nil {
		t.Fatalf("LinkShared: %v", err)
	}
	for _, r := range SharedStatus(p) {
		if r.Outcome != LinkAlreadyCorrect && r.Outcome != LinkSourceMissing {
			t.Errorf("%s = %s after linking", r.Name, r.Outcome)
		}
	}
}

// TestMarkOnboardedPreservesEverythingElse: .claude.json is Claude Code's file, and
// acs writes exactly one key into it.
func TestMarkOnboardedPreservesEverythingElse(t *testing.T) {
	p := setupShared(t)
	path := filepath.Join(p.Dir, ".claude.json")
	original := `{"numStartups":7,"oauthAccount":{"emailAddress":"a@x.com"},"theme":"dark"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := MarkOnboarded(p.Literal); err != nil {
		t.Fatalf("MarkOnboarded: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc[onboardingKey] != true {
		t.Errorf("%s = %v, want true", onboardingKey, doc[onboardingKey])
	}
	if doc["numStartups"] != float64(7) || doc["theme"] != "dark" {
		t.Errorf("unrelated keys changed: %+v", doc)
	}
	account, ok := doc["oauthAccount"].(map[string]any)
	if !ok || account["emailAddress"] != "a@x.com" {
		t.Errorf("oauthAccount was disturbed: %+v", doc["oauthAccount"])
	}
	if !IsOnboarded(p.Literal) {
		t.Error("IsOnboarded() = false right after MarkOnboarded")
	}
}

// TestMarkOnboardedRefusesUnparseableFile: rewriting state we cannot read risks
// destroying the identity stored in it.
func TestMarkOnboardedRefusesUnparseableFile(t *testing.T) {
	p := setupShared(t)
	path := filepath.Join(p.Dir, ".claude.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := MarkOnboarded(p.Literal); err == nil {
		t.Error("MarkOnboarded overwrote a file it could not parse")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "{not json" {
		t.Errorf("file was modified: %q", raw)
	}
}

func TestMarkOnboardedCreatesMissingFile(t *testing.T) {
	p := setupShared(t)

	if err := MarkOnboarded(p.Literal); err != nil {
		t.Fatalf("MarkOnboarded: %v", err)
	}
	if !IsOnboarded(p.Literal) {
		t.Error("IsOnboarded() = false after creating the file")
	}
	info, err := os.Stat(filepath.Join(p.Dir, ".claude.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func outcomeFor(results []LinkResult, name string) LinkOutcome {
	for _, r := range results {
		if r.Name == name {
			return r.Outcome
		}
	}
	return ""
}
