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

	got, err := Environ(base, lit)
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

func TestEnvironRejectsZeroLiteral(t *testing.T) {
	var zero profile.ConfigDirLiteral
	if _, err := Environ([]string{"PATH=/usr/bin"}, zero); err == nil {
		t.Error("Environ accepted the zero literal; claude would use the default config dir")
	}
}

func TestEnvironIgnoresMalformedEntries(t *testing.T) {
	lit := mustLiteral(t, "/Users/x/.acs/profiles/per")
	got, err := Environ([]string{"NOEQUALS", "OK=1"}, lit)
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
