package keys

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func mkkey() *ecdsa.PublicKey {
	s, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return &s.PublicKey
}

func TestTrustedKeys(t *testing.T) {
	k1 := mkkey()
	k2 := mkkey()
	test := func(name string, k1Trust, k2Trust TrustLevel) error {
		var errs []string
		t1 := LevelOfTrust(k1, name)
		t2 := LevelOfTrust(k2, name)
		if t1 != k1Trust {
			errs = append(errs, fmt.Sprintf("Got %v want %v for LevelOfTrust(k1, %v)", t1, k1Trust, name))
		}
		if t2 != k2Trust {
			errs = append(errs, fmt.Sprintf("Got %v want %v for LevelOfTrust(k2, %v)", t2, k2Trust, name))
		}
		switch len(errs) {
		case 0:
			return nil
		case 1:
			return errors.New(errs[0])
		default:
			return errors.New(strings.Join(errs, ". "))
		}
	}

	// Initially, everything is unregistered
	if err := test("foo", Unknown, Unknown); err != nil {
		t.Error(err)
	}
	// k1 will be trusted for "foo" after Trust is called.
	Trust(k1, "foo")
	if err := test("foo", Trusted, Mistrusted); err != nil {
		t.Error(err)
	}
	// multiple keys can be trusted for the same name
	Trust(k2, "foo")
	if err := test("foo", Trusted, Trusted); err != nil {
		t.Error(err)
	}
	// Trust so far is only for "foo", not "bar"
	if err := test("bar", Unknown, Unknown); err != nil {
		t.Error(err)
	}
	Trust(k2, "bar")
	if err := test("bar", Mistrusted, Trusted); err != nil {
		t.Error(err)
	}
}
