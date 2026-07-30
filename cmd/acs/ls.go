package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// lsRow is the JSON shape of `acs ls --json`.
type lsRow struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Email    string `json:"email,omitempty"`
	Plan     string `json:"plan,omitempty"`
	Keychain string `json:"keychain"`
	LoggedIn bool   `json:"loggedIn"`
	Quota    string `json:"quota"`
}

// cmdLs lists profiles.
//
// It must never touch the network or read a secret: this is the command people
// run constantly, and one that pops a Keychain dialog is one they stop running.
// Quota comes from cache only, and shows an em dash rather than 0% when there is
// nothing cached.
func cmdLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	profiles, err := profile.List()
	if err != nil {
		// Report broken entries but still list the healthy ones.
		fmt.Fprintln(os.Stderr, "acs: "+err.Error())
	}
	cs := profile.DefaultCredStore()

	rows := make([]lsRow, 0, len(profiles))
	for _, p := range profiles {
		service := cs.ServiceName(p.Literal)
		// A lookup failure is not the same as "no credential": reporting a locked
		// keychain as not-logged-in would send the user to log in again for no
		// reason, and risks a second credential for one account.
		exists, err := cs.Exists(service)
		if err != nil {
			fmt.Fprintf(os.Stderr, "acs: could not read the keychain for %q: %v\n", p.Name, err)
		}
		id := p.ResolvedIdentity()
		rows = append(rows, lsRow{
			Name:     p.Name,
			Label:    p.Label,
			Email:    id.Email,
			Plan:     id.SubscriptionType,
			Keychain: shortService(service),
			LoggedIn: exists,
			Quota:    cachedQuotaLabel(p.Name),
		})
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"profiles": rows})
	}
	printLs(rows)
	return nil
}

func printLs(rows []lsRow) {
	if len(rows) == 0 {
		fmt.Println("no profiles yet -- run `acs add <name>`")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PROFILE\tLABEL\tACCOUNT\tPLAN\tKEYCHAIN\tQUOTA")
	for _, r := range rows {
		state := r.Keychain
		if !r.LoggedIn {
			state = "not logged in"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Name, dash(r.Label), dash(r.Email), dash(r.Plan), state, r.Quota)
	}
	_ = tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func shortService(service string) string {
	if len(service) > len(profile.ServicePrefix)+1 {
		return service[len(profile.ServicePrefix)+1:]
	}
	return service
}
