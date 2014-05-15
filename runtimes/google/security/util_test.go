package security

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"veyron/runtimes/google/security/keys"
	"veyron/security/caveat"

	"veyron2/security"
	"veyron2/vom"
)

var (
	// Trusted identity providers
	// (type assertion just to ensure test sanity)
	veyronChain = newChain("veyron").(*chainPrivateID)
	veyronTree  = newTree("veyron").(*treePrivateID)
	googleChain = newChain("google").(*chainPrivateID)
	googleTree  = newTree("google").(*treePrivateID)
)

func matchesErrorPattern(err error, pattern string) bool {
	if (len(pattern) == 0) != (err == nil) {
		return false
	}
	return err == nil || strings.Index(err.Error(), pattern) >= 0
}

func encode(id security.PublicID) ([]byte, error) {
	var b bytes.Buffer
	err := vom.NewEncoder(&b).Encode(id)
	return b.Bytes(), err
}

func decode(b []byte) (security.PublicID, error) {
	dID := new(security.PublicID)
	err := vom.NewDecoder(bytes.NewReader(b)).Decode(dID)
	return *dID, err
}

func roundTrip(id security.PublicID) (security.PublicID, error) {
	b, err := encode(id)
	if err != nil {
		return nil, err
	}
	return decode(b)
}

func newChain(name string) security.PrivateID {
	id, err := newChainPrivateID(name)
	if err != nil {
		panic(err)
	}
	return id
}

func newTree(name string) security.PrivateID {
	id, err := newTreePrivateID(name)
	if err != nil {
		panic(err)
	}
	return id
}

func bless(blessee security.PublicID, blessor security.PrivateID, name string, caveats []security.ServiceCaveat) security.PublicID {
	blessed, err := blessor.Bless(blessee, name, 5*time.Minute, caveats)
	if err != nil {
		panic(err)
	}
	return blessed
}

func derive(pub security.PublicID, priv security.PrivateID) security.PrivateID {
	d, err := priv.Derive(pub)
	if err != nil {
		panic(err)
	}
	return d
}

func verifyAuthorizedID(origID, authID security.PublicID, authName string) error {
	if authID == nil {
		if len(authName) != 0 {
			return fmt.Errorf("%q.Authorize returned nil, want %q", origID, authName)
		}
		return nil
	}
	if got := fmt.Sprintf("%s", authID); got != authName {
		return fmt.Errorf("%q.Authorize returned %q want %q", origID, authID, authName)
	}
	if !reflect.DeepEqual(origID.PublicKey(), authID.PublicKey()) {
		return fmt.Errorf("%q.Authorize returned %q with public key %v, should have had %v", origID, authID, authID.PublicKey(), origID.PublicKey())
	}
	if _, err := roundTrip(authID); err != nil {
		return fmt.Errorf("%q.Authorize returned %q, which failed roundTripping: %v", origID, authID, err)
	}
	return nil
}

func methodRestrictionCaveat(service security.PrincipalPattern, methods []string) []security.ServiceCaveat {
	return []security.ServiceCaveat{
		{Service: service, Caveat: caveat.MethodRestriction(methods)},
	}
}

func peerIdentityCaveat(p security.PrincipalPattern) []security.ServiceCaveat {
	return []security.ServiceCaveat{security.UniversalCaveat(caveat.PeerIdentity{p})}
}

func init() {
	// Mark "veyron" and "google" as trusted identity providers.
	keys.Trust(veyronChain.PublicID().PublicKey(), "veyron")
	keys.Trust(veyronTree.PublicID().PublicKey(), "veyron")
	keys.Trust(googleChain.PublicID().PublicKey(), "google")
	keys.Trust(googleTree.PublicID().PublicKey(), "google")
}

func TestTrustIdentityProviders(t *testing.T) {
	var (
		cSelf = newChain("chainself")
		// cBlessed = "chainprovider/somebody"
		cProvider = newChain("chainprovider")
		cBlessed  = derive(bless(cSelf.PublicID(), cProvider, "somebody", nil), cSelf)

		tSelf     = newTree("treeself")
		tProvider = newTree("treeprovider")
		tBlessed  = derive(bless(tSelf.PublicID(), tProvider, "somebody", nil), tSelf)

		fake = security.FakePrivateID("fake")
	)
	// Initially nobody is trusted
	m := map[security.PrivateID]bool{
		cSelf:     false,
		cProvider: false,
		tSelf:     false,
		tProvider: false,
		fake:      false,
	}
	test := func() {
		for priv, want := range m {
			id := priv.PublicID()
			key := id.PublicKey()
			tl := keys.LevelOfTrust(key, id.Names()[0])
			switch tl {
			case keys.Trusted:
				if !want {
					t.Errorf("%q is trusted, should not be", id)
				}
			case keys.Mistrusted:
				t.Errorf("%q is mistrusted. This test should not allow anyone to be mistrusted", id)
			case keys.Unknown:
				if want {
					t.Errorf("%q is not trusted, it should be", id)
				}
			default:
				t.Errorf("%q has an invalid trust level: %v", tl)
			}
		}
	}
	test()
	// Trusting cSelf
	TrustIdentityProviders(cSelf)
	m[cSelf] = true
	test()
	// Trusting cBlessed should cause cProvider to be trusted
	TrustIdentityProviders(cBlessed)
	m[cProvider] = true
	test()
	// Trusting tBlessed should cause both tSelf and tProvider to be
	// trusted (since tBlessed has both as identity providers)
	TrustIdentityProviders(tBlessed)
	m[tSelf] = true
	m[tProvider] = true
	// Trusting a fake identity should be a no-op
	TrustIdentityProviders(fake)
	test()
}
