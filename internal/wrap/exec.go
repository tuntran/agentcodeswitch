// Package wrap launches `claude` under a profile's config dir.
package wrap

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// ConfigDirVar is the variable Claude Code reads to pick its config dir, and
// whose literal value it hashes into the Keychain service name.
const ConfigDirVar = "CLAUDE_CONFIG_DIR"

// ModelVar is the variable Claude Code reads for its default model.
//
// It is not an AuthVar and must not join that list: those are stripped
// unconditionally because they decide which account pays, while this one only
// decides which model answers. A `--model` flag on the command line still wins
// over it, so `acs per --model sonnet` keeps working without acs doing anything.
const ModelVar = "ANTHROPIC_MODEL"

// AuthVars take precedence over stored credentials, so an inherited one silently
// defeats the whole point of switching profiles.
//
// The Bedrock/Vertex/Foundry group matters as much as the API key group: leave it
// in and `acs per` runs against a completely different backend while looking like
// it worked. That silent-wrong-backend case is exactly what this list prevents.
var AuthVars = []string{
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

// Environ builds the environment for a `claude` run under a profile: the caller's
// environment with AuthVars removed and ConfigDirVar set to the frozen literal.
//
// Shared with internal/login so a login and a later launch cannot disagree about
// which environment a profile means.
//
// model is the profile's default, or "" for none.
func Environ(base []string, lit profile.ConfigDirLiteral, model string) ([]string, error) {
	if lit.IsZero() {
		return nil, fmt.Errorf("%s: no config dir literal", ConfigDirVar)
	}
	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if name == ConfigDirVar || slices.Contains(AuthVars, name) {
			continue
		}
		// An inherited model is dropped only when the profile names one of its
		// own. A profile without a model has to keep behaving like plain
		// `claude`, so an ANTHROPIC_MODEL exported in the shell still applies --
		// stripping it always would make acs silently undo a setting it was
		// never asked about.
		if name == ModelVar && model != "" {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, ConfigDirVar+"="+lit.String())
	if model != "" {
		out = append(out, ModelVar+"="+model)
	}
	return out, nil
}

// Exec REPLACES the current process with `claude`.
//
// CLI ONLY. Calling this from the Wails app kills the app: there is no return
// from a successful execve. `go list` cannot detect a UI-to-internal call
// because that direction is legitimate everywhere else, so this comment is the
// only guard there is.
//
// Replacing the process rather than spawning a child is deliberate: signals,
// TTY ownership and the exit code then pass through untouched, so Ctrl-C reaches
// `claude` and its exit status reaches the shell. exec.Command plus Wait would
// have to re-implement all three, badly.
func Exec(lit profile.ConfigDirLiteral, model string, args []string) error {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found on PATH: %w", err)
	}
	env, err := Environ(os.Environ(), lit, model)
	if err != nil {
		return err
	}
	argv := append([]string{"claude"}, args...)
	// Returns only on failure.
	return syscall.Exec(bin, argv, env)
}
