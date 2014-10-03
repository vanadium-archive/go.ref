package rt

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/security"
)

type storeTester struct {
	t                                         *testing.T
	bForAll, bForGoogle, bForVeyron, bDefault security.Blessings
}

func (t *storeTester) testSet(s security.BlessingStore) error {
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
		_, err := s.Set(d.blessings, d.pattern)
		if merr := matchesError(err, d.wantErr); merr != nil {
			return fmt.Errorf("Set(%v, %q): %v", d.blessings, p, merr)
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
	s := newInMemoryBlessingStore(pkey)
	if err := tester.testSet(s); err != nil {
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
	s, err := newPersistingBlessingStore(pkey, dir, signer)
	if err != nil {
		t.Fatalf("newPersistingBlessingStore failed: %v", err)
	}

	if err := tester.testSet(s); err != nil {
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
	s, err = newPersistingBlessingStore(pkey, dir, signer)
	if err != nil {
		t.Fatalf("newPersistingBlessingStore failed: %v", err)
	}
	if err := tester.testForPeer(s); err != nil {
		t.Error(err)
	}
	if got, want := s.Default(), tester.bDefault; !reflect.DeepEqual(got, want) {
		t.Fatalf("Default(): got: %v, want: %v", got, want)
	}
}

func TestBlessingStoreSetOverridesOldSetting(t *testing.T) {
	var (
		p     = newPrincipal(t)
		alice = blessSelf(t, p, "alice")
		bob   = blessSelf(t, p, "bob")
		s     = newInMemoryBlessingStore(p.PublicKey())
	)
	// Set(alice, "alice")
	// Set(bob, "alice/...")
	// So, {alice, bob} is shared with "alice", whilst {bob} is shared with "alice/tv"
	if _, err := s.Set(alice, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(bob, "alice/..."); err != nil {
		t.Fatal(err)
	}
	if got, want := s.ForPeer("alice"), unionOfBlessings(t, alice, bob); !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
	if got, want := s.ForPeer("alice/friend"), bob; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}

	// Clear out the blessing associated with "alice".
	// Now, bob should be shared with both alice and alice/friend.
	if _, err := s.Set(nil, "alice"); err != nil {
		t.Fatal(err)
	}
	if got, want := s.ForPeer("alice"), bob; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
	if got, want := s.ForPeer("alice/friend"), bob; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}

	// Clearing out an association that doesn't exist should have no effect.
	if _, err := s.Set(nil, "alice/enemy"); err != nil {
		t.Fatal(err)
	}
	if got, want := s.ForPeer("alice"), bob; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
	if got, want := s.ForPeer("alice/friend"), bob; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}

	// Clear everything
	if _, err := s.Set(nil, "alice/..."); err != nil {
		t.Fatal(err)
	}
	if got := s.ForPeer("alice"); got != nil {
		t.Errorf("Got %v, want nil", got)
	}
	if got := s.ForPeer("alice/friend"); got != nil {
		t.Errorf("Got %v, want nil", got)
	}
}

func TestBlessingStoreSetReturnsOldValue(t *testing.T) {
	var (
		p     = newPrincipal(t)
		alice = blessSelf(t, p, "alice")
		bob   = blessSelf(t, p, "bob")
		s     = newInMemoryBlessingStore(p.PublicKey())
	)
	if old, err := s.Set(alice, "..."); old != nil || err != nil {
		t.Errorf("Got (%v, %v)", old, err)
	}
	if old, err := s.Set(alice, "..."); !reflect.DeepEqual(old, alice) || err != nil {
		t.Errorf("Got (%v, %v) want (%v, nil)", old, err, alice)
	}
	if old, err := s.Set(bob, "..."); !reflect.DeepEqual(old, alice) || err != nil {
		t.Errorf("Got (%v, %v) want (%v, nil)", old, err, alice)
	}
	if old, err := s.Set(nil, "..."); !reflect.DeepEqual(old, bob) || err != nil {
		t.Errorf("Got (%v, %v) want (%v, nil)", old, err, bob)
	}
}
