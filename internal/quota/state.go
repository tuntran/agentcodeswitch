// Package quota reads per-account usage from Claude's OAuth usage endpoint.
//
// The governing rule: never render 0% when the number is unknown. Absence is data,
// not zero.
//
// That is not pedantry. The endpoint returns 200 with an empty body when the token
// lacks the user:profile scope, so a naive client shows a healthy account as 0%
// used -- and 0% is exactly the number that makes someone kick off a long task
// right before they get blocked. The failure mode is not just missing information,
// it actively pushes the decision the wrong way.
package quota

import "fmt"

// State says what acs knows about a profile's quota.
//
// Collapsing these into one "error" would take away the ability to self-diagnose:
// three of the four failures have different fixes, and one is not a problem at all.
type State string

const (
	// StateOK means the numbers are real.
	StateOK State = "ok"
	// StateNoLogin means there is no credential yet.
	StateNoLogin State = "no_login"
	// StateMissingScope means the credential works but cannot read usage. Almost
	// always a --console or setup-token login.
	StateMissingScope State = "missing_scope"
	// StateExpired means the access token is past its expiry. acs does not refresh
	// it: a bad writeback can break a session that is running fine.
	StateExpired State = "expired"
	// StateUnavailable means the endpoint could not be reached or did not answer
	// usefully. It is undocumented, so this is expected to happen eventually.
	StateUnavailable State = "unavailable"
)

// Message explains a state and says what to do about it.
//
// Every non-OK state answers "what do I do now?". A bare "Error" is a dead end.
func Message(state State, profileName string, detail string) string {
	switch state {
	case StateOK:
		return "quota is current"
	case StateNoLogin:
		return fmt.Sprintf("not logged in -- run `acs login %s`", profileName)
	case StateMissingScope:
		return fmt.Sprintf(
			"logged in, but this credential cannot read usage -- run `acs login %s` and choose the Claude subscription, not the Console",
			profileName)
	case StateExpired:
		return fmt.Sprintf(
			"access token expired -- run `acs %s` (Claude Code refreshes on start) or `acs login %s`",
			profileName, profileName)
	case StateUnavailable:
		if detail != "" {
			return "quota unavailable: " + detail
		}
		return "quota unavailable -- the usage endpoint did not answer"
	default:
		return "unknown state " + string(state)
	}
}

// Blocks reports whether this state also prevents launching claude.
//
// Only a missing credential does. Everything else is an information failure, and
// an information failure must never stop someone from switching account -- that is
// the feature that works even when the undocumented endpoint does not.
func Blocks(state State) bool { return state == StateNoLogin }
