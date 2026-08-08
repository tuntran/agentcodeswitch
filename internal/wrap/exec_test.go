package wrap

import (
	"slices"
	"strings"
	"testing"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

func mustLiteral(t *testing.T, s string) profile.ConfigDirLiteral {
	t.Helper()
	lit, err := profile.NewConfigDirLiteral(s)
	if err != nil {
		t.Fatalf("NewConfigDirLiteral(%q): %v", s, err)
	}
	return lit
}

// TestEnvironStripsAuthVars is the test behind "ANTHROPIC_API_KEY is set and
// `acs per` still uses the right profile".
//
// The Bedrock/Vertex/Foundry group matters as much as the key group: leave one in
// and claude talks to a different backend entirely while the switch looks like it
// worked.
func TestEnvironStripsAuthVars(t *testing.T) {
	lit := mustLiteral(t, "/Users/x/.acs/profiles/per")

	base := []string{
		"PATH=/usr/bin",
		"HOME=/Users/x",
		"CLAUDE_CONFIG_DIR=/somewhere/stale",
	}
	for _, v := range AuthVars {
		base = append(base, v+"=1")
	}

	got, err := Environ(base, lit, "")
	if err != nil {
		t.Fatalf("Environ: %v", err)
	}

	names := map[string]string{}
	for _, kv := range got {
		name, value, _ := strings.Cut(kv, "=")
		names[name] = value
	}
	for _, v := range AuthVars {
		if _, present := names[v]; present {
			t.Errorf("%s survived; it takes precedence over the profile credential", v)
		}
	}
	if names[ConfigDirVar] != lit.String() {
		t.Errorf("%s = %q, want %q", ConfigDirVar, names[ConfigDirVar], lit.String())
	}
	if names["PATH"] != "/usr/bin" || names["HOME"] != "/Users/x" {
		t.Error("unrelated variables were dropped")
	}
	// A stale inherited value must not remain alongside the new one.
	if count := countVar(got, ConfigDirVar); count != 1 {
		t.Errorf("%s appears %d times, want exactly 1", ConfigDirVar, count)
	}
}

func TestAuthVarsCoversEveryPrecedenceGroup(t *testing.T) {
	// Enumerated rather than counted: a future edit that drops one of these
	// silently reintroduces "the switch did nothing and said nothing".
	want := []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"ANTHROPIC_BASE_URL",
		"CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CODE_USE_FOUNDRY",
		"ANTHROPIC_BEDROCK_BASE_URL",
		"ANTHROPIC_VERTEX_PROJECT_ID",
	}
	for _, v := range want {
		if !slices.Contains(AuthVars, v) {
			t.Errorf("AuthVars is missing %s", v)
		}
	}
}

// TestEnvironModel pins the precedence rule between a profile's model and one
// inherited from the shell.
//
// The asymmetry is the point. A profile with a model owns the variable, so a
// stale inherited value cannot survive next to it. A profile without one owns
// nothing, so `export ANTHROPIC_MODEL=...` has to keep working through acs --
// dropping it would make acs silently undo a setting it was never told about.
func TestEnvironModel(t *testing.T) {
	lit := mustLiteral(t, "/Users/x/.acs/profiles/per")

	tests := []struct {
		name      string
		inherited string
		model     string
		want      string
	}{
		{"profile model, nothing inherited", "", "claude-opus-5", "claude-opus-5"},
		{"profile model wins over inherited", "claude-sonnet-5", "claude-opus-5", "claude-opus-5"},
		{"no profile model keeps the inherited one", "claude-sonnet-5", "", "claude-sonnet-5"},
		{"no model anywhere", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := []string{"PATH=/usr/bin"}
			if tt.inherited != "" {
				base = append(base, ModelVar+"="+tt.inherited)
			}
			got, err := Environ(base, lit, tt.model)
			if err != nil {
				t.Fatalf("Environ: %v", err)
			}

			var value string
			for _, kv := range got {
				if k, v, _ := strings.Cut(kv, "="); k == ModelVar {
					value = v
				}
			}
			if value != tt.want {
				t.Errorf("%s = %q, want %q", ModelVar, value, tt.want)
			}
			// Counted, not just read: "" as a value and "" as absent are
			// different environments, and reading the value alone cannot tell
			// them apart. Two of the same variable is also how a wrong one wins
			// by ordering.
			want := 1
			if tt.want == "" {
				want = 0
			}
			if n := countVar(got, ModelVar); n != want {
				t.Errorf("%s appears %d times, want %d", ModelVar, n, want)
			}
		})
	}
}

// The model decides which model answers, not which account pays. Stripping it the
// way AuthVars are stripped would break a shell-level default for every profile.
func TestModelVarIsNotAnAuthVar(t *testing.T) {
	if slices.Contains(AuthVars, ModelVar) {
		t.Errorf("%s is in AuthVars; it would be stripped even when no profile sets one", ModelVar)
	}
}

func TestEnvironRejectsZeroLiteral(t *testing.T) {
	var zero profile.ConfigDirLiteral
	if _, err := Environ([]string{"PATH=/usr/bin"}, zero, ""); err == nil {
		t.Error("Environ accepted the zero literal; claude would use the default config dir")
	}
}

func TestEnvironIgnoresMalformedEntries(t *testing.T) {
	lit := mustLiteral(t, "/Users/x/.acs/profiles/per")
	got, err := Environ([]string{"NOEQUALS", "OK=1"}, lit, "")
	if err != nil {
		t.Fatalf("Environ: %v", err)
	}
	if slices.Contains(got, "NOEQUALS") {
		t.Error("a malformed environment entry was passed through")
	}
}

func countVar(env []string, name string) int {
	n := 0
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k == name {
			n++
		}
	}
	return n
}
