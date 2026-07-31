// Package doctor self-checks an acs installation.
//
// It exists because the three load-bearing failure modes of acs all fail silently
// with symptoms that point somewhere else. A drifted config dir literal shows up
// as "I got randomly logged out", which sends you debugging OAuth for a day.
// Doctor is the only thing that names the actual cause.
//
// Doctor never repairs anything. Rewriting a drifted literal would make the old
// credential unreachable with nobody noticing, which is worse than the report.
package doctor

import (
	"slices"

	"github.com/tuntran/agentcodeswitch/internal/config"
	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// Status is the outcome of one check.
type Status string

const (
	// StatusOK means the invariant holds.
	StatusOK Status = "ok"
	// StatusFail means something is broken and acs will misbehave.
	StatusFail Status = "fail"
	// StatusWarn means suspicious but not broken.
	StatusWarn Status = "warn"
)

// Check is one verified invariant.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	// Action is what the user should do about it. Required whenever Status is not
	// StatusOK: a report that says only "fail" takes away the ability to fix it.
	Action string `json:"action,omitempty"`
}

// ProfileReport groups the checks for one profile.
type ProfileReport struct {
	Name   string  `json:"name"`
	Checks []Check `json:"checks"`
}

// Report is the whole doctor run.
type Report struct {
	// Deep records whether secrets were read, so a reader can tell a missing
	// scope check from a scope check that was skipped.
	Deep     bool            `json:"deep"`
	Profiles []ProfileReport `json:"profiles"`
	// Orphans are stored Claude Code credentials no acs profile accounts for.
	//
	// Informational, never a failure. A drifted literal does produce one, but so
	// does any other tool that sets CLAUDE_CONFIG_DIR -- and acs cannot tell the
	// two apart from a service name. Failing the report on that would mean doctor
	// is permanently red on a machine that also uses one of those tools.
	Orphans []string `json:"orphans"`
	// ConfigError is set when the config itself could not be read, which is the
	// one condition that makes every other check meaningless.
	ConfigError string `json:"configError,omitempty"`
}

// OK reports whether every check passed. Warnings and orphans do not fail the
// report; a failed check does.
func (r Report) OK() bool {
	if r.ConfigError != "" {
		return false
	}
	for _, p := range r.Profiles {
		for _, c := range p.Checks {
			if c.Status == StatusFail {
				return false
			}
		}
	}
	return true
}

// Run checks every profile.
//
// With deep set it also reads each credential, which makes macOS prompt the first
// time. That is why it is opt-in: plain `acs doctor` promises to stay silent, and
// a self-check that pops a dialog is a self-check people stop running.
func Run(cs profile.CredStore, deep bool) Report {
	// Both slices start empty rather than nil so `--json` emits [] instead of null.
	// A consumer doing `.orphans.length` should not have to special-case an empty
	// installation.
	r := Report{Deep: deep, Profiles: []ProfileReport{}, Orphans: []string{}}

	f, err := config.Load()
	if err != nil {
		r.ConfigError = err.Error()
		return r
	}

	names := make([]string, 0, len(f.Profiles))
	for name := range f.Profiles {
		names = append(names, name)
	}
	slices.Sort(names)

	var known []string
	for _, name := range names {
		pr := ProfileReport{Name: name}
		lit, litCheck := checkLiteral(name, f.Profiles[name])
		pr.Checks = append(pr.Checks, litCheck)

		if litCheck.Status == StatusFail {
			// Every remaining check keys off the literal; running them against a
			// broken one would produce confident nonsense.
			r.Profiles = append(r.Profiles, pr)
			continue
		}
		known = append(known, cs.ServiceName(lit))
		p, resolveErr := profile.Get(name)
		pr.Checks = append(pr.Checks,
			checkKeychain(cs, name, lit),
			checkIdentity(name, lit, f.Profiles[name]),
			checkOnboarding(name, lit))
		if resolveErr == nil {
			pr.Checks = append(pr.Checks, checkTooling(name, p))
		}
		if deep {
			pr.Checks = append(pr.Checks, deepChecks(cs, name, lit)...)
		}
		r.Profiles = append(r.Profiles, pr)
	}

	// With no profiles configured, acs manages nothing and so cannot call anything
	// orphaned -- every stored credential belongs to something else.
	//
	// Keyed on configured profiles, not on len(known): a profile whose literal is
	// broken contributes no service name, and that is precisely the case where the
	// unclaimed credential is the one it lost.
	if len(names) == 0 {
		return r
	}
	if orphans, err := profile.Orphans(cs, known); err == nil {
		r.Orphans = orphans
	} else {
		r.Profiles = append(r.Profiles, ProfileReport{
			Name: "(keychain)",
			Checks: []Check{{
				Name: "orphan scan", Status: StatusWarn,
				Detail: err.Error(),
				Action: "check that the login keychain is unlocked",
			}},
		})
	}
	return r
}
