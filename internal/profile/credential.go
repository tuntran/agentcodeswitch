package profile

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// ScopeProfile is the scope that makes the usage endpoint return real numbers.
//
// A token without it still works for inference, so the account looks perfectly
// healthy -- the endpoint just answers 200 with an empty body. That is why
// `claude setup-token` and `claude auth login --console` are unusable for quota.
const ScopeProfile = "user:profile"

// Credential is Claude Code's stored OAuth blob.
//
// The access token is held in memory for the duration of one request and is never
// written to the cache, a log, or an error message.
type Credential struct {
	AccessToken      string
	Scopes           []string
	ExpiresAt        time.Time
	SubscriptionType string
}

// oauthBlob mirrors the Keychain payload:
//
//	{"claudeAiOauth":{"accessToken":…,"refreshToken":…,"expiresAt":…,
//	                  "scopes":[…],"subscriptionType":"max","rateLimitTier":…}}
//
// refreshToken is intentionally not decoded: acs is read-only on credentials
// (a bad writeback can destroy a session that is working fine), so there is no
// reason to hold it.
type oauthBlob struct {
	ClaudeAiOAuth *struct {
		AccessToken      string   `json:"accessToken"`
		ExpiresAt        *float64 `json:"expiresAt"`
		Scopes           []string `json:"scopes"`
		SubscriptionType string   `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// ReadCredential reads and parses a profile's stored credential.
//
// This is the prompting path: only `acs quota` and `acs doctor --deep` may call
// it. Everything else uses Exists, which reads attributes only.
func ReadCredential(cs CredStore, lit ConfigDirLiteral) (Credential, error) {
	raw, err := cs.ReadSecret(cs.ServiceName(lit))
	if err != nil {
		return Credential{}, err
	}
	var blob oauthBlob
	if err := json.Unmarshal(raw, &blob); err != nil {
		// The payload is not echoed: it contains the token.
		return Credential{}, fmt.Errorf("keychain payload is not a Claude Code credential: %w", err)
	}
	if blob.ClaudeAiOAuth == nil || blob.ClaudeAiOAuth.AccessToken == "" {
		return Credential{}, fmt.Errorf("keychain payload has no claudeAiOauth access token")
	}
	return Credential{
		AccessToken:      blob.ClaudeAiOAuth.AccessToken,
		Scopes:           blob.ClaudeAiOAuth.Scopes,
		ExpiresAt:        epochToTime(blob.ClaudeAiOAuth.ExpiresAt),
		SubscriptionType: blob.ClaudeAiOAuth.SubscriptionType,
	}, nil
}

// HasScope reports whether the credential carries a scope.
func (c Credential) HasScope(scope string) bool {
	return slices.Contains(c.Scopes, scope)
}

// CanReadQuota reports whether the usage endpoint will return real numbers.
func (c Credential) CanReadQuota() bool { return c.HasScope(ScopeProfile) }

// Expired reports whether the access token is past its expiry. An unknown expiry
// counts as not expired: refusing to fetch on a missing field would turn a
// harmless shape change into a broken feature.
func (c Credential) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// epochToTime accepts either milliseconds or seconds since the epoch. Claude Code
// writes milliseconds; accepting both means a unit change does not read as a
// token that expired in 1970.
func epochToTime(v *float64) time.Time {
	if v == nil || *v <= 0 {
		return time.Time{}
	}
	const millisThreshold = 1e11 // ~1973 in ms, ~5138 in seconds
	if *v > millisThreshold {
		return time.UnixMilli(int64(*v)).UTC()
	}
	return time.Unix(int64(*v), 0).UTC()
}
