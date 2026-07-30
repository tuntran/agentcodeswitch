package login

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tuntran/agentcodeswitch/internal/config"
	"github.com/tuntran/agentcodeswitch/internal/profile"
)

// fileCredStore is a fake store whose contents live in a file, so the fake claude
// script can add an item to it the way a real login would.
type fileCredStore struct{ path string }

func (f fileCredStore) ServiceName(lit profile.ConfigDirLiteral) string {
	return profile.ServiceNameFor(lit)
}

func (f fileCredStore) List() ([]string, error) {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

func (f fileCredStore) Exists(service string) (bool, error) {
	stored, err := f.List()
	if err != nil {
		return false, err
	}
	for _, s := range stored {
		if s == service {
			return true, nil
		}
	}
	return false, nil
}

func (f fileCredStore) ReadSecret(string) ([]byte, error) {
	return []byte(`{"claudeAiOauth":{"accessToken":"t","scopes":["user:profile"]}}`), nil
}

func (f fileCredStore) Delete(string) error { return nil }

// harness wires a temp ACS_HOME, a fake claude on PATH, and a file-backed store.
type harness struct {
	store         fileCredStore
	argsFile      string
	configDirFile string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("ACS_HOME", t.TempDir())

	work := t.TempDir()
	h := &harness{
		store:         fileCredStore{path: filepath.Join(work, "store")},
		argsFile:      filepath.Join(work, "args"),
		configDirFile: filepath.Join(work, "configdirs"),
	}

	fake, err := filepath.Abs("testdata/fake-claude.sh")
	if err != nil {
		t.Fatalf("resolve fake claude: %v", err)
	}
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatalf("chmod fake claude: %v", err)
	}
	claudeBinary = fake
	t.Cleanup(func() { claudeBinary = "claude" })

	t.Setenv("FAKE_ARGS_FILE", h.argsFile)
	t.Setenv("FAKE_CONFIG_DIR_FILE", h.configDirFile)
	t.Setenv("FAKE_STORE_FILE", h.store.path)
	return h
}

// expectCreated makes the fake claude create the credential acs expects, which is
// what a correct login looks like.
func (h *harness) expectCreated(t *testing.T, name string) {
	t.Helper()
	lit, err := profile.NewConfigDirLiteral(filepath.Join(config.Home(), "profiles", name))
	if err != nil {
		t.Fatalf("NewConfigDirLiteral: %v", err)
	}
	t.Setenv("FAKE_CREATED_SERVICE", profile.ServiceNameFor(lit))
}

func (h *harness) args(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(h.argsFile)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func (h *harness) configDirs(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(h.configDirFile)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestAddCreatesProfileAndCachesIdentity(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "per")

	res, err := Add(h.store, "per", "Personal", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if res.Status.Email != "alice@example.com" {
		t.Errorf("email = %q", res.Status.Email)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings on a clean login: %v", res.Warnings)
	}

	p, err := profile.Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Cached.Email != "alice@example.com" || p.Cached.SubscriptionType != "max" {
		t.Errorf("identity not cached: %+v", p.Cached)
	}
	if p.Label != "Personal" {
		t.Errorf("label = %q, want Personal", p.Label)
	}
}

// TestAddAlwaysUsesClaudeai guards the trap: --console authenticates for API
// billing, producing a token with no user:profile scope, which makes the usage
// endpoint return an empty 200 and quota render as 0%.
func TestAddAlwaysUsesClaudeai(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "per")

	if _, err := Add(h.store, "per", "", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	login := h.args(t)[0]
	if !strings.Contains(login, "--claudeai") {
		t.Errorf("login args = %q, want --claudeai", login)
	}
	if strings.Contains(login, "--console") {
		t.Errorf("login args = %q, must never contain --console", login)
	}
}

func TestAddPassesEmailThrough(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "com")

	if _, err := Add(h.store, "com", "", "bob@example.com"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if login := h.args(t)[0]; !strings.Contains(login, "--email bob@example.com") {
		t.Errorf("login args = %q, want --email bob@example.com", login)
	}
}

// TestAddPassesFrozenLiteralToClaude is the whole isolation mechanism in one
// assertion: the string claude receives has to be the frozen literal, because that
// is what it hashes into the Keychain service name.
func TestAddPassesFrozenLiteralToClaude(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "per")

	res, err := Add(h.store, "per", "", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	want := res.Profile.Literal.String()
	for _, got := range h.configDirs(t) {
		if got != want {
			t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got, want)
		}
	}
}

// TestAddRollsBackOnCancel: a cancelled login must leave no trace of the profile.
func TestAddRollsBackOnCancel(t *testing.T) {
	h := newHarness(t)
	t.Setenv("FAKE_LOGIN_EXIT", "130") // Ctrl-C

	_, err := Add(h.store, "per", "", "")
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Add = %v, want ErrCancelled", err)
	}
	if _, err := profile.Get("per"); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("config entry survived a cancelled login: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.Home(), "profiles", "per")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("profile dir survived a cancelled login: %v", err)
	}
}

// TestAddRollsBackWhenNobodyIsLoggedIn covers login exiting 0 without authenticating.
func TestAddRollsBackWhenNobodyIsLoggedIn(t *testing.T) {
	h := newHarness(t)
	t.Setenv("FAKE_STATUS_JSON", `{"loggedIn":false}`)

	if _, err := Add(h.store, "per", "", ""); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("Add = %v, want ErrNotLoggedIn", err)
	}
	if _, err := profile.Get("per"); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("config entry survived: %v", err)
	}
}

// TestRollbackLeavesKeychainAlone: a partly completed login can have produced a
// real credential for a real account. Deleting that to tidy up would destroy
// something the user has.
func TestRollbackLeavesKeychainAlone(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "per")
	t.Setenv("FAKE_STATUS_JSON", `{"loggedIn":false}`)

	if _, err := Add(h.store, "per", "", ""); err == nil {
		t.Fatal("Add succeeded, want failure")
	}
	stored, err := h.store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(stored) != 1 {
		t.Errorf("stored credentials = %v, want the one claude created to survive", stored)
	}
}

// TestAddWarnsWhenCredentialNameIsUnexpected is the loud case: the login worked,
// but acs would look for the credential under a different name forever. That fails
// silently and presents as a random logout.
func TestAddWarnsWhenCredentialNameIsUnexpected(t *testing.T) {
	h := newHarness(t)
	t.Setenv("FAKE_CREATED_SERVICE", profile.ServicePrefix+"-deadbeef")

	res, err := Add(h.store, "per", "", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("no warning for a credential under an unexpected name")
	}
	joined := strings.Join(res.Warnings, "\n")
	if !strings.Contains(joined, "deadbeef") || !strings.Contains(joined, "acs doctor") {
		t.Errorf("warnings do not name the item and the fix: %v", res.Warnings)
	}
}

func TestLoginDoesNotRollBackExistingProfile(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "per")
	if _, err := Add(h.store, "per", "Personal", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}

	t.Setenv("FAKE_LOGIN_EXIT", "130")
	if _, err := Login(h.store, "per", "", false); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Login = %v, want ErrCancelled", err)
	}

	// Everything about the working profile has to survive a cancelled retry.
	p, err := profile.Get("per")
	if err != nil {
		t.Fatalf("profile lost after a cancelled login: %v", err)
	}
	if p.Cached.Email != "alice@example.com" || p.Label != "Personal" {
		t.Errorf("profile changed by a cancelled login: %+v", p)
	}
	if _, err := os.Stat(p.Dir); err != nil {
		t.Errorf("profile dir removed by a cancelled login: %v", err)
	}
}

func TestLoginForceLogsOutFirst(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "per")
	if _, err := Add(h.store, "per", "", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.Remove(h.argsFile); err != nil {
		t.Fatalf("reset args: %v", err)
	}

	if _, err := Login(h.store, "per", "", true); err != nil {
		t.Fatalf("Login: %v", err)
	}
	args := h.args(t)
	if len(args) < 2 || !strings.HasPrefix(args[0], "auth logout") {
		t.Errorf("args = %v, want logout before login", args)
	}
}

func TestLoginWithoutForceDoesNotLogOut(t *testing.T) {
	h := newHarness(t)
	h.expectCreated(t, "per")
	if _, err := Add(h.store, "per", "", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.Remove(h.argsFile); err != nil {
		t.Fatalf("reset args: %v", err)
	}

	if _, err := Login(h.store, "per", "", false); err != nil {
		t.Fatalf("Login: %v", err)
	}
	for _, a := range h.args(t) {
		if strings.HasPrefix(a, "auth logout") {
			t.Error("plain `acs login` logged out; that discards a working credential")
		}
	}
}

func TestLoginUnknownProfile(t *testing.T) {
	h := newHarness(t)
	if _, err := Login(h.store, "nope", "", false); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("Login = %v, want ErrNotFound", err)
	}
}

func TestAddRejectsReservedName(t *testing.T) {
	h := newHarness(t)
	if _, err := Add(h.store, "quota", "", ""); !errors.Is(err, profile.ErrReservedName) {
		t.Errorf("Add = %v, want ErrReservedName", err)
	}
	// A rejected name must not have run a login.
	if args := h.args(t); len(args) > 0 && args[0] != "" {
		t.Errorf("claude was invoked for an invalid name: %v", args)
	}
}
