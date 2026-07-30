package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/tuntran/agentcodeswitch/internal/login"
	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// cmdAdd creates a profile and logs it in.
func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	email := fs.String("email", "", "pre-fill this address on the login page")
	label := fs.String("label", "", "human-readable label, e.g. \"Personal\"")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: acs add <name> [--email addr] [--label text]")
	}
	name := fs.Arg(0)

	fmt.Printf("Creating profile %q. A browser window will open for the login.\n", name)
	if *email == "" {
		fmt.Println("Tip: if the browser is already signed in to another account," +
			" cancel and retry with --email <addr>, or use a private window.")
	}

	res, err := login.Add(profile.DefaultCredStore(), name, *label, *email)
	if err != nil {
		if errors.Is(err, login.ErrCancelled) {
			return fmt.Errorf("%w -- profile %q was not created", err, name)
		}
		return err
	}
	reportLogin(res)
	return nil
}

// cmdLogin re-authenticates an existing profile.
func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	email := fs.String("email", "", "pre-fill this address on the login page")
	force := fs.Bool("force", false, "log out first, for when claude refuses to re-login")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: acs login <name> [--email addr] [--force]")
	}

	res, err := login.Login(profile.DefaultCredStore(), fs.Arg(0), *email, *force)
	if err != nil {
		if errors.Is(err, login.ErrCancelled) {
			// Nothing was rolled back: the previous credential is still valid.
			return fmt.Errorf("%w -- profile %q is unchanged", err, fs.Arg(0))
		}
		return err
	}
	reportLogin(res)
	return nil
}

func reportLogin(res login.Result) {
	fmt.Printf("\nprofile   %s\n", res.Profile.Name)
	fmt.Printf("account   %s\n", dash(res.Status.Email))
	fmt.Printf("plan      %s\n", dash(res.Status.SubscriptionType))
	if res.Status.OrgName != "" {
		fmt.Printf("org       %s\n", res.Status.OrgName)
	}
	fmt.Printf("keychain  %s\n", shortService(res.KeychainService))
	fmt.Printf("config    %s\n", res.Profile.Literal.String())

	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning: "+w)
	}
	warnSharedAccount(res.Profile.Name)
	fmt.Printf("\nrun `acs %s` to start claude with this account\n", res.Profile.Name)
}

// warnSharedAccount notes when two profiles resolve to the same account.
//
// Worth saying, never worth blocking: two config dirs on one account is a
// legitimate setup, for instance different settings per project.
func warnSharedAccount(name string) {
	profiles, err := profile.List()
	if err != nil {
		return
	}
	for email, names := range profile.SameAccount(profiles) {
		if !slices.Contains(names, name) {
			continue
		}
		fmt.Fprintf(os.Stderr,
			"note: profiles %v all use %s. That is allowed; they still have separate credentials.\n",
			names, email)
	}
}
