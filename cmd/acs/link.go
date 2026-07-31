package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// cmdLink wires a profile's tooling to the default config dir.
//
// `acs add` does this automatically. This command exists for profiles created
// before it did, and for repairing links after ~/.claude moves.
func cmdLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ContinueOnError)
	replace := fs.Bool("replace", false,
		"move existing files aside (to <name>.acs-replaced-<time>) and link over them")
	all := fs.Bool("all", false, "link every profile")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	// Exactly one of the two forms: a single profile name, or --all with none.
	// Spelled out rather than folded into one boolean expression -- the clever
	// version read fine and was inverted.
	namedOne := fs.NArg() == 1 && !*all
	askedForAll := fs.NArg() == 0 && *all
	if !namedOne && !askedForAll {
		return errors.New("usage: acs link <name> [--replace]  |  acs link --all [--replace]")
	}

	var profiles []profile.Profile
	if *all {
		var err error
		if profiles, err = profile.List(); err != nil {
			return err
		}
	} else {
		p, err := profile.Get(fs.Arg(0))
		if err != nil {
			return err
		}
		profiles = []profile.Profile{p}
	}

	blocked := 0
	for _, p := range profiles {
		fmt.Printf("%s\n", p.Name)
		results, err := profile.LinkShared(p, *replace)
		if err != nil {
			return err
		}
		for _, r := range results {
			fmt.Printf("  %-14s %s\n", r.Name, r.Outcome)
			if r.MovedTo != "" {
				fmt.Printf("  %-14s previous content kept at %s\n", "", r.MovedTo)
			}
			if r.Outcome == profile.LinkBlocked {
				blocked++
			}
		}
		if err := profile.MarkOnboarded(p.Literal); err != nil {
			fmt.Printf("  %-14s could not mark onboarding complete: %v\n", "onboarding", err)
		}
	}

	if blocked > 0 {
		// Nothing was deleted, and nothing will be without being asked.
		fmt.Printf("\n%d asset(s) already hold real content and were left alone.\n", blocked)
		fmt.Println("Re-run with --replace to move them aside and link; the old content is kept.")
	}
	fmt.Println("\nShared tooling comes from " + profile.DefaultConfigDir() +
		". Identity and history stay per-profile.")
	return nil
}
