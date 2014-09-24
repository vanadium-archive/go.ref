package rt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"veyron.io/veyron/veyron2/security"
)

type s []string

type rootsTester struct {
	k1, k2, k3 security.PublicKey
}

func (t *rootsTester) testAdd(br security.BlessingRoots) error {
	testdata := []struct {
		root    security.PublicKey
		pattern security.BlessingPattern
	}{
		{t.k1, "veyron/..."},
		{t.k2, "google/foo/..."},
		{t.k1, "google"},
	}
	for _, d := range testdata {
		if err := br.Add(d.root, d.pattern); err != nil {
			return fmt.Errorf("%v.Add(%v, %q) failed: %s", br, d.root, d.pattern, err)
		}
	}
	return nil
}

func (t *rootsTester) testRecognized(br security.BlessingRoots) error {
	testdata := []struct {
		root          security.PublicKey
		recognized    []string
		notRecognized []string
	}{
		{t.k1, s{"veyron", "veyron/foo", "veyron/foo/bar", "google"}, s{"google/foo", "foo", "foo/bar"}},
		{t.k2, s{"google", "google/foo", "google/foo/bar"}, s{"google/bar", "veyron", "veyron/foo", "foo", "foo/bar"}},
		{t.k3, s{}, s{"veyron", "veyron/foo", "veyron/bar", "google", "google/foo", "google/bar", "foo", "foo/bar"}},
	}
	for _, d := range testdata {
		for _, b := range d.recognized {
			if err := br.Recognized(d.root, b); err != nil {
				return fmt.Errorf("%v.Recognized(%v, %q): got: %v, want nil", br, d.root, b, err)
			}
		}
		for _, b := range d.notRecognized {
			if err := matchesError(br.Recognized(d.root, b), "not a recognized root"); err != nil {
				return fmt.Errorf("%v.Recognized(%v, %q): %v", br, d.root, b, err)
			}
		}
	}
	return nil
}

func mkKey() security.PublicKey {
	s, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return security.NewECDSAPublicKey(&s.PublicKey)
}

func TestInMemoryBlessingRoots(t *testing.T) {
	br := NewInMemoryBlessingRoots()
	rootsTester := rootsTester{mkKey(), mkKey(), mkKey()}
	if err := rootsTester.testAdd(br); err != nil {
		t.Error(err)
	}
	if err := rootsTester.testRecognized(br); err != nil {
		t.Error(err)
	}
}

func TestPersistingBlessingRoots(t *testing.T) {
	newTempDir := func(name string) string {
		dir, err := ioutil.TempDir("", name)
		if err != nil {
			t.Fatal(err)
		}
		return dir
	}

	rootsTester := rootsTester{mkKey(), mkKey(), mkKey()}

	// Create a new persisting BlessingRoots and add key k1 as an authority over
	// blessings matching "veyron/...".
	dir := newTempDir("blessingstore")
	defer os.RemoveAll(dir)
	signer := newPrincipal(t)
	br, err := NewPersistingBlessingRoots(dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingRoots failed: %s", err)
	}

	if err := rootsTester.testAdd(br); err != nil {
		t.Error(err)
	}
	if err := rootsTester.testRecognized(br); err != nil {
		t.Error(err)
	}

	// Test that all mutations are appropriately reflected in a BlessingRoots
	// constructed from same directory and signer.
	br, err = NewPersistingBlessingRoots(dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingRoots failed: %s", err)
	}
	if err := rootsTester.testRecognized(br); err != nil {
		t.Error(err)
	}
}
