package login

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/tuntran/agentcodeswitch/internal/profile"
	"github.com/tuntran/agentcodeswitch/internal/wrap"
)

// claudeBinary is the command acs delegates authentication to. Tests replace it
// with a script that fakes exit codes and status output.
var claudeBinary = "claude"

// ErrCancelled means `claude auth login` exited non-zero: the user pressed Ctrl-C,
// closed the browser, or the login failed.
var ErrCancelled = errors.New("login cancelled")

// command builds a `claude` invocation for one profile.
//
// The environment comes from wrap.Environ, the same function `acs <profile>` uses,
// so a login and every later launch cannot disagree about which credential a
// profile means.
func command(lit profile.ConfigDirLiteral, args ...string) (*exec.Cmd, error) {
	bin, err := exec.LookPath(claudeBinary)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w", claudeBinary, err)
	}
	env, err := wrap.Environ(os.Environ(), lit)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...) // #nosec G204 -- delegating to claude is the point
	cmd.Env = env
	return cmd, nil
}

// Run performs the interactive login.
//
// --claudeai is not configurable. --console authenticates for API billing, which
// produces a token without the user:profile scope, which makes the usage endpoint
// answer 200 with an empty body. That would show a healthy account as 0% quota --
// the one number that makes someone start a big task right before being blocked.
//
// exec.Command with inherited stdio, not syscall.Exec: acs has to keep running
// afterwards to verify the result and check the Keychain. There is deliberately no
// timeout, because a browser login legitimately takes minutes.
func Run(lit profile.ConfigDirLiteral, email string) error {
	args := []string{"auth", "login", "--claudeai"}
	if email != "" {
		// Solves the "second account logs in as the first" problem: the browser
		// already has a session, and this pre-fills the other address.
		args = append(args, "--email", email)
	}
	cmd, err := command(lit, args...)
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("%w (claude auth login exited %d)", ErrCancelled, ee.ExitCode())
		}
		return fmt.Errorf("run claude auth login: %w", err)
	}
	return nil
}

// Verify asks Claude Code who is now logged in for this config dir.
//
// This is the only success signal acs trusts. Nothing parses login stdout: the
// wording is Claude Code's to change, and scraping it would break on a release
// note.
func Verify(lit profile.ConfigDirLiteral) (Status, error) {
	cmd, err := command(lit, "auth", "status", "--json")
	if err != nil {
		return Status{}, err
	}
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return Status{}, fmt.Errorf("run claude auth status: %w", err)
	}
	return ParseStatus(out)
}

// Logout signs a profile out. Used by `acs login --force`, whose behaviour depends
// on what `claude auth login` does on an already-authenticated dir.
func Logout(lit profile.ConfigDirLiteral) error {
	cmd, err := command(lit, "auth", "logout")
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run claude auth logout: %w", err)
	}
	return nil
}
