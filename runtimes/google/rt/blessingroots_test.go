package rt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"veyron2/security"
)

type s []string

type tester struct {
	k1, k2, k3 security.PublicKey
}

func (t *tester) testAdd(br security.BlessingRoots) error {
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

func (t *tester) testRecognized(br security.BlessingRoots) error {
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

func matchesError(err error, pattern string) error {
	retErr := fmt.Errorf("got error: %v, want to match: %v", err, pattern)
	if (len(pattern) == 0) != (err == nil) {
		return retErr
	}
	if (err != nil) && (strings.Index(err.Error(), pattern) < 0) {
		return retErr
	}
	return nil
}

func TestInMemoryBlessingRoots(t *testing.T) {
	br := NewInMemoryBlessingRoots()
	tester := tester{mkKey(), mkKey(), mkKey()}
	if err := tester.testAdd(br); err != nil {
		t.Error(err)
	}
	if err := tester.testRecognized(br); err != nil {
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

	tester := tester{mkKey(), mkKey(), mkKey()}

	// Create a new persisting BlessingRoots and add key k1 as an authority over
	// blessings matching "veyron/...".
	dir := newTempDir("blessingstore")
	defer os.RemoveAll(dir)
	signer := newChain("signer")
	br, err := NewPersistingBlessingRoots(dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingRoots failed: %s", err)
	}

	if err := tester.testAdd(br); err != nil {
		t.Error(err)
	}
	if err := tester.testRecognized(br); err != nil {
		t.Error(err)
	}

	// Test that all mutations are appropriately reflected in a BlessingRoots
	// constructed from same directory and signer.
	br, err = NewPersistingBlessingRoots(dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingRoots failed: %s", err)
	}

	if err := tester.testAdd(br); err != nil {
		t.Error(err)
	}
	if err := tester.testRecognized(br); err != nil {
		t.Error(err)
	}
}
