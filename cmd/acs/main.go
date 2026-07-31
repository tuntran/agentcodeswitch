// Command acs switches between isolated Claude Code accounts.
//
// Grammar is positional: a known subcommand comes first, otherwise the first
// argument is a profile name and everything after it belongs to `claude`.
//
//	acs ls
//	acs per --dangerously-skip-permissions
//	acs per -- --help          # -- keeps acs from claiming the flag
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tuntran/agentcodeswitch/internal/profile"
	"github.com/tuntran/agentcodeswitch/internal/version"
	"github.com/tuntran/agentcodeswitch/internal/wrap"
)

// exitError carries a specific exit status. Commands that mean something by their
// status (doctor: 1 for a failed check, 2 for an unreadable config) return one.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "acs: "+err.Error())
	var ee exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}
	os.Exit(1)
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("acs " + version.Current)
		return nil
	case "help", "--help", "-h":
		usage(os.Stdout)
		return nil
	case "ls":
		return cmdLs(args[1:])
	case "add":
		return cmdAdd(args[1:])
	case "login":
		return cmdLogin(args[1:])
	case "link":
		return cmdLink(args[1:])
	case "rm":
		return cmdRm(args[1:])
	case "quota":
		return cmdQuota(args[1:])
	case "doctor":
		return cmdDoctor(args[1:])
	case "ui":
		return cmdUI(args[1:])
	default:
		return cmdExec(args)
	}
}

// parseFlags parses a subcommand's arguments, allowing flags to follow positional
// ones.
//
// Go's flag package stops at the first non-flag argument, so `acs rm per --purge`
// would leave --purge as a positional and silently skip the confirmation branch.
// The documented grammar puts the profile name first, so the arguments are
// reordered before parsing rather than the grammar being bent to suit the parser.
func parseFlags(fs *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			// Everything after an explicit terminator is positional.
			positional = append(positional, args[i+1:]...)
			i = len(args)
			continue
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			positional = append(positional, arg)
			continue
		}

		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue // value is inline
		}
		defined := fs.Lookup(name)
		if defined == nil {
			continue // let fs.Parse report the unknown flag
		}
		if boolFlag, ok := defined.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
			continue // takes no value
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	// The "--" is re-emitted rather than consumed. Dropping it and handing the rest
	// to flag.Parse turned the very arguments it was protecting back into flags:
	// parseFlags(["--", "--purge"]) used to set purge.
	//
	// Emitting it unconditionally also protects a positional that happens to look
	// like a flag, and costs nothing when there are none.
	return fs.Parse(append(append(flags, "--"), positional...))
}

// cmdExec launches `claude` for a profile.
//
// On success this never returns: wrap.Exec replaces the process so that signals,
// TTY ownership and the exit code all pass through to `claude` untouched.
func cmdExec(args []string) error {
	name := args[0]
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("unknown command %q (run `acs help`)", name)
	}
	if err := profile.ValidateName(name); err != nil {
		return err
	}
	p, err := profile.Get(name)
	if err != nil {
		if errors.Is(err, profile.ErrNotFound) {
			return fmt.Errorf("%w -- run `acs add %s`", err, name)
		}
		return err
	}

	// A single leading "--" is acs's own separator and is not forwarded, so
	// `acs per -- --help` asks claude for help instead of acs.
	rest := args[1:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	return wrap.Exec(p.Literal, rest)
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `acs `+version.Current+` -- switch between isolated Claude Code accounts

  acs <profile> [claude args...]   launch claude for a profile
  acs add <name> [--email addr]    create a profile and log in
  acs login <name> [--force]       re-authenticate a profile
  acs link <name> [--replace]      share skills/agents/hooks with ~/.claude
  acs ls [--json]                  list profiles with cached quota
  acs quota [--json] [--force]     fetch live quota
  acs doctor [--deep] [--json]     self-check; --deep reads secrets
  acs rm <name> [--purge]          remove a profile; --purge deletes history
  acs ui                           launch the desktop app
  acs version

Profile names use ASCII lowercase [a-z0-9_-]. Reserved: `+strings.Join(profile.Reserved(), " ")+`
`)
}
