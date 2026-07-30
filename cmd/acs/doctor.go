package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/tuntran/agentcodeswitch/internal/doctor"
	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// cmdDoctor self-checks the installation.
//
// Exit status is part of the contract so scripts can use it: 0 when everything
// passes, 1 when a check failed, 2 when the config itself could not be read.
func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	deep := fs.Bool("deep", false, "also read credentials (macOS will prompt the first time)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	report := doctor.Run(profile.DefaultCredStore(), *deep)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		printDoctor(report)
	}

	switch {
	case report.ConfigError != "":
		return exitError{code: 2, err: errors.New("config could not be read")}
	case !report.OK():
		return exitError{code: 1, err: errors.New("doctor found problems")}
	}
	return nil
}

func printDoctor(r doctor.Report) {
	if r.ConfigError != "" {
		fmt.Fprintln(os.Stderr, "config: "+r.ConfigError)
		return
	}
	if len(r.Profiles) == 0 {
		fmt.Println("no profiles yet -- run `acs add <name>`")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, p := range r.Profiles {
		for i, c := range p.Checks {
			name := p.Name
			if i > 0 {
				name = "" // group visually under the profile
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s %s\t%s\n", name, mark(c.Status), c.Name, c.Detail)
		}
	}
	_ = tw.Flush()

	// Actions print after the table so they are not squeezed into a column.
	// Every failure has to answer "what do I do now?" -- a report that only says
	// "fail" takes away the ability to fix it.
	for _, p := range r.Profiles {
		for _, c := range p.Checks {
			if c.Status != doctor.StatusOK && c.Action != "" {
				fmt.Printf("\n%s %s: %s\n  -> %s\n", mark(c.Status), p.Name, c.Detail, c.Action)
			}
		}
	}

	if len(r.Orphans) > 0 {
		// Informational: another tool that sets CLAUDE_CONFIG_DIR produces items
		// that look identical to a drifted acs literal, and acs cannot tell them
		// apart. Say so rather than implying something is broken.
		fmt.Printf("\nkeychain items no acs profile claims: %d\n", len(r.Orphans))
		for _, o := range r.Orphans {
			fmt.Println("  " + o)
		}
		fmt.Println("  These belong to another tool, or to a profile whose config dir literal changed." +
			"\n  If a profile above shows a literal or keychain failure, this is the credential it lost." +
			"\n  Otherwise leave them alone.")
	}
	if !r.Deep {
		fmt.Println("\nrun `acs doctor --deep` to check scopes and expiry (reads secrets)")
	}
}

func mark(s doctor.Status) string {
	switch s {
	case doctor.StatusOK:
		return "✓"
	case doctor.StatusWarn:
		return "!"
	default:
		return "✗"
	}
}
