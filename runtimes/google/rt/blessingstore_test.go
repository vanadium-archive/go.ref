package rt

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

type storeTester struct {
	t                                         *testing.T
	bForAll, bForGoogle, bForVeyron, bDefault security.Blessings
}

func (t *storeTester) testAdd(s security.BlessingStore) error {
	var (
		p      = newPrincipal(t.t)
		bOther = blessSelf(t.t, p, "irrelevant")
	)
	testdata := []struct {
		blessings security.Blessings
		pattern   security.BlessingPattern
		wantErr   string
	}{
		{t.bForAll, "...", ""},
		{t.bForGoogle, "google/...", ""},
		{t.bForVeyron, "veyron", ""},
		{bOther, "...", "public key does not match"},
		{t.bForAll, "", "invalid BlessingPattern"},
		{t.bForAll, "foo...", "invalid BlessingPattern"},
		{t.bForAll, "...foo", "invalid BlessingPattern"},
		{t.bForAll, "foo/.../bar", "invalid BlessingPattern"},
	}
	for _, d := range testdata {
		if err := matchesError(s.Add(d.blessings, d.pattern), d.wantErr); err != nil {
			return fmt.Errorf("Add(%v, %q): %v", d.blessings, p, err)
		}
	}
	return nil
}

func (t *storeTester) testSetDefault(s security.BlessingStore, currentDefault security.Blessings) error {
	if got := s.Default(); !reflect.DeepEqual(got, currentDefault) {
		return fmt.Errorf("Default(): got: %v, want: %v", got, currentDefault)
	}
	testdata := []struct {
		blessings security.Blessings
		wantErr   string
	}{
		{t.bDefault, ""},
		{blessSelf(t.t, newPrincipal(t.t), "irrelevant"), "public key does not match"},
	}
	for _, d := range testdata {
		if err := matchesError(s.SetDefault(d.blessings), d.wantErr); err != nil {
			return fmt.Errorf("SetDefault(%v): %v", d.blessings, err)
		}
		if got, want := s.Default(), t.bDefault; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("Default(): got: %v, want: %v", got, want)
		}
	}
	return nil
}

func (t *storeTester) testForPeer(s security.BlessingStore) error {
	testdata := []struct {
		peers     []string
		blessings security.Blessings
	}{
		{nil, t.bForAll},
		{[]string{"foo"}, t.bForAll},
		{[]string{"google"}, unionOfBlessings(t.t, t.bForAll, t.bForGoogle)},
		{[]string{"veyron"}, unionOfBlessings(t.t, t.bForAll, t.bForVeyron)},
		{[]string{"google/foo"}, unionOfBlessings(t.t, t.bForAll, t.bForGoogle)},
		{[]string{"veyron/baz"}, t.bForAll},
		{[]string{"google/foo/bar"}, unionOfBlessings(t.t, t.bForAll, t.bForGoogle)},
		{[]string{"veyron/foo", "google"}, unionOfBlessings(t.t, t.bForAll, t.bForGoogle)},
		{[]string{"veyron", "google"}, unionOfBlessings(t.t, t.bForAll, t.bForGoogle, t.bForVeyron)},
	}
	for _, d := range testdata {
		if got, want := s.ForPeer(d.peers...), d.blessings; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("ForPeer(%v): got: %v, want: %v", d.peers, got, want)
		}
	}
	return nil
}

func newStoreTester(t *testing.T) (*storeTester, security.PublicKey) {
	var (
		// root principals
		v      = newPrincipal(t)
		g      = newPrincipal(t)
		veyron = blessSelf(t, v, "veyron")
		google = blessSelf(t, g, "google")

		// test principal
		p    = newPrincipal(t)
		pkey = p.PublicKey()
	)
	s := &storeTester{
		t:          t,
		bForAll:    bless(t, v, pkey, veyron, "alice"),
		bForGoogle: bless(t, g, pkey, google, "alice"),
		bDefault:   bless(t, g, pkey, google, "aliceDefault"),
	}
	s.bForVeyron = unionOfBlessings(t, s.bForAll, s.bForGoogle)
	return s, pkey
}

func TestInMemoryBlessingStore(t *testing.T) {
	tester, pkey := newStoreTester(t)
	s := NewInMemoryBlessingStore(pkey)
	if err := tester.testAdd(s); err != nil {
		t.Error(err)
	}
	if err := tester.testForPeer(s); err != nil {
		t.Error(err)
	}
	if err := tester.testSetDefault(s, tester.bForAll); err != nil {
		t.Error(err)
	}
}

func TestPersistingBlessingStore(t *testing.T) {
	newTempDir := func(name string) string {
		dir, err := ioutil.TempDir("", name)
		if err != nil {
			t.Fatal(err)
		}
		return dir
	}

	tester, pkey := newStoreTester(t)

	// Create a new persisting BlessingStore.
	dir := newTempDir("blessingstore")
	defer os.RemoveAll(dir)
	signer := newPrincipal(t)
	s, err := NewPersistingBlessingStore(pkey, dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingStore failed: %v", err)
	}

	if err := tester.testAdd(s); err != nil {
		t.Error(err)
	}
	if err := tester.testForPeer(s); err != nil {
		t.Error(err)
	}
	if err := tester.testSetDefault(s, tester.bForAll); err != nil {
		t.Error(err)
	}
	// Test that all mutations are appropriately reflected in a BlessingStore constructed
	// from same public key, directory and signer.
	s, err = NewPersistingBlessingStore(pkey, dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingStore failed: %v", err)
	}
	if err := tester.testForPeer(s); err != nil {
		t.Error(err)
	}
	if got, want := s.Default(), tester.bDefault; !reflect.DeepEqual(got, want) {
		t.Fatalf("Default(): got: %v, want: %v", got, want)
	}
}

func TestBlessingStoreDuplicates(t *testing.T) {
	roundTrip := func(blessings security.Blessings) security.Blessings {
		var b bytes.Buffer
		if err := vom.NewEncoder(&b).Encode(blessings); err != nil {
			t.Fatalf("could not VOM-Encode Blessings: %v", err)
		}
		var decodedBlessings security.Blessings
		if err := vom.NewDecoder(&b).Decode(&decodedBlessings); err != nil {
			t.Fatalf("could not VOM-Decode Blessings: %v", err)
		}
		return decodedBlessings
	}
	add := func(s security.BlessingStore, blessings security.Blessings, forPeers security.BlessingPattern) {
		if err := s.Add(blessings, forPeers); err != nil {
			t.Fatalf("Add(%v, %q) failed unexpectedly: %v", blessings, forPeers, err)
		}
	}
	var (
		// root principal
		// test blessings
		p     = newPrincipal(t)
		alice = blessSelf(t, p, "alice")

		pkey = p.PublicKey()
	)
	s := NewInMemoryBlessingStore(pkey)
	add(s, alice, "...")
	add(s, roundTrip(alice), "...")

	if got, want := s.ForPeer(), alice; !reflect.DeepEqual(got, want) {
		t.Fatalf("ForPeer(): got: %v, want: %v", got, want)
	}
}
