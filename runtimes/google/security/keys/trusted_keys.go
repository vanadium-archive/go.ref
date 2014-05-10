// Package keys provides methods for managing various (trusted) issuer public keys.
package keys

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"log"
	"reflect"
	"strings"
	"sync"

	"veyron2/vom"
)

var (
	// trusted holds the set of trusted public keys for certificates with the
	// self-signed name matching the key of the map.
	trusted   map[string][]ecdsa.PublicKey
	trustedMu sync.Mutex
)

// Trust adds an ECDSA public key to the set of keys trusted for the named
// identity provider.
func Trust(key *ecdsa.PublicKey, name string) {
	trustedMu.Lock()
	trusted[name] = append(trusted[name], *key)
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
func LevelOfTrust(key *ecdsa.PublicKey, name string) TrustLevel {
	trustedMu.Lock()
	defer trustedMu.Unlock()
	keys, exists := trusted[name]
	if !exists {
		return Unknown
	}
	for _, k := range keys {
		if reflect.DeepEqual(k, *key) {
			return Trusted
		}
	}
	return Mistrusted
}

func init() {
	// Public key from:
	// http://www.vonery.com:8125/pubkey/base64vom
	// TODO(ashankar,ataly,gauthamt): Handle key rotation
	pubkey := strings.NewReader("_4EEGgFCAP-DNBoBQwEudmV5cm9uL3J1bnRpbWVzL2dvb2dsZS9zZWN1cml0eS5jaGFpblByaXZhdGVJRAD_hVEYAQIBRAEIUHVibGljSUQAAQQBBlNlY3JldAABM3ZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5DaGFpblByaXZhdGVJRAD_hwQaAUUA_4lJGAEBAUYBDENlcnRpZmljYXRlcwABMnZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5DaGFpblB1YmxpY0lEAP-LBBIBRwD_jWcYAQQBAwEETmFtZQABSAEJUHVibGljS2V5AAFJAQdDYXZlYXRzAAFKAQlTaWduYXR1cmUAATB2ZXlyb24vcnVudGltZXMvZ29vZ2xlL3NlY3VyaXR5L3dpcmUuQ2VydGlmaWNhdGUA_49FGAECAUsBBUN1cnZlAAEEAQJYWQABLnZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5QdWJsaWNLZXkA_5UzEAEyAS12ZXlyb24vcnVudGltZXMvZ29vZ2xlL3NlY3VyaXR5L3dpcmUua2V5Q3VydmUA_5EEEgFMAP-XRxgBAgEDAQdTZXJ2aWNlAAEEAQVCeXRlcwABK3ZleXJvbi9ydW50aW1lcy9nb29nbGUvc2VjdXJpdHkvd2lyZS5DYXZlYXQA_5NAGAECAQQBAVIAAQQBAVMAAS52ZXlyb24vcnVudGltZXMvZ29vZ2xlL3NlY3VyaXR5L3dpcmUuU2lnbmF0dXJlAP-C_gH7AQMBBQECAQZ2ZXlyb24BAkEEz-L8bujLPEmGVHmnzTTzaTG7EoyB2GFHSVZ97SXzUZGobaxhW3R2qGhCLCNTv79-c-2FXewJEmiBprq48QdOIQACASDeFogv5V9-aBhZiD5odRy1DxBm62trz4BXpwrL7hGxlAEgAWVLl5BX2VwbtsSJCkHuUzbeULnonbrEClWlKbpCki8AAAETYXNoYW5rYXJAZ29vZ2xlLmNvbQECQQTu9oVCmTKrLuoAFGptua2W-4IdXhCfsA39YvKdODoxTeRHjalE5rOtxzHSLd3XAnlCBmndTj2Z6C35z585tPpXAAEBAQEqAf-T_4EEGgFCAP-DUBgBAgFDAQlJc3N1ZVRpbWUAAUMBCkV4cGlyeVRpbWUAAS12ZXlyb24vcnVudGltZXMvZ29vZ2xlL3NlY3VyaXR5L2NhdmVhdC5FeHBpcnkA_4UPEAEEAQl0aW1lLlRpbWUA_4IkAQEPAQAAAA7KzyIxGS03Cv5cAQ8BAAAADsywVbEZLTcK_lwAAAEBIK7y4lmfaPqgFIZxQbJmnaAdC2tmOk8NMj6pn1Q_OF75ASAXTcBWHYgJMYzWHJfgpUhniugLw9fLicONAEpP719ZwgAAAAEgzflfPomhIOwxoelqghHYV43RhQ2yiZ_eY0UQ6AU9nqgA")
	// Register the elliptic curve type for the encoded public key with VOM.
	vom.Register(elliptic.P256())
	var pub ecdsa.PublicKey
	if err := vom.NewDecoder(base64.NewDecoder(base64.URLEncoding, pubkey)).Decode(&pub); err != nil {
		log.Fatalln("Invalid ecdsa.PublicKey in the binary. Error:", err)
	}
	trusted = make(map[string][]ecdsa.PublicKey)
	// TODO(ashankar): No keys should be baked in by default.
	// Update security.Runtime to have a notion of "TrustIdentityProvider"
	// or something like that.
	Trust(&pub, "veyron")
}
