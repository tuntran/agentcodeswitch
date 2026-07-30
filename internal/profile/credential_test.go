package profile

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func storeWith(t *testing.T, payload string) (*fakeCredStore, ConfigDirLiteral) {
	t.Helper()
	lit := mustLit(t, "/Users/x/.acs/profiles/per")
	cs := newFakeCredStore()
	cs.items[cs.ServiceName(lit)] = payload
	return cs, lit
}

func TestReadCredentialParsesBlob(t *testing.T) {
	expires := time.Now().Add(8 * time.Hour).Truncate(time.Millisecond)
	cs, lit := storeWith(t, `{"claudeAiOauth":{
		"accessToken":"secret-token","refreshToken":"refresh","expiresAt":`+
		strconv.FormatInt(expires.UnixMilli(), 10)+`,
		"scopes":["user:profile","user:inference"],"subscriptionType":"max"}}`)

	cred, err := ReadCredential(cs, lit)
	if err != nil {
		t.Fatalf("ReadCredential: %v", err)
	}
	if cred.AccessToken != "secret-token" {
		t.Errorf("AccessToken = %q", cred.AccessToken)
	}
	if !cred.CanReadQuota() {
		t.Error("CanReadQuota() = false with user:profile present")
	}
	if cred.SubscriptionType != "max" {
		t.Errorf("SubscriptionType = %q, want max", cred.SubscriptionType)
	}
	if !cred.ExpiresAt.Equal(expires.UTC()) {
		t.Errorf("ExpiresAt = %v, want %v", cred.ExpiresAt, expires.UTC())
	}
	if cred.Expired(time.Now()) {
		t.Error("Expired() = true for a token valid for 8 more hours")
	}
}

// TestReadCredentialWithoutProfileScope is the condition that makes the usage
// endpoint answer 200 with an empty body -- the case that must never render as 0%.
func TestReadCredentialWithoutProfileScope(t *testing.T) {
	cs, lit := storeWith(t,
		`{"claudeAiOauth":{"accessToken":"t","scopes":["user:inference"]}}`)

	cred, err := ReadCredential(cs, lit)
	if err != nil {
		t.Fatalf("ReadCredential: %v", err)
	}
	if cred.CanReadQuota() {
		t.Error("CanReadQuota() = true without user:profile")
	}
}

func TestReadCredentialRejectsWrongShape(t *testing.T) {
	tests := map[string]string{
		"not json":          `not json at all`,
		"no claudeAiOauth":  `{"somethingElse":{}}`,
		"no access token":   `{"claudeAiOauth":{"scopes":["user:profile"]}}`,
		"null claudeAiOuth": `{"claudeAiOauth":null}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			cs, lit := storeWith(t, payload)
			if _, err := ReadCredential(cs, lit); err == nil {
				t.Error("ReadCredential accepted a payload that is not a credential")
			}
		})
	}
}

// TestReadCredentialErrorNeverEchoesPayload: the payload is the token. An error
// message can end up in a log, an issue, or a screenshot.
func TestReadCredentialErrorNeverEchoesPayload(t *testing.T) {
	const token = "super-secret-token-value"
	cs, lit := storeWith(t, `{"claudeAiOauth":{"accessToken":"`+token+`"`) // truncated JSON

	_, err := ReadCredential(cs, lit)
	if err == nil {
		t.Fatal("ReadCredential accepted truncated JSON")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error message contains the token: %v", err)
	}
}

// TestExpiresAtAcceptsSecondsAndMillis keeps a unit change from reading as a token
// that expired in 1970, which would report every profile as expired.
func TestExpiresAtAcceptsSecondsAndMillis(t *testing.T) {
	want := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	millis := float64(want.UnixMilli())
	seconds := float64(want.Unix())

	if got := epochToTime(&millis); !got.Equal(want) {
		t.Errorf("epochToTime(millis) = %v, want %v", got, want)
	}
	if got := epochToTime(&seconds); !got.Equal(want) {
		t.Errorf("epochToTime(seconds) = %v, want %v", got, want)
	}
}

// TestMissingExpiryIsNotExpired: refusing to fetch on an absent field would turn a
// harmless shape change into a broken feature.
func TestMissingExpiryIsNotExpired(t *testing.T) {
	cs, lit := storeWith(t,
		`{"claudeAiOauth":{"accessToken":"t","scopes":["user:profile"]}}`)

	cred, err := ReadCredential(cs, lit)
	if err != nil {
		t.Fatalf("ReadCredential: %v", err)
	}
	if !cred.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want zero", cred.ExpiresAt)
	}
	if cred.Expired(time.Now()) {
		t.Error("Expired() = true for a credential with no expiry field")
	}
}

func TestExpiredDetectsPastExpiry(t *testing.T) {
	cred := Credential{ExpiresAt: time.Now().Add(-time.Minute)}
	if !cred.Expired(time.Now()) {
		t.Error("Expired() = false for a token that expired a minute ago")
	}
}
