package security

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/security"
)

type storeTester struct {
	forAll, forFoo, forBar, def security.Blessings
	other                       security.Blessings // Blessings bound to a different principal.
}

func (t *storeTester) testSet(s security.BlessingStore) error {
	testdata := []struct {
		blessings security.Blessings
		pattern   security.BlessingPattern
		wantErr   string
	}{
		{t.forAll, "...", ""},
		{t.forFoo, "foo/...", ""},
		{t.forBar, "bar", ""},
		{t.other, "...", "public key does not match"},
		{t.forAll, "", "invalid BlessingPattern"},
		{t.forAll, "foo...", "invalid BlessingPattern"},
		{t.forAll, "...foo", "invalid BlessingPattern"},
		{t.forAll, "foo/.../bar", "invalid BlessingPattern"},
	}
	for _, d := range testdata {
		_, err := s.Set(d.blessings, d.pattern)
		if merr := matchesError(err, d.wantErr); merr != nil {
			return fmt.Errorf("Set(%v, %q): %v", d.blessings, d.pattern, merr)
		}
	}
	return nil
}

func (t *storeTester) testSetDefault(s security.BlessingStore, currentDefault security.Blessings) error {
	if got := s.Default(); !reflect.DeepEqual(got, currentDefault) {
		return fmt.Errorf("Default(): got: %v, want: %v", got, currentDefault)
	}
	if err := s.SetDefault(t.def); err != nil {
		return fmt.Errorf("SetDefault(%v): %v", t.def, err)
	}
	if got, want := s.Default(), t.def; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("Default returned %v, want %v", got, want)
	}
	// Changing default to an invalid blessing should not affect the existing default.
	if err := matchesError(s.SetDefault(t.other), "public key does not match"); err != nil {
		return err
	}
	if got, want := s.Default(), t.def; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("Default returned %v, want %v", got, want)
	}
	return nil
}

func (t *storeTester) testForPeer(s security.BlessingStore) error {
	testdata := []struct {
		peers     []string
		blessings security.Blessings
	}{
		{nil, t.forAll},
		{[]string{"baz"}, t.forAll},
		{[]string{"foo"}, unionOfBlessings(t.forAll, t.forFoo)},
		{[]string{"bar"}, unionOfBlessings(t.forAll, t.forBar)},
		{[]string{"foo/foo"}, unionOfBlessings(t.forAll, t.forFoo)},
		{[]string{"bar/baz"}, t.forAll},
		{[]string{"foo/foo/bar"}, unionOfBlessings(t.forAll, t.forFoo)},
		{[]string{"bar/foo", "foo"}, unionOfBlessings(t.forAll, t.forFoo)},
		{[]string{"bar", "foo"}, unionOfBlessings(t.forAll, t.forFoo, t.forBar)},
	}
	for _, d := range testdata {
		if got, want := s.ForPeer(d.peers...), d.blessings; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("ForPeer(%v): got: %v, want: %v", d.peers, got, want)
		}
	}
	return nil
}

func newStoreTester(blessed security.Principal) *storeTester {
	var (
		blessing = func(root, extension string) security.Blessings {
			blesser, err := NewPrincipal()
			if err != nil {
				panic(err)
			}
			blessing, err := blesser.Bless(blessed.PublicKey(), blessSelf(blesser, root), extension, security.UnconstrainedUse())
			if err != nil {
				panic(err)
			}
			return blessing
		}
	)
	pother, err := NewPrincipal()
	if err != nil {
		panic(err)
	}

	s := &storeTester{}
	s.forAll = blessing("bar", "alice")
	s.forFoo = blessing("foo", "alice")
	s.forBar = unionOfBlessings(s.forAll, s.forFoo)
	s.def = blessing("default", "alice")
	s.other = blessSelf(pother, "other")
	return s
}

func TestBlessingStore(t *testing.T) {
	p, err := NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	tester := newStoreTester(p)
	s := p.BlessingStore()
	if err := tester.testSet(s); err != nil {
		t.Error(err)
	}
	if err := tester.testForPeer(s); err != nil {
		t.Error(err)
	}
	if err := tester.testSetDefault(s, tester.forAll); err != nil {
		t.Error(err)
	}
}

func TestBlessingStorePersistence(t *testing.T) {
	dir, err := ioutil.TempDir("", "TestPersistingBlessingStore")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	p, err := CreatePersistentPrincipal(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	tester := newStoreTester(p)
	s := p.BlessingStore()

	if err := tester.testSet(s); err != nil {
		t.Error(err)
	}
	if err := tester.testForPeer(s); err != nil {
		t.Error(err)
	}
	if err := tester.testSetDefault(s, tester.forAll); err != nil {
		t.Error(err)
	}

	// Recreate the BlessingStore from the directory.
	p2, err := LoadPersistentPrincipal(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = p2.BlessingStore()
	if err := tester.testForPeer(s); err != nil {
		t.Error(err)
	}
	if got, want := s.Default(), tester.def; !reflect.DeepEqual(got, want) {
		t.Fatalf("Default(): got: %v, want: %v", got, want)
	}
}

func TestBlessingStoreSetOverridesOldSetting(t *testing.T) {
	p, err := NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	var (
		alice = blessSelf(p, "alice")
		bob   = blessSelf(p, "bob")
		s     = p.BlessingStore()
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
	if got, want := s.ForPeer("alice"), unionOfBlessings(alice, bob); !reflect.DeepEqual(got, want) {
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
	p, err := NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	var (
		alice = blessSelf(p, "alice")
		bob   = blessSelf(p, "bob")
		s     = p.BlessingStore()
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
