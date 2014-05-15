package security

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"veyron/runtimes/google/security/caveat"
	"veyron/runtimes/google/security/wire"
	"veyron2/security"
)

func TestNameAndAuth(t *testing.T) {
	var (
		cUnknownAlice    = newChain("alice").PublicID()
		cTrustedAlice    = bless(cUnknownAlice, veyronChain, "alice", nil)
		cMistrustedAlice = bless(cUnknownAlice, newChain("veyron"), "alice", nil)

		tUntrustedAlice  = newTree("alice").PublicID()
		tTrustedAlice    = bless(tUntrustedAlice, veyronTree, "alice", nil)
		tMistrustedAlice = bless(tUntrustedAlice, newTree("veyron"), "alice", nil)
	)
	testdata := []struct {
		id             security.PublicID
		name, authName string
	}{
		{id: cUnknownAlice, name: "untrusted/alice", authName: "untrusted/alice"},
		{id: cTrustedAlice, name: "veyron/alice", authName: "veyron/alice"},
		{id: cMistrustedAlice, name: "untrusted/veyron/alice", authName: ""},
		{id: tUntrustedAlice, name: "untrusted/alice", authName: "untrusted/alice"},
		{id: tTrustedAlice, name: "untrusted/alice#veyron/alice", authName: "untrusted/alice#veyron/alice"},
		{id: tMistrustedAlice, name: "untrusted/alice#untrusted/veyron/alice", authName: "untrusted/alice"},
	}
	for _, d := range testdata {
		if got, want := fmt.Sprintf("%s", d.id), d.name; got != want {
			t.Errorf("Got %q(%T) want %q", d.id, d.id, d.name)
		}
		authID, err := d.id.Authorize(NewContext(ContextArgs{}))
		if (authID != nil) == (err != nil) {
			t.Errorf("%q.Authorize returned (%v, %v), exactly one return value must be nil", d.id, authID, err)
			continue
		}
		if err := verifyAuthorizedID(d.id, authID, d.authName); err != nil {
			t.Error(err)
		}
	}
}

func TestMatch(t *testing.T) {
	type matchInstance struct {
		pattern security.PrincipalPattern
		want    bool
	}
	testdata := []struct {
		id        security.PublicID
		matchData []matchInstance
	}{
		{
			// self-signed alice chain, not a trusted identity provider so should only match "*"
			id: newChain("alice").PublicID(),
			matchData: []matchInstance{
				{pattern: "*", want: true},
				{pattern: "alice", want: false},
				{pattern: "alice/*", want: false},
			},
		},
		{
			// self-blessed alice tree, not a trusted identity provider so should only match "*"
			id: newTree("alice").PublicID(),
			matchData: []matchInstance{
				{pattern: "*", want: true},
				{pattern: "alice", want: false},
				{pattern: "alice/*", want: false},
			},
		},
		{
			// veyron/alice: rooted in the trusted "veyron" identity provider
			id: bless(newChain("immaterial").PublicID(), veyronChain, "alice", nil),
			matchData: []matchInstance{
				{pattern: "*", want: true},
				{pattern: "veyron/*", want: true},
				{pattern: "veyron/alice", want: true},
				{pattern: "veyron/alice/*", want: true},
				{pattern: "veyron/alice/TV", want: true},
				{pattern: "veyron", want: false},
				{pattern: "veyron/ali", want: false},
				{pattern: "veyron/aliced", want: false},
				{pattern: "veyron/bob", want: false},
				{pattern: "google/alice", want: false},
			},
		},
		{
			// alice#veyron/alice#google/alice: two trusted identity providers
			id: bless(bless(newTree("alice").PublicID(), veyronTree, "alice", nil), googleTree, "alice", nil),
			matchData: []matchInstance{
				{pattern: "*", want: true},
				// Since alice is not a trusted identity
				// provider, the tree's self-blessed identity
				// should not match "alice/*"
				{pattern: "alice", want: false},
				{pattern: "alice/*", want: false},
				{pattern: "veyron/*", want: true},
				{pattern: "veyron/alice", want: true},
				{pattern: "veyron/alice/TV", want: true},
				{pattern: "veyron/alice/*", want: true},
				{pattern: "ali", want: false},
				{pattern: "aliced", want: false},
				{pattern: "veyron", want: false},
				{pattern: "veyron/ali", want: false},
				{pattern: "veyron/aliced", want: false},
				{pattern: "veyron/bob", want: false},
				{pattern: "google/alice", want: true},
				{pattern: "google/alice/TV", want: true},
				{pattern: "google/alice/*", want: true},
			},
		},
	}
	for _, d := range testdata {
		for _, m := range d.matchData {
			if got := d.id.Match(m.pattern); got != m.want {
				t.Errorf("%q.Match(%s), Got %t, want %t", d.id, m.pattern, got, m.want)
			}
		}
	}
}

func TestExpiredIdentity(t *testing.T) {
	testdata := []struct {
		blessor    security.PrivateID
		blessee    security.PublicID
		authorized string
	}{
		{veyronChain, newChain("alice").PublicID(), ""},
		{veyronTree, newTree("alice").PublicID(), "untrusted/alice"},
	}
	for _, d := range testdata {
		id, err := d.blessor.Bless(d.blessee, "alice", time.Millisecond, nil)
		if err != nil {
			t.Errorf("%q.Bless(%q, ...) failed: %v", d.blessor, d.blessee, err)
			continue
		}
		time.Sleep(time.Millisecond)
		authorizedID, _ := id.Authorize(NewContext(ContextArgs{}))
		if err := verifyAuthorizedID(id, authorizedID, d.authorized); err != nil {
			t.Error(err)
		}
	}
}

func TestTamperedIdentityChain(t *testing.T) {
	alice := newChain("alice").PublicID().(*chainPublicID)
	// Tamper with the alice's public key
	nCerts := len(alice.certificates)
	pKey, _ := alice.certificates[nCerts-1].PublicKey.Decode()
	pKey.Y.SetInt64(1)
	if err := alice.certificates[nCerts-1].PublicKey.Encode(pKey); err != nil {
		t.Fatalf("Failed publicKey.Encode:%v", err)
	}
	if _, err := roundTrip(alice); err != wire.ErrNoIntegrity {
		t.Errorf("Got %v want %v from roundTrip(%v)", err, wire.ErrNoIntegrity, alice)
	}
}

func TestTamperedIdentityTree(t *testing.T) {
	alice := newTree("alice").PublicID().(*treePublicID)
	// Tamper with the alice's public key
	alice.publicKey.Y.SetInt64(1)
	// Check that integrity verification fails
	if _, err := roundTrip(alice); err != wire.ErrNoIntegrity {
		t.Errorf("Got %v want %v from roundTrip(%v)", err, wire.ErrNoIntegrity, alice)
	}
}

func TestBless(t *testing.T) {
	var (
		cAlice       = newChain("alice")
		cBob         = newChain("bob")
		cVeyronAlice = derive(bless(cAlice.PublicID(), veyronChain, "alice", nil), cAlice)

		tAlice       = newTree("alice")
		tBob         = newTree("bob")
		tVeyronAlice = derive(bless(tAlice.PublicID(), veyronTree, "alice", nil), tAlice)
	)
	testdata := []struct {
		blessor  security.PrivateID
		blessee  security.PublicID
		blessing string // name provided to security.PublicID.Bless
		blessed  string // name of the blessed identity. Empty if the Bless operation should have failed
		err      string
	}{
		{
			blessor: veyronChain,
			blessee: cAlice.PublicID(),
			err:     `invalid blessing name:""`,
		},
		{
			blessor:  veyronChain,
			blessee:  cAlice.PublicID(),
			blessing: "alice/bob",
			err:      `invalid blessing name:"alice/bob"`,
		},
		{
			blessor:  veyronChain,
			blessee:  cAlice.PublicID(),
			blessing: "alice",
			blessed:  "veyron/alice",
		},
		{
			blessor:  cVeyronAlice,
			blessee:  cBob.PublicID(),
			blessing: "friend_bob",
			blessed:  "veyron/alice/friend_bob",
		},
		{
			blessor:  cAlice,
			blessee:  cBob.PublicID(),
			blessing: "friend_bob",
			blessed:  "untrusted/alice/friend_bob",
		},
		{
			blessor: veyronTree,
			blessee: tAlice.PublicID(),
			err:     `invalid blessing name:""`,
		},
		{
			blessor:  veyronTree,
			blessee:  tAlice.PublicID(),
			blessing: "alice/bob",
			err:      `invalid blessing name:"alice/bob"`,
		},
		{
			blessor:  veyronTree,
			blessee:  tAlice.PublicID(),
			blessing: "alice",
			blessed:  "untrusted/alice#veyron/alice",
		},
		{
			blessor:  tVeyronAlice,
			blessee:  tBob.PublicID(),
			blessing: "friend_bob",
			blessed:  "untrusted/bob#untrusted/alice/friend_bob#veyron/alice/friend_bob",
		},
		{
			blessor:  googleTree,
			blessee:  tVeyronAlice.PublicID(),
			blessing: "googler",
			blessed:  "untrusted/alice#veyron/alice#google/googler",
		},
	}

	for _, d := range testdata {
		blessed, err := d.blessor.Bless(d.blessee, d.blessing, 1*time.Minute, nil)
		// Exaclty one of (blessed, err) should be nil
		if (blessed != nil) == (err != nil) {
			t.Errorf("%q.Bless(%q, %q, ...) returned (%v, %v): exactly one return value should be nil", d.blessor, d.blessee, d.blessing, blessed, err)
			continue
		}
		// If err != nil, should match d.err
		if err != nil {
			if err.Error() != d.err {
				t.Errorf("Got error [%s], want [%s] from %q.Bless(%q, %q, ...)", err, d.err, d.blessor, d.blessee, d.blessing)
			}
			continue
		}
		// If d.err is specified, then err should not have been nil
		if len(d.err) != 0 {
			t.Errorf("Got %q want error=%v from %q.Bless(%q, %q, ...)", blessed, d.err, d.blessor, d.blessee, d.blessing)
			continue
		}
		// Compare names
		if got, want := fmt.Sprintf("%s", blessed), d.blessed; got != want {
			t.Errorf("Got %q want %q from %q.Bless(%q, %q, ...)", got, want, d.blessor, d.blessee, d.blessing)
		}
		// Public keys should match for blessed and blessee
		if !reflect.DeepEqual(blessed.PublicKey(), d.blessee.PublicKey()) {
			t.Errorf("PublicKey mismatch in %q.Bless(%q, %q, ...)", d.blessor, d.blessee, d.blessing)
		}
		// Verify wire encoding of the blessed
		if _, err := roundTrip(blessed); err != nil {
			t.Errorf("roundTrip(%q) failed: %v (from %q.Bless(%q, %q, ...))", blessed, err, d.blessor, d.blessee, d.blessing)
		}
	}
}

func TestAuthorizeWithCaveats(t *testing.T) {
	var (
		// Alice's chain and tree identities
		pcAlice = newChain("alice")
		cAlice  = pcAlice.PublicID().(*chainPublicID)
		ptAlice = newTree("alice")
		tAlice  = ptAlice.PublicID().(*treePublicID)

		// veyron/alice/tv
		cVeyronAliceTV = bless(newChain("tv").PublicID(),
			derive(bless(cAlice, veyronChain, "alice", nil), pcAlice),
			"tv", nil).(*chainPublicID)
		tVeyronAliceTV = bless(newTree("tv").PublicID(),
			derive(bless(tAlice, veyronTree, "alice", nil), ptAlice),
			"tv", nil).(*treePublicID)

		// Some random server called bob
		bob = newChain("bob").PublicID()

		// Caveats
		// Can only call "Play" at the Google service
		cavOnlyPlayAtGoogle = methodRestrictionCaveat("google", []string{"Play"})
		// Can only talk to the "Google" service
		cavOnlyGoogle = peerIdentityCaveat("google")
		// Can only call the PublicProfile method on veyron/alice/*
		cavOnlyPublicProfile = methodRestrictionCaveat("veyron/alice/*", []string{"PublicProfile"})
	)

	type rpc struct {
		server security.PublicID
		method string
		// Expected output: exactly one should be non-empty
		authName, authErr string
	}
	testdata := []struct {
		client security.PublicID
		tests  []rpc
	}{
		// client has a chain identity
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyPlayAtGoogle),
			tests: []rpc{
				{server: bob, method: "Hello", authName: "veyron/alice"},
				{server: bob, authName: "veyron/alice"},
				{server: googleTree.PublicID(), method: "Hello", authErr: `caveat.MethodRestriction{"Play"} forbids invocation of method Hello`},
				{server: googleTree.PublicID(), method: "Play", authName: "veyron/alice"},
				{server: googleTree.PublicID(), authName: "veyron/alice"},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyGoogle),
			tests: []rpc{
				{server: bob, method: "Hello", authErr: `caveat.PeerIdentity{"google"} forbids RPCing with peer untrusted/bob`},
				{server: googleTree.PublicID(), method: "Hello", authName: "veyron/alice"},
				{server: googleTree.PublicID(), method: "Play", authName: "veyron/alice"},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", append(cavOnlyGoogle, cavOnlyPlayAtGoogle...)),
			tests: []rpc{
				{server: bob, method: "Hello", authErr: `caveat.PeerIdentity{"google"} forbids RPCing with peer untrusted/bob`},
				{server: googleTree.PublicID(), method: "Hello", authErr: `caveat.MethodRestriction{"Play"} forbids invocation of method Hello`},
				{server: googleTree.PublicID(), method: "Play", authName: "veyron/alice"},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyPublicProfile),
			tests: []rpc{
				{server: cVeyronAliceTV, method: "PrivateProfile", authErr: `caveat.MethodRestriction{"PublicProfile"} forbids invocation of method PrivateProfile`},
				{server: cVeyronAliceTV, method: "PublicProfile", authName: "veyron/alice"},
			},
		},
		// client has a tree identity
		{
			client: bless(tAlice, veyronTree, "alice", cavOnlyPlayAtGoogle),
			tests: []rpc{
				{server: bob, method: "Hello", authName: "untrusted/alice#veyron/alice"},
				{server: bob, authName: "untrusted/alice#veyron/alice"},
				{server: googleTree.PublicID(), method: "Hello", authName: "untrusted/alice"},
				{server: googleTree.PublicID(), method: "Play", authName: "untrusted/alice#veyron/alice"},
				{server: googleTree.PublicID(), authName: "untrusted/alice#veyron/alice"},
			},
		},
		{
			client: bless(tAlice, veyronTree, "alice", cavOnlyGoogle),
			tests: []rpc{
				{server: bob, method: "Hello", authName: "untrusted/alice"},
				{server: googleTree.PublicID(), method: "Hello", authName: "untrusted/alice#veyron/alice"},
				{server: googleTree.PublicID(), method: "Play", authName: "untrusted/alice#veyron/alice"},
			},
		},
		{
			client: bless(tAlice, veyronTree, "alice", append(cavOnlyGoogle, cavOnlyPlayAtGoogle...)),
			tests: []rpc{
				{server: bob, method: "Hello", authName: "untrusted/alice"},
				{server: googleTree.PublicID(), method: "Hello", authName: "untrusted/alice"},
				{server: googleTree.PublicID(), method: "Play", authName: "untrusted/alice#veyron/alice"},
			},
		},
		{
			client: bless(tAlice, veyronTree, "alice", cavOnlyPublicProfile),
			tests: []rpc{
				{server: tVeyronAliceTV, method: "PrivateProfile", authName: "untrusted/alice"},
				{server: tVeyronAliceTV, method: "PublicProfile", authName: "untrusted/alice#veyron/alice"},
			},
		},
	}
	for _, d := range testdata {
		// Validate that the client identity (with all its blessings) is valid for wire transmission.
		if _, err := roundTrip(d.client); err != nil {
			t.Errorf("roundTrip(%q): %v", d.client, err)
			continue
		}
		for _, test := range d.tests {
			if (len(test.authName) == 0) == (len(test.authErr) == 0) {
				t.Fatalf("Bad testdata. One of authName and authErr must be non-empty: %q, %+v", d.client, test)
			}
			ctx := NewContext(ContextArgs{LocalID: test.server, RemoteID: d.client, Method: test.method})
			authID, err := d.client.Authorize(ctx)
			if !matchesErrorPattern(err, test.authErr) {
				t.Errorf("%q.Authorize(%v) returned error %v, want to match %q", d.client, ctx, err, test.authErr)
			}
			if err := verifyAuthorizedID(d.client, authID, test.authName); err != nil {
				t.Errorf("%q.Authorize(%v) returned identity %v want %q", d.client, ctx, authID, test.authName)
			}
		}
	}
}

func TestAuthorizeWithThirdPartyCaveats(t *testing.T) {
	mkveyronchain := func(name string) security.PrivateID {
		base := newChain(name)
		return derive(bless(base.PublicID(), veyronChain, name, nil), base)
	}
	mkveyrontree := func(name string) security.PrivateID {
		base := newTree(name)
		return derive(bless(base.PublicID(), veyronTree, name, nil), base)
	}
	// Principals (type conversions just to protect against accidentally
	// calling the wrong factory function)
	var (
		alice  = mkveyronchain("alice")
		cBob   = mkveyronchain("bob").(*chainPrivateID)
		tBob   = mkveyrontree("bob").(*treePrivateID)
		cCarol = mkveyronchain("carol").(*chainPrivateID).PublicID()
		tCarol = mkveyrontree("carol").(*treePrivateID).PublicID()
	)
	// aliceProximityCaveat is a caveat that can only be minted by alice
	aliceProximityCaveat, err := caveat.NewPublicKeyCaveat("proximity", alice.PublicID(), "alice location")
	if err != nil {
		t.Fatal(err)
	}
	mintDischarge := func(id security.PrivateID, duration time.Duration, caveats []security.ServiceCaveat) security.ThirdPartyDischarge {
		d, err := id.MintDischarge(aliceProximityCaveat, duration, caveats)
		if err != nil {
			t.Fatalf("%q.MintDischarge failed: %v", id, err)
		}
		return d
	}
	// Discharges
	var (
		dAlice   = mintDischarge(alice, time.Minute, nil)
		dGoogle  = mintDischarge(alice, time.Minute, peerIdentityCaveat("google"))
		dExpired = mintDischarge(alice, 0, nil)
		dInvalid = mintDischarge(cBob, time.Minute, nil) // Invalid because carol cannot mint valid discharges for aliceProximityCaveat
	)
	// Contexts
	var (
		ctxEmpty = NewContext(ContextArgs{Debug: "ctxEmpty"})
		ctxAlice = NewContext(ContextArgs{
			Discharges: security.CaveatDischargeMap{dAlice.CaveatID(): dAlice},
			Debug:      "ctxAlice",
		})
		// Context containing the discharge dGoogle but the server is not a Google server, so
		// the service caveat is not satisfied
		ctxGoogleAtOther = NewContext(ContextArgs{
			Discharges: security.CaveatDischargeMap{dGoogle.CaveatID(): dGoogle},
			Debug:      "ctxGoogleAtOther",
		})
		// Context containing the discharge dGoogle at a google server.
		ctxGoogleAtGoogle = NewContext(ContextArgs{
			Discharges: security.CaveatDischargeMap{dGoogle.CaveatID(): dGoogle},
			LocalID:    googleChain.PublicID(),
			Debug:      "ctxGoogleAtGoogle",
		})
		ctxExpired = NewContext(ContextArgs{
			Discharges: security.CaveatDischargeMap{dExpired.CaveatID(): dExpired},
			Debug:      "ctxExpired",
		})
		ctxInvalid = NewContext(ContextArgs{
			Discharges: security.CaveatDischargeMap{dInvalid.CaveatID(): dInvalid},
			Debug:      "ctxInvalid",
		})
	)

	type want struct {
		// Exactly one of these should be non-empty
		name, err string
	}

	chaintests := map[security.Context]want{
		ctxEmpty:          want{err: "missing discharge"},
		ctxAlice:          want{name: "veyron/bob/friend"},
		ctxGoogleAtOther:  want{err: "forbids RPCing with peer"},
		ctxGoogleAtGoogle: want{name: "veyron/bob/friend"},
		ctxExpired:        want{err: "at this time"},
		ctxInvalid:        want{err: "invalid signature"},
	}
	treetests := map[security.Context]want{
		ctxEmpty:          want{name: "untrusted/carol#veyron/carol"},
		ctxAlice:          want{name: "untrusted/carol#veyron/carol#untrusted/bob/friend#veyron/bob/friend"},
		ctxGoogleAtOther:  want{name: "untrusted/carol#veyron/carol"},
		ctxGoogleAtGoogle: want{name: "untrusted/carol#veyron/carol#untrusted/bob/friend#veyron/bob/friend"},
		ctxExpired:        want{name: "untrusted/carol#veyron/carol"},
		ctxInvalid:        want{name: "untrusted/carol#veyron/carol"},
	}
	caveats := []security.ServiceCaveat{security.UniversalCaveat(aliceProximityCaveat)}
	testdata := []struct {
		id    security.PublicID
		tests map[security.Context]want
	}{
		{bless(cCarol, cBob, "friend", caveats), chaintests},
		{bless(tCarol, tBob, "friend", caveats), treetests},
	}
	for _, d := range testdata {
		if _, err := roundTrip(d.id); err != nil {
			t.Errorf("%q is not round-trippable: %v", d.id, d.id, err)
		}
		for ctx, want := range d.tests {
			if (len(want.name) == 0) == (len(want.err) == 0) {
				t.Fatalf("Bad testdata. One of (name, err) must be non-empty: %q, %v", d.id, ctx)
			}
			authID, err := d.id.Authorize(ctx)
			if !matchesErrorPattern(err, want.err) {
				t.Errorf("%q.Authorize(%v) returned error %v, want to match %q", d.id, ctx, err, want.err)
			}
			if err := verifyAuthorizedID(d.id, authID, want.name); err != nil {
				t.Errorf("%q.Authorize(%v) returned identity %v want %q", d.id, ctx, authID, want.name)
			}
		}
	}
}

func TestThirdPartyCaveatAccessors(t *testing.T) {
	mkTPCaveat := func(restriction string, id security.PublicID) security.ThirdPartyCaveat {
		tpCav, err := caveat.NewPublicKeyCaveat(restriction, id, "someLocation")
		if err != nil {
			t.Fatalf("NewPublicKeyCaveat(%q, %q, ...) failed: %v", restriction, id, err)
		}
		return tpCav
	}
	mintDischarge := func(caveat security.ThirdPartyCaveat, id security.PrivateID, caveats []security.ServiceCaveat) security.ThirdPartyDischarge {
		d, err := id.MintDischarge(caveat, time.Minute, caveats)
		if err != nil {
			t.Fatalf("%q.MintDischarge failed: %v", id, err)
		}
		return d
	}

	// Principals (type conversions just to protect against accidentally
	// calling the wrong factory function)
	var (
		alice = newChain("alice").(*chainPrivateID)
		cBob  = newChain("bob").(*chainPrivateID)
		tBob  = newTree("bob").(*treePrivateID)
	)
	// Caveats
	var (
		tpCavService   = security.ServiceCaveat{Service: "someService", Caveat: mkTPCaveat("foo", alice.PublicID())}
		tpCavUniversal = security.UniversalCaveat(mkTPCaveat("bar", alice.PublicID()))
		cav            = methodRestrictionCaveat("someService", nil)[0]
	)

	caveatsData := []struct {
		caveats           []security.ServiceCaveat
		thirdPartyCaveats []security.ServiceCaveat
	}{
		{caveats: nil, thirdPartyCaveats: nil},
		{caveats: []security.ServiceCaveat{cav}, thirdPartyCaveats: nil},
		{caveats: []security.ServiceCaveat{tpCavService}, thirdPartyCaveats: []security.ServiceCaveat{tpCavService}},
		{caveats: []security.ServiceCaveat{tpCavService, tpCavUniversal}, thirdPartyCaveats: []security.ServiceCaveat{tpCavService, tpCavUniversal}},
		{caveats: []security.ServiceCaveat{tpCavService, cav, tpCavUniversal}, thirdPartyCaveats: []security.ServiceCaveat{tpCavService, tpCavUniversal}},
	}
	testdata := []struct {
		privID security.PrivateID
		pubID  security.PublicID
	}{
		{privID: veyronChain, pubID: cBob.PublicID()},
		{privID: veyronTree, pubID: tBob.PublicID()},
	}
	for _, d := range testdata {
		for _, c := range caveatsData {
			// Test ThirdPartyCaveat accessors on security.PublicIDs.
			id := bless(d.pubID, d.privID, "irrelevant", c.caveats)
			if got, want := id.ThirdPartyCaveats(), c.thirdPartyCaveats; !reflect.DeepEqual(got, want) {
				t.Errorf("Test credential %q with caveats %+v: got ThirdPartyCaveats() = %+v, want %+v", id, c.caveats, got, want)
			}
			// Test ThirdPartyCaveat accessors on security.ThirdPartyCaveatDischarges.
			dis := mintDischarge(mkTPCaveat("baz", alice.PublicID()), d.privID, c.caveats)
			if got, want := dis.ThirdPartyCaveats(), c.thirdPartyCaveats; !reflect.DeepEqual(got, want) {
				t.Errorf("Test credential %q with caveats %+v: got ThirdPartyCaveats() = %+v, want %+v", dis, c.caveats, got, want)
			}
		}
	}
}

func TestBlessingChainAmplification(t *testing.T) {
	var (
		// alice has blessings from trusted identity providers google and veyron
		alice       = newChain("alice")
		googleAlice = derive(bless(alice.PublicID(), googleChain, "alice", nil), alice)
		veyronAlice = derive(bless(alice.PublicID(), veyronChain, "alice", nil), alice)
		bob         = newChain("bob").PublicID()
	)

	// veyron/alice blesses bob for 5 minutes
	veyronAliceBob, err := veyronAlice.Bless(bob, "bob@veyron@alice", 5*time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	authID, _ := veyronAliceBob.Authorize(NewContext(ContextArgs{}))
	if err := verifyAuthorizedID(veyronAliceBob, authID, "veyron/alice/bob@veyron@alice"); err != nil {
		t.Fatal(err)
	}

	// google/alice blesses bob for 1 millisecond
	googleAliceBob, err := googleAlice.Bless(bob, "bob@google@alice", 1*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for 1ms so that the blessing expires
	time.Sleep(time.Millisecond)
	authID, _ = googleAliceBob.Authorize(NewContext(ContextArgs{}))
	if err := verifyAuthorizedID(googleAliceBob, authID, ""); err != nil {
		t.Fatal(err)
	}

	// At this point, Bob has a valid blessing from veyron/alice and an
	// expired blessing from google/alice.  Bob should not be able to
	// construct a valid blessing from google/alice by combining certificates.
	veyronBob := veyronAliceBob.(*chainPublicID)
	googleBob := googleAliceBob.(*chainPublicID)
	// googleBob should be a valid identity before any modifications
	if _, err := roundTrip(googleBob); err != nil {
		t.Fatal(err)
	}
	// Keep the "google/alice" certificate and replace "alice/bob" from
	// "google/alice/bob" with the one from "veyron/alice/bob"
	cert := googleBob.certificates[2]
	googleBob.certificates[2] = veyronBob.certificates[2]
	// This hacked up identity should fail integrity tests
	if _, err := roundTrip(googleBob); err != wire.ErrNoIntegrity {
		t.Fatalf("roundTrip(%q) returned %v want %v", googleBob, err, wire.ErrNoIntegrity)
	}

	// Restoring the certificate should restore validity.
	googleBob.certificates[2] = cert
	if _, err := roundTrip(googleBob); err != nil {
		t.Fatal(err)
	}

	// Replacing the "google/alice" certificate with the "veyron/alice"
	// certificate should also cause the identity to be invalid.
	googleBob.certificates[1] = veyronBob.certificates[1]
	if _, err := roundTrip(googleBob); err != wire.ErrNoIntegrity {
		t.Fatalf("roundTrip(%q) returned %v want %v", googleBob, err, wire.ErrNoIntegrity)
	}
}

func TestDerive(t *testing.T) {
	var (
		cAlice       = newChain("alice")
		cVeyronAlice = bless(cAlice.PublicID(), veyronChain, "alice", nil)
		cBob         = newChain("bob").PublicID()
		tAlice       = newTree("alice")
		tVeyronAlice = bless(tAlice.PublicID(), veyronTree, "alice", nil)
		tBob         = newTree("bob").PublicID()
	)
	testdata := []struct {
		priv security.PrivateID
		pub  security.PublicID
		err  bool
	}{
		{priv: cAlice, pub: cVeyronAlice},
		{priv: cAlice, pub: cBob, err: true},
		{priv: tAlice, pub: tVeyronAlice},
		{priv: tAlice, pub: tBob, err: true},
	}
	for _, d := range testdata {
		derivedID, err := d.priv.Derive(d.pub)
		if (err != nil) != d.err {
			t.Errorf("%q.Derive(%q) returned error %v, wanted: %t", d.priv, d.pub, err, d.err)
			continue
		}
		if err != nil {
			// If it was not supposed to be, the previous check
			// would have registered the error.
			continue
		}
		if !reflect.DeepEqual(derivedID.PublicID(), d.pub) {
			t.Errorf("%q.Derive(%q) returned %q. PublicID mismatch", d.priv, d.pub, derivedID)
		}
		if !reflect.DeepEqual(derivedID.PrivateKey(), d.priv.PrivateKey()) {
			t.Errorf("%q.Derive(%q) returned %q. PrivateKey mismatch", d.priv, d.pub, derivedID)
		}
		if _, err := roundTrip(derivedID.PublicID()); err != nil {
			t.Errorf("roundTrip(%q=%q.Derive(%q)) failed: %v", derivedID, d.priv, d.pub, err)
		}
	}
}
