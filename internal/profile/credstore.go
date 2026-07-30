package profile

import (
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/text/unicode/norm"
)

// ServicePrefix is the Keychain service name Claude Code uses for the default
// config dir. Custom dirs get "-<hash8>" appended.
const ServicePrefix = "Claude Code-credentials"

// CredStore is the seam over the platform credential store. It stays deliberately
// narrow: acs reads attributes, occasionally reads a secret, and deletes items.
// It never creates one -- that is `claude auth login`'s job, and reimplementing it
// would mean minting tokens with Claude Code's client ID.
type CredStore interface {
	// ServiceName is the store key for a profile's literal.
	ServiceName(ConfigDirLiteral) string
	// Exists checks attributes only and must never raise a Keychain dialog,
	// because `acs ls` and `acs doctor` promise to stay silent.
	Exists(service string) (bool, error)
	// ReadSecret returns the credential blob. This does prompt the first time,
	// so only `acs quota` and `acs doctor --deep` may call it.
	ReadSecret(service string) ([]byte, error)
	Delete(service string) error
	// List returns every stored service name with the Claude Code prefix.
	// Orphan detection and the before/after snapshot around a login are both
	// derived from this, so there is one definition of "what is stored".
	List() ([]string, error)
}

// Orphans returns stored credentials that no known profile accounts for -- the
// signature of a config dir literal that drifted.
//
// The bare prefix is never an orphan: that is plain Claude Code's credential for
// ~/.claude, which acs does not manage.
func Orphans(cs CredStore, known []string) ([]string, error) {
	stored, err := cs.List()
	if err != nil {
		return nil, err
	}
	knownSet := make(map[string]struct{}, len(known)+1)
	knownSet[ServicePrefix] = struct{}{}
	for _, k := range known {
		knownSet[k] = struct{}{}
	}
	var orphans []string
	for _, svc := range stored {
		if _, ok := knownSet[svc]; !ok {
			orphans = append(orphans, svc)
		}
	}
	return orphans, nil
}

// ServiceNameFor derives the service name from a literal.
//
//	sha256(NFC(literal)) -> hex -> first 8 chars
//
// Confirmed against a real item: "/Users/<u>/.ccs/instances/personal" hashes to
// 79a4535b, which is the name Claude Code actually created.
//
// The input is the literal as passed, never a resolved path. Four spellings of one
// directory give four different names, which is the whole reason
// ConfigDirLiteral exists.
func ServiceNameFor(lit ConfigDirLiteral) string {
	sum := sha256.Sum256([]byte(norm.NFC.String(lit.String())))
	return ServicePrefix + "-" + hex.EncodeToString(sum[:])[:8]
}
