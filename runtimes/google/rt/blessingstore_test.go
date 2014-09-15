package rt

import (
	"bytes"
	"io/ioutil"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	isecurity "veyron/runtimes/google/security"
	"veyron2/security"
	"veyron2/vom"
)

var (
	cVeyron = newChain("veyron")
	cGoogle = newChain("google")
)

func newChain(name string) security.PrivateID {
	id, err := isecurity.NewPrivateID(name, nil)
	if err != nil {
		panic(err)
	}
	return id
}

func newSetPublicID(ids ...security.PublicID) security.PublicID {
	id, err := isecurity.NewSetPublicID(ids...)
	if err != nil {
		panic(err)
	}
	return id
}

func bless(blessee security.PublicID, blessor security.PrivateID, name string, caveats []security.Caveat) security.PublicID {
	blessed, err := blessor.Bless(blessee, name, 5*time.Minute, caveats)
	if err != nil {
		panic(err)
	}
	return blessed
}

func verifyBlessingsAndPublicKey(blessings security.PublicID, wantBlessings []string, wantPublicKey security.PublicKey) bool {
	if blessings == nil {
		return len(wantBlessings) == 0
	}
	gotBlessings := blessings.Names()
	sort.Strings(gotBlessings)
	sort.Strings(wantBlessings)
	return reflect.DeepEqual(gotBlessings, wantBlessings) && reflect.DeepEqual(blessings.PublicKey(), wantPublicKey)
}

func TestStoreSetters(t *testing.T) {
	matchesErrorPattern := func(err error, pattern string) bool {
		if (len(pattern) == 0) != (err == nil) {
			return false
		}
		return err == nil || strings.Index(err.Error(), pattern) >= 0
	}
	var (
		// test principals
		cAlice       = newChain("alice")
		cAlicePub    = cAlice.PublicID()
		cBobPub      = newChain("bob").PublicID()
		cVeyronAlice = bless(cAlice.PublicID(), cVeyron, "alice", nil)
		sAlice       = newSetPublicID(cAlicePub, cVeyronAlice)

		pkey = cAlicePub.PublicKey()
	)

	s := NewInMemoryBlessingStore(pkey)

	testDataForAdd := []struct {
		blessings []security.PublicID
		patterns  []security.BlessingPattern
		wantErr   string
	}{
		{
			blessings: []security.PublicID{cAlicePub, cVeyronAlice, sAlice},
			patterns:  []security.BlessingPattern{"...", "foo", "foo/bar"},
		},
		{
			blessings: []security.PublicID{cBobPub},
			patterns:  []security.BlessingPattern{"...", "foo", "foo/bar"},
			wantErr:   "public key does not match",
		},
		{
			blessings: []security.PublicID{cAlicePub, cVeyronAlice, sAlice},
			patterns:  []security.BlessingPattern{"", "foo...", "...foo", "/bar", "foo/", "foo/.../bar"},
			wantErr:   "invalid blessing pattern",
		},
	}
	for _, d := range testDataForAdd {
		for _, b := range d.blessings {
			for _, p := range d.patterns {
				if got := s.Add(b, p); !matchesErrorPattern(got, d.wantErr) {
					t.Errorf("%v.Add(%v, %q): got error: %v, want to match: %q", s, b, p, got, d.wantErr)
				}
			}
		}
	}
	testDataForSetDefault := []struct {
		blessings []security.PublicID
		wantErr   string
	}{
		{
			blessings: []security.PublicID{cAlicePub, cVeyronAlice, sAlice},
		},
		{
			blessings: []security.PublicID{cBobPub},
			wantErr:   "public key does not match",
		},
	}
	for _, d := range testDataForSetDefault {
		for _, b := range d.blessings {
			if got := s.SetDefault(b); !matchesErrorPattern(got, d.wantErr) {
				t.Errorf("%v.SetDefault(%v): got error: %v, want to match: %q", s, b, got, d.wantErr)
			}
		}
	}
}

func TestStoreGetters(t *testing.T) {
	add := func(s security.BlessingStore, blessings security.PublicID, forPeers security.BlessingPattern) {
		if err := s.Add(blessings, forPeers); err != nil {
			t.Fatalf("%v.Add(%v, %q) failed unexpectedly: %s", s, blessings, forPeers, err)
		}
	}
	var (
		// test principals
		cAlice       = newChain("alice")
		cVeyronAlice = bless(cAlice.PublicID(), cVeyron, "alice", nil)
		cGoogleAlice = bless(cAlice.PublicID(), cGoogle, "alice", nil)
		sAlice       = newSetPublicID(cVeyronAlice, cGoogleAlice)

		pkey = cAlice.PublicID().PublicKey()
	)

	// Create a new BlessingStore for Alice and add her blessings to it.
	s := NewInMemoryBlessingStore(pkey)

	add(s, cVeyronAlice, "veyron/foo/...")
	add(s, cGoogleAlice, "veyron/bar")
	add(s, sAlice, "google")

	// Test ForPeer
	testDataForPeer := []struct {
		peerBlessings []string
		blessings     []string
	}{
		{nil, nil},
		{[]string{"foo"}, nil},
		{[]string{"google"}, []string{"veyron/alice", "google/alice"}},
		{[]string{"veyron"}, []string{"veyron/alice", "google/alice"}},
		{[]string{"google/foo"}, nil},
		{[]string{"veyron/baz"}, nil},
		{[]string{"veyron/foo"}, []string{"veyron/alice"}},
		{[]string{"veyron/bar"}, []string{"google/alice"}},
		{[]string{"foo", "veyron/bar"}, []string{"google/alice"}},
		{[]string{"veyron/foo/bar", "veyron/bar"}, []string{"veyron/alice", "google/alice"}},
		{[]string{"veyron/foo/bar", "veyron/bar/baz"}, []string{"veyron/alice"}},
	}
	for _, d := range testDataForPeer {
		if got := s.ForPeer(d.peerBlessings...); !verifyBlessingsAndPublicKey(got, d.blessings, pkey) {
			t.Errorf("%v.ForPeer(%v): got: %q, want Blessings: %q", s, d.peerBlessings, got, d.blessings)
		}
	}

	// Test Default
	// Default should return nil as SetDefault has not been invoked and no blessing
	// has been marked for all peers.
	if got := s.Default(); got != nil {
		t.Errorf("%v.Default(): got: %v, want: nil", s, got)
	}

	// Mark the blessings: cVeyronAlice and cGoogleAlice for all peers, and check that
	// Default returns the union of those blessings
	add(s, cVeyronAlice, security.AllPrincipals)
	add(s, cGoogleAlice, security.AllPrincipals)
	defaultBlessings := []string{"veyron/alice", "google/alice"}
	if got := s.Default(); !verifyBlessingsAndPublicKey(got, defaultBlessings, pkey) {
		t.Errorf("%v.Default(): got: %v, want Blessings: %v", s, got, defaultBlessings)
	}

	// Set the blessing cVeyronAlice as default and check that Default returns it.
	if err := s.SetDefault(cVeyronAlice); err != nil {
		t.Fatalf("%v.SetDefault(%v) failed: %s", s, cVeyronAlice, err)
	}
	if got, want := s.Default(), cVeyronAlice; !reflect.DeepEqual(got, want) {
		t.Errorf("%v.Default(): got: %v, want: %v", s, got, want)
	}
}

func TestBlessingStoreDuplicates(t *testing.T) {
	roundTrip := func(blessings security.PublicID) security.PublicID {
		var b bytes.Buffer
		if err := vom.NewEncoder(&b).Encode(blessings); err != nil {
			t.Fatalf("could not VOM-Encode Blessings: %s", err)
		}
		var decodedBlessings security.PublicID
		if err := vom.NewDecoder(&b).Decode(&decodedBlessings); err != nil {
			t.Fatalf("could not VOM-Decode Blessings: %s", err)
		}
		return decodedBlessings
	}
	add := func(s security.BlessingStore, blessings security.PublicID, forPeers security.BlessingPattern) {
		if err := s.Add(blessings, forPeers); err != nil {
			t.Fatalf("%v.Add(%v, %q) failed unexpectedly: %s", s, blessings, forPeers, err)
		}
	}
	var (
		// test principals
		cAlice       = newChain("alice")
		cVeyronAlice = bless(cAlice.PublicID(), cVeyron, "alice", nil)

		pkey = cAlice.PublicID().PublicKey()
	)

	// Create a new BlessingStore add the blessings cVeyronAlice to it twice.
	s := NewInMemoryBlessingStore(pkey)
	add(s, cVeyronAlice, "google")
	add(s, roundTrip(cVeyronAlice), "google/foo")

	peer := "google"
	wantBlessings := []string{"veyron/alice"}
	if got := s.ForPeer(peer); !verifyBlessingsAndPublicKey(got, wantBlessings, pkey) {
		t.Errorf("%v.ForPeer(%v): got: %q, want Blessings: %q", s, peer, got, wantBlessings)
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

	var (
		signer = newChain("signer")

		cAlice       = newChain("alice")
		cVeyronAlice = bless(cAlice.PublicID(), cVeyron, "alice", nil)
		cGoogleAlice = bless(cAlice.PublicID(), cGoogle, "alice", nil)

		pkey = cAlice.PublicID().PublicKey()
	)

	// Create a new persisting BlessingStore.
	dir := newTempDir("blessingstore")
	defer os.RemoveAll(dir)

	s, err := NewPersistingBlessingStore(pkey, dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingStore failed: %s", err)
	}
	if err := s.Add(cVeyronAlice, "veyron/..."); err != nil {
		t.Fatalf("%v.Add(%v, ...) failed: %s", s, cVeyronAlice, err)
	}
	if err := s.SetDefault(cGoogleAlice); err != nil {
		t.Fatalf("%v.SetDefault(%v) failed: %s", s, cGoogleAlice, err)
	}

	// Test that all mutations are appropriately reflected in a BlessingStore constructed
	// from same public key, directory and signer.
	s, err = NewPersistingBlessingStore(pkey, dir, signer)
	if err != nil {
		t.Fatalf("NewPersistingBlessingStore failed: %s", err)
	}

	if got, want := s.PublicKey(), pkey; !reflect.DeepEqual(got, want) {
		t.Errorf("%v.PublicKey(): got: %v, want: %v", s, got, want)
	}

	testDataForPeer := []struct {
		peerBlessings []string
		blessings     []string
	}{
		{peerBlessings: nil, blessings: nil},
		{peerBlessings: []string{"google"}, blessings: nil},
		{peerBlessings: []string{"veyron"}, blessings: []string{"veyron/alice"}},
		{peerBlessings: []string{"veyron/foo"}, blessings: []string{"veyron/alice"}},
		{peerBlessings: []string{"google", "veyron/foo"}, blessings: []string{"veyron/alice"}},
	}
	for _, d := range testDataForPeer {
		if got := s.ForPeer(d.peerBlessings...); !verifyBlessingsAndPublicKey(got, d.blessings, pkey) {
			t.Errorf("%v.ForPeer(%s): got: %q, want Blessings: %q", s, d.peerBlessings, got, d.blessings)
		}
	}

	if got, want := s.Default(), cGoogleAlice; !reflect.DeepEqual(got, want) {
		t.Errorf("%v.Default(): got: %v, want: %v", s, got, want)
	}

	// Test that constructing a BlesssingStore from the same directory and signer, but for a different
	// publicKey fails
	pkey = newChain("irrelevant").PublicID().PublicKey()
	_, err = NewPersistingBlessingStore(pkey, dir, signer)
	if err == nil {
		t.Fatalf("NewPersistingBlessingStore(%v, %v, %v) passed uneexpectedly", pkey, dir, signer)
	}
}

func init() {
	isecurity.TrustIdentityProviders(cVeyron)
	isecurity.TrustIdentityProviders(cGoogle)
}
