package login

import (
	"errors"
	"fmt"
	"slices"

	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// Result reports what an authentication run produced.
type Result struct {
	Profile         profile.Profile
	Status          Status
	KeychainService string
	// Warnings are anomalies that do not fail the login but that the user needs
	// to see -- above all, a Keychain item appearing under an unexpected name,
	// which means the frozen literal is wrong.
	Warnings []string
	// Links reports the shared tooling wired up for a new profile, empty for a
	// re-login of an existing one.
	Links []profile.LinkResult
}

// ErrNotLoggedIn means `claude auth login` exited zero but nobody is authenticated.
var ErrNotLoggedIn = errors.New("claude reports nobody logged in")

// Add creates a profile and authenticates it.
//
// Order matters: the config dir literal is frozen and written before `claude` ever
// sees it. Writing it afterwards leaves a window in which the string acs recorded
// and the string Claude Code hashed into the Keychain name are not the same, and
// that mismatch presents as a random logout days later.
func Add(cs profile.CredStore, name, label, email string) (Result, error) {
	p, err := profile.Create(name, label)
	if err != nil {
		return Result{}, err
	}

	// Both of these run BEFORE the login, and the order matters.
	//
	// CLAUDE_CONFIG_DIR moves the entire config dir, so without the links the new
	// profile has no skills, agents, hooks or settings -- a bare Claude Code.
	// Linking first also means the settings.json that `claude auth login` creates
	// writes through the symlink instead of becoming a competing real file.
	//
	// Marking onboarding first likewise avoids a first run that opens the wizard and
	// looks like the login failed.
	links, linkErr := profile.LinkShared(p, false)
	if linkErr == nil {
		linkErr = profile.MarkOnboarded(p.Literal)
	}

	res, err := authenticate(cs, p, email)
	if err != nil {
		// Roll back only what this command created. DiscardNew leaves the Keychain
		// untouched on purpose.
		if rbErr := profile.DiscardNew(name); rbErr != nil {
			return Result{}, fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return Result{}, err
	}
	// Reported rather than fatal: an authenticated profile with no shared tooling is
	// usable, just stripped down, and failing the add would throw away a completed
	// login over a symlink. `acs doctor` repeats this, and `acs link` fixes it.
	if linkErr != nil {
		res.Warnings = append(res.Warnings,
			"shared tooling was not linked: "+linkErr.Error()+" -- run `acs link "+name+"`")
	}
	res.Links = links
	return res, nil
}

// Login re-authenticates an existing profile.
//
// A failure here rolls back nothing: the previous credential and identity are
// still valid, and discarding them because a retry was cancelled would take away
// a working setup.
//
// force logs out first. Whether plain login is enough depends on what `claude auth
// login` does against an already-authenticated config dir -- it may re-login, it
// may insist on a logout, it may no-op. Rather than guess, acs runs the plain
// command (whose own message is visible, since stdio is inherited) and offers
// --force as the escape hatch for the other two cases.
func Login(cs profile.CredStore, name, email string, force bool) (Result, error) {
	p, err := profile.Get(name)
	if err != nil {
		return Result{}, err
	}
	if force {
		if err := Logout(p.Literal); err != nil {
			return Result{}, err
		}
	}
	return authenticate(cs, p, email)
}

// authenticate runs the login, verifies it, and checks what appeared in the store.
func authenticate(cs profile.CredStore, p profile.Profile, email string) (Result, error) {
	before, listErr := cs.List()

	if err := Run(p.Literal, email); err != nil {
		return Result{}, err
	}

	status, err := Verify(p.Literal)
	if err != nil {
		return Result{}, err
	}
	if !status.LoggedIn {
		return Result{}, ErrNotLoggedIn
	}
	if err := profile.SaveIdentity(p.Name, status.Identity()); err != nil {
		return Result{}, err
	}

	res := Result{
		Profile:         p,
		Status:          status,
		KeychainService: cs.ServiceName(p.Literal),
	}
	res.Warnings = checkStore(cs, p, res.KeychainService, before, listErr)
	return res, nil
}

// checkStore compares the credential store before and after the login.
//
// The case worth catching is a new item under a name that is not
// ServiceName(literal): the login worked, but acs will look for the credential
// somewhere else forever. That fails silently and presents as a logout, so it is
// worth a loud warning even though the login itself succeeded.
func checkStore(cs profile.CredStore, p profile.Profile, want string, before []string, beforeErr error) []string {
	var warnings []string
	if beforeErr != nil {
		warnings = append(warnings,
			"could not read the keychain before logging in, so the credential name was not verified: "+beforeErr.Error())
	}

	exists, err := cs.Exists(want)
	switch {
	case err != nil:
		warnings = append(warnings, "could not confirm the credential exists: "+err.Error())
	case !exists:
		warnings = append(warnings, fmt.Sprintf(
			"logged in, but no credential named %s exists -- run `acs doctor`", want))
	}

	after, err := cs.List()
	if err != nil {
		return warnings
	}
	var added []string
	for _, svc := range after {
		if !slices.Contains(before, svc) {
			added = append(added, svc)
		}
	}
	for _, svc := range added {
		if svc != want {
			warnings = append(warnings, fmt.Sprintf(
				"a credential appeared under %s but %s expects %s -- the frozen config dir literal (%s) may be wrong. Run `acs doctor`.",
				svc, p.Name, want, p.Literal.String()))
		}
	}
	if len(added) > 1 {
		warnings = append(warnings, fmt.Sprintf(
			"%d new credentials appeared for one login -- run `acs doctor`", len(added)))
	}
	return warnings
}
