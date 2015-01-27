package security

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"v.io/core/veyron2/security"
)

type rootsTester [3]security.PublicKey

func newRootsTester() *rootsTester {
	var tester rootsTester
	var err error
	for idx := range tester {
		if tester[idx], _, err = NewPrincipalKey(); err != nil {
			panic(err)
		}
	}
	return &tester
}

func (t *rootsTester) add(br security.BlessingRoots) error {
	testdata := []struct {
		root    security.PublicKey
		pattern security.BlessingPattern
	}{
		{t[0], "veyron/..."},
		{t[1], "google/foo/..."},
		{t[0], "google/$"},
	}
	for _, d := range testdata {
		if err := br.Add(d.root, d.pattern); err != nil {
			return fmt.Errorf("Add(%v, %q) failed: %s", d.root, d.pattern, err)
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
		{
			root:          t[0],
			recognized:    []string{"veyron", "veyron/foo", "veyron/foo/bar", "google"},
			notRecognized: []string{"google/foo", "foo", "foo/bar"},
		},
		{
			root:          t[1],
			recognized:    []string{"google", "google/foo", "google/foo/bar"},
			notRecognized: []string{"google/bar", "veyron", "veyron/foo", "foo", "foo/bar"},
		},
		{
			root:          t[2],
			recognized:    []string{},
			notRecognized: []string{"veyron", "veyron/foo", "veyron/bar", "google", "google/foo", "google/bar", "foo", "foo/bar"},
		},
	}
	for _, d := range testdata {
		for _, b := range d.recognized {
			if err := br.Recognized(d.root, b); err != nil {
				return fmt.Errorf("Recognized(%v, %q): got: %v, want nil", d.root, b, err)
			}
		}
		for _, b := range d.notRecognized {
			if err := matchesError(br.Recognized(d.root, b), "not a recognized root"); err != nil {
				return fmt.Errorf("Recognized(%v, %q): %v", d.root, b, err)
			}
		}
	}
	return nil
}

func TestBlessingRoots(t *testing.T) {
	p, err := NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	tester := newRootsTester()
	if err := tester.add(p.Roots()); err != nil {
		t.Fatal(err)
	}
	if err := tester.testRecognized(p.Roots()); err != nil {
		t.Fatal(err)
	}
}

func TestBlessingRootsPersistence(t *testing.T) {
	dir, err := ioutil.TempDir("", "TestBlessingRootsPersistence")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	tester := newRootsTester()
	p, err := CreatePersistentPrincipal(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tester.add(p.Roots()); err != nil {
		t.Error(err)
	}
	if err := tester.testRecognized(p.Roots()); err != nil {
		t.Error(err)
	}
	// Recreate the principal (and thus BlessingRoots)
	p2, err := LoadPersistentPrincipal(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tester.testRecognized(p2.Roots()); err != nil {
		t.Error(err)
	}
}
