package doctor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/config"
	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// fakeCredStore models the store as one map of service -> secret, exactly like the
// real Keychain. An orphan is not a separate concept here: it is simply an item
// that is stored while no profile's literal hashes to it.
type fakeCredStore struct {
	items   map[string]string
	listErr error
}

func (f *fakeCredStore) ServiceName(lit profile.ConfigDirLiteral) string {
	return profile.ServiceNameFor(lit)
}

func (f *fakeCredStore) Exists(service string) (bool, error) {
	_, ok := f.items[service]
	return ok, nil
}

func (f *fakeCredStore) ReadSecret(service string) ([]byte, error) {
	s, ok := f.items[service]
	if !ok {
		return nil, errors.New("no such item")
	}
	return []byte(s), nil
}

func (f *fakeCredStore) Delete(service string) error {
	delete(f.items, service)
	return nil
}

func (f *fakeCredStore) List() ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	services := make([]string, 0, len(f.items))
	for svc := range f.items {
		services = append(services, svc)
	}
	return services, nil
}

// credentialBlob builds a Keychain payload in Claude Code's shape.
func credentialBlob(t *testing.T, scopes []string, expiresAt time.Time) string {
	t.Helper()
	blob := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "test-token",
			"scopes":           scopes,
			"expiresAt":        expiresAt.UnixMilli(),
			"subscriptionType": "max",
		},
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal blob: %v", err)
	}
	return string(raw)
}

// setupHealthy creates one fully working profile.
func setupHealthy(t *testing.T) (*fakeCredStore, profile.Profile) {
	t.Helper()
	t.Setenv("ACS_HOME", t.TempDir())

	p, err := profile.Create("per", "Personal")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	writeClaudeJSON(t, p.Literal.String(), "alice@example.com")

	cs := &fakeCredStore{items: map[string]string{}}
	cs.items[cs.ServiceName(p.Literal)] = credentialBlob(t,
		[]string{profile.ScopeProfile, "user:inference"}, time.Now().Add(8*time.Hour))
	return cs, p
}

func writeClaudeJSON(t *testing.T, dir, email string) {
	t.Helper()
	raw := `{"numStartups":3,"oauthAccount":{"accountUuid":"u","emailAddress":"` + email +
		`","organizationUuid":"o","organizationName":"Org","organizationType":"claude_max"}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}
}

func TestRunHealthyProfilePasses(t *testing.T) {
	cs, _ := setupHealthy(t)

	r := Run(cs, false)
	if !r.OK() {
		t.Errorf("healthy profile did not pass: %+v", r)
	}
	if r.Deep {
		t.Error("Deep = true for a shallow run")
	}
	// A shallow run reports literal, keychain and identity -- and nothing that
	// needed a secret.
	if len(r.Profiles) != 1 || len(r.Profiles[0].Checks) != 3 {
		t.Fatalf("checks = %+v, want 3", r.Profiles)
	}
	for _, c := range r.Profiles[0].Checks {
		if c.Name == "scope" || c.Name == "expiry" {
			t.Errorf("%s check ran without --deep; it reads the secret", c.Name)
		}
	}
}

// TestRunDetectsTrailingSlashLiteral is the scenario from the plan: someone edits
// configDirLiteral, the hash changes, and the real credential becomes an orphan.
// Doctor must name both halves and repair neither.
func TestRunDetectsTrailingSlashLiteral(t *testing.T) {
	cs, p := setupHealthy(t)
	// The credential Claude Code created stays exactly where it is. Only the
	// stored literal drifts, so the item stops being reachable and shows up as an
	// orphan -- no fixture trickery involved.
	realService := cs.ServiceName(p.Literal)

	err := config.Update(func(f *config.File) error {
		e := f.Profiles["per"]
		e.ConfigDirLiteral += "/"
		f.Profiles["per"] = e
		return nil
	})
	if err != nil {
		t.Fatalf("config.Update: %v", err)
	}

	r := Run(cs, false)
	if r.OK() {
		t.Error("OK() = true for a drifted literal")
	}
	lit := findCheck(t, r, "per", "literal")
	if lit.Status != StatusFail {
		t.Errorf("literal check = %s, want fail", lit.Status)
	}
	if lit.Action == "" {
		t.Error("literal failure has no action to take")
	}
	if len(r.Orphans) != 1 || r.Orphans[0] != realService {
		t.Errorf("Orphans = %v, want [%s]", r.Orphans, realService)
	}

	// The stored literal must be untouched: doctor reports, never repairs.
	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if got := f.Profiles["per"].ConfigDirLiteral; got != p.Literal.String()+"/" {
		t.Errorf("doctor changed the stored literal to %q", got)
	}
}

func TestRunReportsMissingKeychainItem(t *testing.T) {
	cs, p := setupHealthy(t)
	delete(cs.items, cs.ServiceName(p.Literal))

	r := Run(cs, false)
	if r.OK() {
		t.Error("OK() = true with no credential")
	}
	kc := findCheck(t, r, "per", "keychain")
	if kc.Status != StatusFail || kc.Action == "" {
		t.Errorf("keychain check = %+v, want fail with an action", kc)
	}
}

func TestRunReportsMissingIdentity(t *testing.T) {
	cs, p := setupHealthy(t)
	if err := os.Remove(filepath.Join(p.Literal.String(), ".claude.json")); err != nil {
		t.Fatalf("remove .claude.json: %v", err)
	}

	r := Run(cs, false)
	id := findCheck(t, r, "per", "identity")
	if id.Status != StatusFail || id.Action == "" {
		t.Errorf("identity check = %+v, want fail with an action", id)
	}
}

// TestRunDeepDetectsMissingScope covers the worst failure mode in acs: a token
// that works for everything except quota, where the endpoint answers 200 with an
// empty body and a naive client renders 0%.
func TestRunDeepDetectsMissingScope(t *testing.T) {
	cs, p := setupHealthy(t)
	cs.items[cs.ServiceName(p.Literal)] = credentialBlob(t,
		[]string{"user:inference"}, time.Now().Add(8*time.Hour))

	r := Run(cs, true)
	if !r.Deep {
		t.Error("Deep = false for a deep run")
	}
	scope := findCheck(t, r, "per", "scope")
	if scope.Status != StatusFail {
		t.Errorf("scope check = %s, want fail", scope.Status)
	}
	if scope.Action == "" {
		t.Error("scope failure has no action to take")
	}
	if r.OK() {
		t.Error("OK() = true with no user:profile scope")
	}
}

func TestRunDeepDetectsExpiredToken(t *testing.T) {
	cs, p := setupHealthy(t)
	cs.items[cs.ServiceName(p.Literal)] = credentialBlob(t,
		[]string{profile.ScopeProfile}, time.Now().Add(-time.Hour))

	r := Run(cs, true)
	exp := findCheck(t, r, "per", "expiry")
	if exp.Status != StatusFail || exp.Action == "" {
		t.Errorf("expiry check = %+v, want fail with an action", exp)
	}
}

func TestRunDeepPassesHealthyCredential(t *testing.T) {
	cs, _ := setupHealthy(t)

	r := Run(cs, true)
	if !r.OK() {
		t.Errorf("healthy deep run did not pass: %+v", r)
	}
	findCheck(t, r, "per", "scope")
	findCheck(t, r, "per", "expiry")
}

// TestEveryFailureHasAnAction is the invariant behind "each ✗ comes with a fix".
// A report that only says "fail" takes away the ability to act on it.
func TestEveryFailureHasAnAction(t *testing.T) {
	cs, p := setupHealthy(t)
	delete(cs.items, cs.ServiceName(p.Literal))
	if err := os.Remove(filepath.Join(p.Literal.String(), ".claude.json")); err != nil {
		t.Fatalf("remove .claude.json: %v", err)
	}

	for _, deep := range []bool{false, true} {
		r := Run(cs, deep)
		for _, pr := range r.Profiles {
			for _, c := range pr.Checks {
				if c.Status != StatusOK && c.Action == "" {
					t.Errorf("deep=%v %s/%s is %s with no action", deep, pr.Name, c.Name, c.Status)
				}
			}
		}
	}
}

// TestOrphansDoNotFailTheReport is the fix for a real false positive.
//
// Any other tool that sets CLAUDE_CONFIG_DIR (ccs, for one) leaves a credential
// whose service name is indistinguishable from a drifted acs literal. Failing on
// that would leave doctor permanently red on a perfectly healthy machine, which is
// the fastest way to make people stop reading it.
func TestOrphansDoNotFailTheReport(t *testing.T) {
	cs, _ := setupHealthy(t)
	cs.items[profile.ServicePrefix+"-79a4535b"] = credentialBlob(t,
		[]string{profile.ScopeProfile}, time.Now().Add(time.Hour))

	r := Run(cs, false)
	if len(r.Orphans) != 1 {
		t.Errorf("Orphans = %v, want the unclaimed item listed", r.Orphans)
	}
	if !r.OK() {
		t.Error("OK() = false because another tool's credential exists")
	}
}

// TestNoProfilesReportsNoOrphans: with nothing configured, acs manages nothing and
// so cannot call any stored credential orphaned.
func TestNoProfilesReportsNoOrphans(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	cs := &fakeCredStore{items: map[string]string{
		profile.ServicePrefix:               "{}",
		profile.ServicePrefix + "-79a4535b": "{}",
	}}

	r := Run(cs, false)
	if len(r.Orphans) != 0 {
		t.Errorf("Orphans = %v, want none on an install with no profiles", r.Orphans)
	}
	if !r.OK() {
		t.Error("OK() = false on a fresh install with no profiles")
	}
}

// TestDefaultCredentialIsNotAnOrphan: the bare service name belongs to plain
// Claude Code in ~/.claude, not to a stale acs profile.
func TestDefaultCredentialIsNotAnOrphan(t *testing.T) {
	cs, _ := setupHealthy(t)
	cs.items[profile.ServicePrefix] = credentialBlob(t,
		[]string{profile.ScopeProfile}, time.Now().Add(time.Hour))

	r := Run(cs, false)
	if len(r.Orphans) != 0 {
		t.Errorf("Orphans = %v, want none", r.Orphans)
	}
	if !r.OK() {
		t.Error("OK() = false because of the default Claude Code credential")
	}
}

func TestRunWithNoProfiles(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	cs := &fakeCredStore{items: map[string]string{}}

	r := Run(cs, false)
	if !r.OK() {
		t.Errorf("empty install did not pass: %+v", r)
	}
	if len(r.Profiles) != 0 {
		t.Errorf("Profiles = %+v, want none", r.Profiles)
	}
}

// TestReportSlicesSerialiseAsArrays: a nil slice becomes JSON null, and a consumer
// reading .orphans.length on null throws. Both the `--json` contract and the Wails
// binding hand these straight to a caller.
func TestReportSlicesSerialiseAsArrays(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())

	raw, err := json.Marshal(Run(&fakeCredStore{items: map[string]string{}}, false))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Profiles []ProfileReport `json:"profiles"`
		Orphans  []string        `json:"orphans"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Profiles == nil || decoded.Orphans == nil {
		t.Errorf("nil slice serialised as null: %s", raw)
	}
}

func TestRunReportsUnreadableConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	r := Run(&fakeCredStore{items: map[string]string{}}, false)
	if r.ConfigError == "" {
		t.Error("ConfigError is empty for an unreadable config")
	}
	if r.OK() {
		t.Error("OK() = true with an unreadable config")
	}
}

func findCheck(t *testing.T, r Report, profileName, checkName string) Check {
	t.Helper()
	for _, p := range r.Profiles {
		if p.Name != profileName {
			continue
		}
		for _, c := range p.Checks {
			if c.Name == checkName {
				return c
			}
		}
	}
	t.Fatalf("check %q for profile %q not found in %+v", checkName, profileName, r.Profiles)
	return Check{}
}
