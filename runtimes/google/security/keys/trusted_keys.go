// Package keys provides methods for managing various (trusted) issuer public keys.
package keys

import (
	"reflect"
	"sync"

	"veyron.io/veyron/veyron2/security"
)

var (
	// trusted holds the set of trusted public keys for certificates with the
	// self-signed name matching the key of the map.
	trusted   map[string][]security.PublicKey
	trustedMu sync.Mutex
)

// Trust adds a public key to the set of keys trusted for the named
// identity provider.
func Trust(key security.PublicKey, name string) {
	trustedMu.Lock()
	trusted[name] = append(trusted[name], key)
	trustedMu.Unlock()
	// TODO(ataly): Eventually, "trusted" must be persisted.
}

// TrustLevel denotes the level of trust in an identity provider.
type TrustLevel int

const (
	// Unknown is the TrustLevel when the identity provider is not known -
	// no keys have ever been registered as trusted.
	Unknown TrustLevel = iota
	// Mistrusted is the TrustLevel returned when keys have been registered
	// for a named identity provider, but the key provided to LevelOfTrust
	// is not one of those registered keys.
	Mistrusted
	// Trusted is the TrustLevel returned when a key has been explicitly
	// registered as trustworthy for an identity provider (via a call to
	// Trust).
	Trusted
)

func (l TrustLevel) String() string {
	switch l {
	case Unknown:
		return "Unknown"
	case Mistrusted:
		return "Mistrusted"
	case Trusted:
		return "Trusted"
	default:
		return "<invalid TrustLevel>"
	}
}

// LevelOfTrust returns the TrustLevel for the given (key, identity provider) pair.
func LevelOfTrust(key security.PublicKey, name string) TrustLevel {
	trustedMu.Lock()
	defer trustedMu.Unlock()
	keys, exists := trusted[name]
	if !exists {
		return Unknown
	}
	for _, k := range keys {
		if reflect.DeepEqual(k, key) {
			return Trusted
		}
	}
	return Mistrusted
}

func init() {
	trusted = make(map[string][]security.PublicKey)
}
