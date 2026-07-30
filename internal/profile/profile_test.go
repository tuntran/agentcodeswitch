package profile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"simple", "per", nil},
		{"digits and dashes", "work-2", nil},
		{"underscore", "a_b", nil},
		{"empty", "", errNonNil},
		// Accented names are rejected because the Keychain hash is over NFC while
		// macOS hands back NFD: "còng" would fail as a credential that cannot be
		// found, with nothing pointing at the name as the cause.
		{"accented", "còng", errNonNil},
		{"uppercase", "Per", errNonNil},
		{"space", "my profile", errNonNil},
		{"leading dash looks like a flag", "-per", errNonNil},
		{"reserved ls", "ls", ErrReservedName},
		{"reserved doctor", "doctor", ErrReservedName},
		// "report" has no command in v1 and stays reserved anyway: allow it now and
		// adding `acs report` later breaks the grammar with no fix that keeps the
		// user's profile.
		{"reserved report", "report", ErrReservedName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			switch {
			case tt.wantErr == nil && err != nil:
				t.Errorf("ValidateName(%q) = %v, want nil", tt.input, err)
			case tt.wantErr != nil && err == nil:
				t.Errorf("ValidateName(%q) = nil, want error", tt.input)
			case errors.Is(tt.wantErr, ErrReservedName) && !errors.Is(err, ErrReservedName):
				t.Errorf("ValidateName(%q) = %v, want ErrReservedName", tt.input, err)
			}
		})
	}
}

// errNonNil marks table rows that want any error.
var errNonNil = errors.New("any error")

func TestCreateFreezesLiteral(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)

	p, err := Create("per", "Personal")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wantLit := filepath.Join(home, "profiles", "per")
	if p.Literal.String() != wantLit {
		t.Errorf("literal = %q, want %q", p.Literal.String(), wantLit)
	}
	if _, err := os.Stat(wantLit); err != nil {
		t.Errorf("config dir was not created: %v", err)
	}

	// The stored form keeps dir relative so moving ~/.acs does not break file
	// access, while the literal stays absolute because it is hash input.
	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	e := f.Profiles["per"]
	if e.Dir != filepath.Join("profiles", "per") {
		t.Errorf("stored dir = %q, want relative %q", e.Dir, filepath.Join("profiles", "per"))
	}
	if e.ConfigDirLiteral != wantLit {
		t.Errorf("stored literal = %q, want %q", e.ConfigDirLiteral, wantLit)
	}
	if e.Label != "Personal" {
		t.Errorf("label = %q, want %q", e.Label, "Personal")
	}
}

func TestCreateRejectsDuplicate(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := Create("per", ""); err == nil {
		t.Error("second Create succeeded, want error")
	}
}

func TestCreateRejectsReservedName(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("quota", ""); !errors.Is(err, ErrReservedName) {
		t.Errorf("Create(\"quota\") = %v, want ErrReservedName", err)
	}
}

func TestListAndGet(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	for _, n := range []string{"com", "per"} {
		if _, err := Create(n, ""); err != nil {
			t.Fatalf("Create(%q): %v", n, err)
		}
	}

	got, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].Name != "com" || got[1].Name != "per" {
		t.Fatalf("List returned %v, want [com per] sorted", names(got))
	}
	if _, err := Get("per"); err != nil {
		t.Errorf("Get(per): %v", err)
	}
	if _, err := Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(nope) = %v, want ErrNotFound", err)
	}
}

// TestListReportsBrokenLiteral checks that a hand-edited literal surfaces as an
// error instead of a profile that quietly disappears from the list.
func TestListReportsBrokenLiteral(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := config.Update(func(f *config.File) error {
		e := f.Profiles["per"]
		e.ConfigDirLiteral += "/" // the trailing slash hashes to another item
		f.Profiles["per"] = e
		return nil
	})
	if err != nil {
		t.Fatalf("config.Update: %v", err)
	}

	got, listErr := List()
	if listErr == nil {
		t.Error("List() returned no error for a non-canonical literal")
	}
	if len(got) != 0 {
		t.Errorf("List() returned %v, want no usable profiles", names(got))
	}
	if _, err := Get("per"); !errors.Is(err, ErrNotCanonical) {
		t.Errorf("Get(per) = %v, want ErrNotCanonical", err)
	}
}

// TestResolvedIdentityFallsBackToConfigDir: a profile registered outside acs, or one
// whose terminal login has not been recorded yet, is plainly logged in. Showing an em
// dash for its account when the answer is one prompt-free file read away is just
// wrong.
func TestResolvedIdentityFallsBackToConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	p, err := Create("per", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	raw := `{"oauthAccount":{"emailAddress":"a@x.com","organizationUuid":"org-1",` +
		`"organizationName":"Org","organizationType":"claude_max"}}`
	if err := os.WriteFile(filepath.Join(p.Dir, ".claude.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	got := p.ResolvedIdentity()
	if got.Email != "a@x.com" {
		t.Errorf("Email = %q, want the address from .claude.json", got.Email)
	}
	if got.SubscriptionType != "claude_max" || got.OrgName != "Org" {
		t.Errorf("identity = %+v", got)
	}
}

// TestResolvedIdentityPrefersCache: the cached value came from `claude auth status`
// and is authoritative, including its different plan spelling ("max" vs "claude_max").
func TestResolvedIdentityPrefersCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	p, err := Create("per", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	raw := `{"oauthAccount":{"emailAddress":"stale@x.com","organizationType":"claude_max"}}`
	if err := os.WriteFile(filepath.Join(p.Dir, ".claude.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}
	p.Cached = config.Identity{Email: "cached@x.com", SubscriptionType: "max"}

	got := p.ResolvedIdentity()
	if got.Email != "cached@x.com" || got.SubscriptionType != "max" {
		t.Errorf("identity = %+v, want the cached one", got)
	}
}

// TestResolvedIdentityWithoutAnySource stays empty rather than inventing a value.
func TestResolvedIdentityWithoutAnySource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	p, err := Create("per", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := p.ResolvedIdentity(); got.Email != "" {
		t.Errorf("Email = %q, want empty for a profile with no identity anywhere", got.Email)
	}
}

func TestSameAccountWarnsWithoutBlocking(t *testing.T) {
	profiles := []Profile{
		{Name: "per", Cached: config.Identity{Email: "a@x.com"}},
		{Name: "alt", Cached: config.Identity{Email: "a@x.com"}},
		{Name: "com", Cached: config.Identity{Email: "b@y.com"}},
		{Name: "new"},
	}
	dupes := SameAccount(profiles)
	if len(dupes) != 1 {
		t.Fatalf("SameAccount = %v, want one duplicated account", dupes)
	}
	if got := dupes["a@x.com"]; len(got) != 2 {
		t.Errorf("dupes[a@x.com] = %v, want two profiles", got)
	}
}

func TestRedactEmail(t *testing.T) {
	tests := map[string]string{
		"alice@example.com": "a***@example.com",
		"":                  "(none)",
		"not-an-email":      "***",
	}
	for in, want := range tests {
		if got := RedactEmail(in); got != want {
			t.Errorf("RedactEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func names(ps []Profile) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}
