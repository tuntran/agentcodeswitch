//go:build darwin

package profile

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
)

// notFoundExit is what /usr/bin/security returns when no item matches. Verified
// on macOS 15: 44 for a missing service, 0 for a hit.
const notFoundExit = 44

type darwinCredStore struct{}

// DefaultCredStore returns the login Keychain backend.
func DefaultCredStore() CredStore { return darwinCredStore{} }

func (darwinCredStore) ServiceName(lit ConfigDirLiteral) string {
	return ServiceNameFor(lit)
}

// Exists deliberately omits -w. Without it security reads attributes only, which
// is what keeps `acs ls` and `acs doctor` free of Keychain dialogs.
func (darwinCredStore) Exists(service string) (bool, error) {
	err := exec.Command("security", "find-generic-password", "-s", service).Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == notFoundExit {
		return false, nil
	}
	return false, fmt.Errorf("look up keychain item %q: %w", service, err)
}

// ReadSecret asks for the secret itself, which is why macOS prompts on first use.
// Only quota and `doctor --deep` are allowed here.
func (darwinCredStore) ReadSecret(service string) ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == notFoundExit {
			return nil, fmt.Errorf("no keychain item %q", service)
		}
		// The error is returned without stderr attached on purpose: security can
		// echo the item back, and nothing that may hold a token belongs in a
		// message that gets logged.
		return nil, fmt.Errorf("read keychain item %q: %w", service, err)
	}
	return bytes.TrimSpace(out), nil
}

func (darwinCredStore) Delete(service string) error {
	err := exec.Command("security", "delete-generic-password", "-s", service).Run()
	if err == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == notFoundExit {
		return nil // already gone; deleting is idempotent
	}
	return fmt.Errorf("delete keychain item %q: %w", service, err)
}

// svceLine matches the service attribute in `security dump-keychain` output:
//
//	"svce"<blob>="Claude Code-credentials-707f7e46"
var svceLine = regexp.MustCompile(`"svce"<blob>="(` + regexp.QuoteMeta(ServicePrefix) + `[^"]*)"`)

// List enumerates stored Claude Code credentials.
//
// dump-keychain without -d lists attributes only, so this stays prompt-free. It
// measured at ~20ms, cheap enough to run inside doctor and around a login.
func (darwinCredStore) List() ([]string, error) {
	out, err := exec.Command("security", "dump-keychain").Output()
	if err != nil {
		return nil, fmt.Errorf("dump keychain: %w", err)
	}
	var services []string
	seen := map[string]struct{}{}
	for _, m := range svceLine.FindAllStringSubmatch(string(out), -1) {
		svc := m[1]
		if _, dup := seen[svc]; dup {
			continue
		}
		seen[svc] = struct{}{}
		services = append(services, svc)
	}
	return services, nil
}
