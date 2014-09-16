package security

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"veyron/runtimes/google/security/keys"

	"veyron2/security"
	"veyron2/vom"
)

var (
	// Trusted identity providers
	// (type assertion just to ensure test sanity)
	veyronChain = newChain("veyron").(*chainPrivateID)
	googleChain = newChain("google").(*chainPrivateID)
)

type dischargeMap map[string]security.Discharge

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
	id, err := newChainPrivateID(name, nil)
	if err != nil {
		panic(err)
	}
	return id
}

func newSetPublicID(ids ...security.PublicID) security.PublicID {
	id, err := NewSetPublicID(ids...)
	if err != nil {
		panic(err)
	}
	return id
}

func newSetPrivateID(ids ...security.PrivateID) security.PrivateID {
	id, err := NewSetPrivateID(ids...)
	if err != nil {
		panic(err)
	}
	return id
}

func bless(blessee security.PublicID, blessor security.PrivateID, name string, caveats ...security.Caveat) security.PublicID {
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

func verifyAuthorizedID(origID, authID security.PublicID, authNames []string) error {
	if authID == nil {
		if len(authNames) != 0 {
			return fmt.Errorf("%q.Authorize is nil, want identity with names: %v", origID, authNames)
		}
		return nil
	}
	if got, want := authID.Names(), authNames; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("%q(%T).Names(): got: %v, want: %v", authID, authID, got, want)
	}

	if !reflect.DeepEqual(origID.PublicKey(), authID.PublicKey()) {
		return fmt.Errorf("%q.Authorize returned %q with public key %v, should have had %v", origID, authID, authID.PublicKey(), origID.PublicKey())
	}
	if _, err := roundTrip(authID); err != nil {
		return fmt.Errorf("%q.Authorize returned %q, which failed roundTripping: %v", origID, authID, err)
	}
	return nil
}

func newCaveat(validator security.CaveatValidator) security.Caveat {
	cav, err := security.NewCaveat(validator)
	if err != nil {
		panic(err)
	}
	return cav
}

func mkCaveat(cav security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return cav
}

func init() {
	// Mark "veyron" and "google" as trusted identity providers.
	keys.Trust(veyronChain.PublicID().PublicKey(), "veyron")
	keys.Trust(googleChain.PublicID().PublicKey(), "google")
}

func TestTrustIdentityProviders(t *testing.T) {
	var (
		cSelf     = newChain("chainself")
		cProvider = newChain("provider")
		cBlessed  = derive(bless(cSelf.PublicID(), cProvider, "somebody"), cSelf)

		cProvider1 = newChain("provider")
		cProvider2 = newChain("provider")
		cSomebody1 = derive(bless(cSelf.PublicID(), cProvider1, "somebody1"), cSelf)
		cSomebody2 = derive(bless(cSelf.PublicID(), cProvider2, "somebody2"), cSelf)
		setID      = newSetPrivateID(cSomebody1, cSomebody2)

		fake = security.FakePrivateID("fake")
	)
	// Initially nobody is trusted
	m := map[security.PrivateID]bool{
		cSelf:      false,
		cProvider:  false,
		cBlessed:   false,
		cProvider1: false,
		cProvider2: false,
		cSomebody1: false,
		cSomebody2: false,
	}
	test := func() {
		for priv, want := range m {
			id := priv.PublicID()
			switch tl := keys.LevelOfTrust(id.PublicKey(), "provider"); tl {
			case keys.Trusted:
				if !want {
					t.Errorf("%q is trusted, should not be", id)
				}
			case keys.Unknown, keys.Mistrusted:
				if want {
					t.Errorf("%q is %v, it should be trusted", id, tl)
				}
			default:
				t.Errorf("%q has an invalid trust level: %v", tl)
			}
		}
	}
	test()
	// Trusting cBlessed should cause cProvider to be trusted
	TrustIdentityProviders(cBlessed)
	m[cProvider] = true
	test()
	// Trusting setID should cause both cProvider1 and cProvider2
	// to be trusted.
	TrustIdentityProviders(setID)
	m[cProvider1] = true
	m[cProvider2] = true
	// Trusting a fake identity should be a no-op
	TrustIdentityProviders(fake)
	test()
}
