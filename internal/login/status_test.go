package login

import "testing"

func TestParseStatus(t *testing.T) {
	raw := []byte(`{"loggedIn":true,"authMethod":"claudeai","apiProvider":"anthropic",
		"email":"alice@example.com","orgId":"org-1","orgName":"Org","subscriptionType":"max"}`)

	s, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if !s.LoggedIn || s.Email != "alice@example.com" || s.SubscriptionType != "max" {
		t.Errorf("Status = %+v", s)
	}
	id := s.Identity()
	if id.Email != s.Email || id.OrgID != "org-1" || id.OrgName != "Org" {
		t.Errorf("Identity = %+v", id)
	}
}

// TestParseStatusToleratesUnknownFields: Claude Code owns this shape and adds
// fields freely. Treating an unknown one as an error would break acs on a release
// that has nothing to do with acs.
func TestParseStatusToleratesUnknownFields(t *testing.T) {
	raw := []byte(`{"loggedIn":true,"email":"a@b.c","somethingNew":{"nested":[1,2]}}`)

	s, err := ParseStatus(raw)
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if !s.LoggedIn || s.Email != "a@b.c" {
		t.Errorf("Status = %+v", s)
	}
}

// TestParseStatusToleratesMissingFields: only loggedIn and email are load-bearing.
func TestParseStatusToleratesMissingFields(t *testing.T) {
	s, err := ParseStatus([]byte(`{"loggedIn":false}`))
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if s.LoggedIn {
		t.Error("LoggedIn = true")
	}
	if s.Email != "" || s.SubscriptionType != "" {
		t.Errorf("Status = %+v, want zero values", s)
	}
}

func TestParseStatusRejectsNonJSON(t *testing.T) {
	if _, err := ParseStatus([]byte("Not logged in.")); err == nil {
		t.Error("ParseStatus accepted plain text")
	}
}
