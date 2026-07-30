package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/config"
	"github.com/tuntran/agentcodeswitch/internal/profile"
	"github.com/tuntran/agentcodeswitch/internal/quota"
	"github.com/tuntran/agentcodeswitch/internal/version"
)

// cmdQuota fetches live quota for every profile.
//
// Exit status is 0 even when individual profiles fail: a quota that cannot be read
// is information acs is reporting, not a failure of the command. Only an unreadable
// config exits 1.
func cmdQuota(args []string) error {
	fs := flag.NewFlagSet("quota", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	force := fs.Bool("force", false, "ignore the cache and any backoff")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	// Checked separately from List so a broken config exits 1 while a single
	// broken profile does not.
	if _, err := config.Load(); err != nil {
		return exitError{code: 1, err: err}
	}
	profiles, listErr := profile.List()
	if listErr != nil {
		fmt.Fprintln(os.Stderr, "acs: "+listErr.Error())
	}

	reader := quota.NewReader(profile.DefaultCredStore(), version.Current)
	results := reader.GetAll(context.Background(), profiles, *force)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(quota.Document{SchemaVersion: quota.SchemaVersion, Profiles: results})
	}
	printQuota(results)
	return nil
}

func printQuota(results []quota.Result) {
	if len(results) == 0 {
		fmt.Println("no profiles yet -- run `acs add <name>`")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(tw, "PROFILE\t5H\tRESETS\tWEEKLY\tRESETS\t")
	for _, r := range results {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t\n",
			r.Profile,
			windowPercent(r.FiveHour), windowReset(r.FiveHour),
			windowPercent(r.SevenDay), windowReset(r.SevenDay))
	}
	_ = tw.Flush()

	for _, r := range results {
		if r.State != quota.StateOK {
			fmt.Printf("\n%s: %s\n", r.Profile, r.Message)
			// The previous reading is printed here, under its own label, rather
			// than in the table above. In the table it would sit in the same column
			// as a live figure and read as current.
			if r.LastKnown != nil {
				fmt.Printf("  last known %s: 5h %s, weekly %s\n",
					r.LastKnown.FetchedAt.Local().Format(time.TimeOnly),
					windowPercent(r.LastKnown.FiveHour),
					windowPercent(r.LastKnown.SevenDay))
			}
			continue
		}
		if r.Stale {
			fmt.Printf("\n%s: cached numbers from %s, refresh has not succeeded since\n",
				r.Profile, r.FetchedAt.Local().Format(time.TimeOnly))
		}
	}
}

// windowPercent renders a utilisation figure.
//
// A nil window prints an em dash. This is the single most important line in the
// CLI: turning "unknown" into "0%" would tell someone they have their whole window
// left, right before it blocks them.
func windowPercent(w *quota.Window) string {
	if w == nil {
		return "—"
	}
	out := strconv.Itoa(w.Utilization) + "%"
	if w.Severity == quota.SeverityAttention {
		out += " !"
	}
	return out
}

func windowReset(w *quota.Window) string {
	if w == nil || w.ResetsAt == nil {
		return "—"
	}
	return w.ResetsAt.Local().Format("Jan 2 15:04")
}

// cachedQuotaLabel is the compact column `acs ls` shows. Cache only: no network
// call, no secret read.
func cachedQuotaLabel(profileName string) string {
	r := quota.Cached(profileName, time.Now())
	if r.State != quota.StateOK {
		return shortState(r.State)
	}
	label := "5h " + windowPercent(r.FiveHour) + "  wk " + windowPercent(r.SevenDay)
	if r.Stale {
		// Without this the age of the number is invisible, and `acs ls` shows a
		// figure from hours ago exactly like one from a minute ago.
		label += "  (stale)"
	}
	return label
}

func shortState(s quota.State) string {
	switch s {
	case quota.StateNoLogin:
		return "not logged in"
	case quota.StateMissingScope:
		return "no usage scope"
	case quota.StateExpired:
		return "token expired"
	default:
		return "—"
	}
}
