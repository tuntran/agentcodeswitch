// Package login authenticates a profile by delegating to `claude auth login`.
//
// acs never creates a credential itself. Minting tokens with Claude Code's client
// ID is exactly what the February 2026 policy restricts, and writing the Keychain
// blob by hand risks overwriting a login that works. So the whole dependency
// surface here is two things: an exit code, and `claude auth status --json`.
package login

import (
	"encoding/json"
	"fmt"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

// Status is `claude auth status --json`.
//
// Confirmed on claude 2.1.220: loggedIn, authMethod, apiProvider, email, orgId,
// orgName, subscriptionType -- and no quota, which is why acs reads the usage
// endpoint itself.
type Status struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	APIProvider      string `json:"apiProvider"`
	Email            string `json:"email"`
	OrgID            string `json:"orgId"`
	OrgName          string `json:"orgName"`
	SubscriptionType string `json:"subscriptionType"`
}

// ParseStatus decodes the status document.
//
// Parsing is tolerant on purpose: Claude Code owns this shape and may add fields
// at any time. acs only needs loggedIn and email, so an unknown field is not an
// error and a missing optional one is not a crash. Note the absence of
// DisallowUnknownFields -- that is the point.
func ParseStatus(raw []byte) (Status, error) {
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return Status{}, fmt.Errorf("parse `claude auth status --json`: %w", err)
	}
	return s, nil
}

// Identity converts a status into the cacheable identity.
func (s Status) Identity() config.Identity {
	return config.Identity{
		Email:            s.Email,
		OrgID:            s.OrgID,
		OrgName:          s.OrgName,
		SubscriptionType: s.SubscriptionType,
	}
}
