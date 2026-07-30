package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// cmdRm removes a profile.
//
// The directory survives by default. <dir>/projects/*.jsonl is the only copy of
// Claude Code's sessions and transcripts for that account, and nothing can
// regenerate it, so deleting it takes an explicit --purge plus retyping the name.
func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	// There is deliberately no --yes or --force here. The retype prompt is the only
	// thing standing between a typo and the permanent loss of an account's Claude
	// Code history, and a flag that skips it would be used by exactly the scripts
	// that should never have it.
	purge := fs.Bool("purge", false, "also delete the profile directory and its Claude Code history")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: acs rm <name> [--purge]")
	}
	name := fs.Arg(0)

	p, err := profile.Get(name)
	if err != nil {
		return err
	}
	cs := profile.DefaultCredStore()

	typed := ""
	if *purge {
		plan, err := profile.InspectPurge(p)
		if err != nil {
			return err
		}
		printPurgePlan(plan)
		if typed, err = promptName(name); err != nil {
			return err
		}
	}

	out, err := profile.Remove(cs, name, *purge, typed)
	if err != nil {
		return err
	}
	fmt.Printf("removed profile %q (keychain %s)\n", name, shortService(out.KeychainService))
	if out.Purged {
		fmt.Println("deleted " + p.Dir)
	} else {
		fmt.Println("history kept at " + out.HistoryKept)
	}
	return nil
}

// printPurgePlan states what is about to be lost, in files and dates, before
// asking. "Are you sure?" without a number is not a decision anyone can make.
func printPurgePlan(plan profile.PurgePlan) {
	fmt.Println("--purge will permanently delete " + plan.Dir)
	if plan.JSONLCount == 0 {
		fmt.Println("no Claude Code transcripts found there")
		return
	}
	fmt.Printf("including %d Claude Code transcript file(s)", plan.JSONLCount)
	if !plan.Oldest.IsZero() {
		fmt.Printf(", %s to %s",
			plan.Oldest.Format(time.DateOnly), plan.Newest.Format(time.DateOnly))
	}
	fmt.Println("\nThis is the only copy. Nothing can regenerate it.")
}

func promptName(name string) (string, error) {
	fmt.Printf("Retype %q to confirm: ", name)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read confirmation: %w", err)
	}
	return strings.TrimSpace(line), nil
}
