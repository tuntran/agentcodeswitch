package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Adding an account from the UI goes through Terminal.app rather than calling
// internal/login directly.
//
// internal/login sets Stdin/Stdout/Stderr to the process's own. A Wails .app
// launched from Finder has stdin on /dev/null and no TTY, so an interactive
// `claude auth login` would hang or fail quietly -- the "Add account" button would
// sit at "waiting for authentication" forever. Handing the job to a real terminal
// is the honest version, and it reuses the exact CLI path a terminal user takes.

// resolveACSBinary finds the acs CLI to run in the terminal.
//
// The bundled Resources copy is checked before PATH so a .app that ships its own
// CLI does not silently drive some other version the user happens to have.
func resolveACSBinary() (string, error) {
	var candidates []string
	if fromEnv := os.Getenv("ACS_BIN"); fromEnv != "" {
		candidates = append(candidates, fromEnv)
	}
	if exe, err := os.Executable(); err == nil {
		// <bundle>/Contents/MacOS/acs -> <bundle>/Contents/Resources/acs
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "..", "Resources", "acs"),
			filepath.Join(dir, "acs-cli"))
	}
	candidates = append(candidates, "/usr/local/bin/acs", "/opt/homebrew/bin/acs")

	for _, candidate := range candidates {
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(resolved); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return resolved, nil
		}
	}
	if fromPath, err := exec.LookPath("acs"); err == nil {
		return fromPath, nil
	}
	return "", errors.New(
		"the acs command-line tool could not be found. Build it with `go build -o /usr/local/bin/acs ./cmd/acs`, " +
			"or set ACS_BIN to its path, then try again")
}

// launchLoginTerminal opens Terminal.app running `acs login <name>`.
func launchLoginTerminal(name string) error {
	return runACSInTerminal("login", name)
}

// launchProfileTerminal opens Terminal.app running `acs <name>`, which is what makes
// Claude Code start under that profile -- and refresh its own access token on the way
// up. That is why an expired token offers this rather than a fresh login.
func launchProfileTerminal(name string) error {
	return runACSInTerminal("", name)
}

// runACSInTerminal runs `acs [subcommand] <name>` in a new Terminal window. An empty
// subcommand means the bare `acs <name>` form.
func runACSInTerminal(subcommand, name string) error {
	bin, err := resolveACSBinary()
	if err != nil {
		return err
	}
	command := shellQuote(bin)
	if subcommand != "" {
		command += " " + subcommand
	}
	command += " " + shellQuote(name)

	script := fmt.Sprintf(
		`tell application "Terminal"
	activate
	do script %s
end tell`, appleScriptString(command))

	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open Terminal: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// shellQuote wraps a value in single quotes for the shell Terminal.app runs.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScriptString renders a Go string as an AppleScript literal.
func appleScriptString(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
