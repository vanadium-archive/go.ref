package rt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2/security"
)

func matchesError(got error, want string) error {
	if (got == nil) && len(want) == 0 {
		return nil
	}
	if got == nil {
		return fmt.Errorf("Got nil error, wanted to match %q", want)
	}
	if !strings.Contains(got.Error(), want) {
		return fmt.Errorf("Got error %q, wanted to match %q", got, want)
	}
	return nil
}

func newPrincipal(t *testing.T) security.Principal {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to create private key for principal: %v", err)
	}
	signer := security.NewInMemoryECDSASigner(key)
	p, err := security.CreatePrincipal(signer, NewInMemoryBlessingStore(signer.PublicKey()), NewInMemoryBlessingRoots())
	if err != nil {
		t.Fatalf("security.CreatePrincipal failed: %v", err)
	}
	return p
}

func blessSelf(t *testing.T, p security.Principal, name string) security.Blessings {
	b, err := p.BlessSelf(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func bless(t *testing.T, p security.Principal, key security.PublicKey, with security.Blessings, extension string) security.Blessings {
	b, err := p.Bless(key, with, extension, security.UnconstrainedUse())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func unionOfBlessings(t *testing.T, blessings ...security.Blessings) security.Blessings {
	b, err := security.UnionOfBlessings(blessings...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
