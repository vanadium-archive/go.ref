package security

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"veyron/security/caveat"
	"veyron2/security"
	"veyron2/security/wire"
	"veyron2/vom"
)

type S []string

func TestNewPrivateID(t *testing.T) {
	testdata := []struct {
		name, err string
	}{
		{"alice", ""},
		{"alice#google", ""},
		{"alice@google", ""},
		{"bob.smith", ""},
		{"", "invalid blessing name"},
		{"/", "invalid blessing name"},
		{"/alice", "invalid blessing name"},
		{"alice/", "invalid blessing name"},
		{"google/alice", "invalid blessing name"},
		{"google/alice/bob", "invalid blessing name"},
	}
	for _, d := range testdata {
		if _, err := NewPrivateID(d.name, nil); !matchesErrorPattern(err, d.err) {
			t.Errorf("NewPrivateID(%q): got: %s, want to match: %s", d.name, err, d.err)
		}
	}
}

func TestNameAndAuth(t *testing.T) {
	var (
		cUnknownAlice    = newChain("alice").PublicID()
		cTrustedAlice    = bless(cUnknownAlice, veyronChain, "alice", nil)
		cMistrustedAlice = bless(cUnknownAlice, newChain("veyron"), "alice", nil)
		cGoogleAlice     = bless(cUnknownAlice, googleChain, "alice", nil)

		sAlice       = newSetPublicID(cTrustedAlice, cGoogleAlice)
		sBadAlice    = newSetPublicID(cUnknownAlice, cMistrustedAlice)
		sGoogleAlice = newSetPublicID(cMistrustedAlice, cGoogleAlice)
	)
	testdata := []struct {
		id    security.PublicID
		names []string
	}{
		{id: cUnknownAlice},
		{id: cTrustedAlice, names: S{"veyron/alice"}},
		{id: cMistrustedAlice},
		{id: sAlice, names: S{"veyron/alice", "google/alice"}},
		{id: sBadAlice},
		{id: sGoogleAlice, names: S{"google/alice"}},
	}
	for _, d := range testdata {
		if got, want := d.id.Names(), d.names; !reflect.DeepEqual(got, want) {
			t.Errorf("%q(%T).Names(): got: %q, want: %q", d.id, d.id, got, want)
		}
		authID, err := d.id.Authorize(NewContext(ContextArgs{}))
		if (authID != nil) == (err != nil) {
			t.Errorf("%q.Authorize returned: (%v, %v), exactly one return value must be nil", d.id, authID, err)
			continue
		}
		if err := verifyAuthorizedID(d.id, authID, d.names); err != nil {
			t.Error(err)
		}
	}
}

func TestMatch(t *testing.T) {
	alice := newChain("alice")
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
			id: alice.PublicID(),
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
			id: newSetPublicID(alice.PublicID(), bless(alice.PublicID(), veyronChain, "alice", nil), bless(alice.PublicID(), googleChain, "alice", nil)),
			matchData: []matchInstance{
				{pattern: "*", want: true},
				// Since alice is not a trusted identity provider, the self-blessed identity
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
			if got := security.Matches(d.id, m.pattern); got != m.want {
				t.Errorf("%q.Match(%s), Got %t, want %t", d.id, m.pattern, got, m.want)
			}
		}
	}
}

func TestExpiredIdentityChain(t *testing.T) {
	id, err := veyronChain.Bless(newChain("immaterial").PublicID(), "alice", time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if authid, _ := id.Authorize(NewContext(ContextArgs{})); authid != nil && len(authid.Names()) != 0 {
		t.Errorf("%q.Authorize returned %v, wanted empty slice", id, authid)
	}
}

func TestExpiredIdentityInSet(t *testing.T) {
	var (
		alice       = newChain("alice").PublicID()
		googleAlice = bless(alice, googleChain, "googler", nil)
	)
	veyronAlice, err := veyronChain.Bless(alice, "veyroner", time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	setid := newSetPublicID(alice, googleAlice, veyronAlice)
	authid, _ := setid.Authorize(NewContext(ContextArgs{}))
	if authid == nil {
		t.Fatalf("%q.Authorize returned nil, expected google/alice", setid)
	}
	if got, want := authid.Names(), "google/googler"; len(got) != 1 || got[0] != want {
		t.Errorf("Got %v want [%v]", got, want)
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

func TestBless(t *testing.T) {
	var (
		cAlice       = newChain("alice")
		cBob         = newChain("bob").PublicID()
		cVeyronAlice = derive(bless(cAlice.PublicID(), veyronChain, "alice", nil), cAlice)
		cGoogleAlice = derive(bless(cAlice.PublicID(), googleChain, "alice", nil), cAlice)
		cVeyronBob   = bless(cBob, veyronChain, "bob", nil)

		sVeyronAlice = newSetPrivateID(cAlice, cVeyronAlice, cGoogleAlice)
		sVeyronBob   = newSetPublicID(cBob, cVeyronBob)
	)
	testdata := []struct {
		blessor  security.PrivateID
		blessee  security.PublicID
		blessing string   // name provided to security.PublicID.Bless
		blessed  []string // names of the blessed identity. Empty if the Bless operation should have failed
		err      string
	}{
		// Blessings from veyron/alice (chain implementation)
		{
			blessor: cVeyronAlice,
			blessee: cBob,
			err:     `invalid blessing name:""`,
		},
		{
			blessor:  cVeyronAlice,
			blessee:  cBob,
			blessing: "alice/bob",
			err:      `invalid blessing name:"alice/bob"`,
		},
		{
			blessor:  cVeyronAlice,
			blessee:  cBob,
			blessing: "friend_bob",
			blessed:  S{"veyron/alice/friend_bob"},
		},
		{
			blessor:  cVeyronAlice,
			blessee:  sVeyronBob,
			blessing: "friend_bob",
			blessed:  S{"veyron/alice/friend_bob"},
		},
		// Blessings from alice#google/alice#veyron/alice
		{
			blessor: sVeyronAlice,
			blessee: cBob,
			err:     `invalid blessing name:""`,
		},
		{
			blessor:  sVeyronAlice,
			blessee:  cBob,
			blessing: "alice/bob",
			err:      `invalid blessing name:"alice/bob"`,
		},
		{
			blessor:  sVeyronAlice,
			blessee:  cBob,
			blessing: "friend_bob",
			blessed:  S{"veyron/alice/friend_bob", "google/alice/friend_bob"},
		},
		{
			blessor:  sVeyronAlice,
			blessee:  sVeyronBob,
			blessing: "friend_bob",
			blessed:  S{"veyron/alice/friend_bob", "google/alice/friend_bob"},
		},
	}

	for _, d := range testdata {
		if (len(d.blessed) == 0) == (len(d.err) == 0) {
			t.Fatalf("Bad testdata. Exactly one of blessed and err must be non-empty: %+v", d)
		}
		blessed, err := d.blessor.Bless(d.blessee, d.blessing, 1*time.Minute, nil)
		// Exactly one of (blessed, err) should be nil
		if (blessed != nil) == (err != nil) {
			t.Errorf("%q.Bless(%q, %q, ...) returned: (%v, %v): exactly one return value should be nil", d.blessor, d.blessee, d.blessing, blessed, err)
			continue
		}
		// err should match d.err
		if !matchesErrorPattern(err, d.err) {
			t.Errorf("%q.Bless(%q, %q, ...) returned error: %v, want to match: %q", d.blessor, d.blessee, d.blessing, err, d.err)
		}
		if err != nil {
			continue
		}
		// Compare names
		if got, want := blessed.Names(), d.blessed; !reflect.DeepEqual(got, want) {
			t.Errorf("%q.Names(): got: %q, want: %q", blessed, got, want)
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
		// alice
		pcAlice = newChain("alice")
		cAlice  = pcAlice.PublicID().(*chainPublicID)

		// veyron/alice/tv
		cVeyronAliceTV = bless(newChain("tv").PublicID(),
			derive(bless(cAlice, veyronChain, "alice", nil), pcAlice),
			"tv", nil).(*chainPublicID)

		// Some random server called bob
		bob = newChain("bob").PublicID()

		// Caveats
		// Can only call "Play" at the Google service
		cavOnlyPlayAtGoogle = methodRestrictionCaveat("google", S{"Play"})
		// Can only talk to the "Google" service
		cavOnlyGoogle = peerIdentityCaveat("google")
		// Can only call the PublicProfile method on veyron/alice/*
		cavOnlyPublicProfile = methodRestrictionCaveat("veyron/alice/*", S{"PublicProfile"})
	)

	type rpc struct {
		server security.PublicID
		method string
		// Expected output: exactly one should be non-empty
		authErr   string
		authNames []string
	}
	testdata := []struct {
		client security.PublicID
		tests  []rpc
	}{
		// client has a chain identity
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyPlayAtGoogle),
			tests: []rpc{
				{server: bob, method: "Hello", authNames: S{"veyron/alice"}},
				{server: bob, authNames: S{"veyron/alice"}},
				{server: googleChain.PublicID(), method: "Hello", authErr: `caveat.MethodRestriction{"Play"} forbids invocation of method Hello`},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"veyron/alice"}},
				{server: googleChain.PublicID(), authNames: S{"veyron/alice"}},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyGoogle),
			tests: []rpc{
				{server: bob, method: "Hello", authErr: `caveat.PeerIdentity{"google"} forbids RPCing with peer`},
				{server: googleChain.PublicID(), method: "Hello", authNames: S{"veyron/alice"}},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"veyron/alice"}},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", append(cavOnlyGoogle, cavOnlyPlayAtGoogle...)),
			tests: []rpc{
				{server: bob, method: "Hello", authErr: `caveat.PeerIdentity{"google"} forbids RPCing with peer`},
				{server: googleChain.PublicID(), method: "Hello", authErr: `caveat.MethodRestriction{"Play"} forbids invocation of method Hello`},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"veyron/alice"}},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyPublicProfile),
			tests: []rpc{
				{server: cVeyronAliceTV, method: "PrivateProfile", authErr: `caveat.MethodRestriction{"PublicProfile"} forbids invocation of method PrivateProfile`},
				{server: cVeyronAliceTV, method: "PublicProfile", authNames: S{"veyron/alice"}},
			},
		},
		// client has multiple blessings
		{
			client: newSetPublicID(bless(cAlice, veyronChain, "valice", append(cavOnlyPlayAtGoogle, cavOnlyPublicProfile...)), bless(cAlice, googleChain, "galice", cavOnlyGoogle)),
			tests: []rpc{
				{server: bob, method: "Hello", authNames: S{"veyron/valice"}},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"veyron/valice", "google/galice"}},
				{server: googleChain.PublicID(), method: "Buy", authNames: S{"google/galice"}},
				{server: cVeyronAliceTV, method: "PublicProfile", authNames: S{"veyron/valice"}},
				{server: cVeyronAliceTV, method: "PrivateProfile", authErr: "none of the blessings in the set are authorized"},
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
			if (len(test.authNames) == 0) == (len(test.authErr) == 0) {
				t.Fatalf("Bad testdata. Exactly one of authNames and authErr must be non-empty: %+q/%+v", d.client, test)
			}
			ctx := NewContext(ContextArgs{LocalID: test.server, RemoteID: d.client, Method: test.method})
			authID, err := d.client.Authorize(ctx)
			if !matchesErrorPattern(err, test.authErr) {
				t.Errorf("%q.Authorize(%v) returned error: %v, want to match: %q", d.client, ctx, err, test.authErr)
			}
			if err := verifyAuthorizedID(d.client, authID, test.authNames); err != nil {
				t.Errorf("%q.Authorize(%v) returned identity: %v want identity with names: %q [%v]", d.client, ctx, authID, test.authNames, err)
			}
		}
	}
}

type alwaysValidCaveat struct{}

func (alwaysValidCaveat) Validate(security.Context) error {
	return nil
}

// proximityCaveat abuses the Method field to store proximity info
// TODO(andreser): create a context that can hold proximity data by design
type proximityCaveat struct{}

func (proximityCaveat) Validate(ctx security.Context) error {
	if ctx != nil && ctx.Method() == "proximity: close enough" {
		return nil
	} else {
		return fmt.Errorf("proximityCaveat: not close enough")
	}
}

func TestThirdPartyCaveatMinting(t *testing.T) {
	minter := newChain("minter")
	cav, err := caveat.NewPublicKeyCaveat(proximityCaveat{}, minter.PublicID(), "location", security.ThirdPartyRequirements{})
	if err != nil {
		t.Fatal(err)
	}

	discharge, err := minter.MintDischarge(cav, NewContext(ContextArgs{}), time.Hour, nil)
	if discharge != nil || !matchesErrorPattern(err, "not close enough") {
		t.Errorf("Discharge was minted while minting caveats were not met")
	}

	discharge, err = minter.MintDischarge(cav, NewContext(ContextArgs{Method: "proximity: close enough"}), time.Hour, nil)
	if err != nil {
		t.Errorf("Discharge was NOT minted even though minting caveats were met")
	}

	ctxValidateMinting := NewContext(ContextArgs{
		Discharges: security.CaveatDischargeMap{discharge.CaveatID(): discharge},
		Debug:      "ctxValidateMinting",
	})
	if err = cav.Validate(ctxValidateMinting); err != nil {
		t.Errorf("Failed %q.Validate(%q): %s", cav, ctxValidateMinting, err)
	}
}

func TestAuthorizeWithThirdPartyCaveats(t *testing.T) {
	mkveyron := func(id security.PrivateID, name string) security.PrivateID {
		return derive(bless(id.PublicID(), veyronChain, name, nil), id)
	}
	mkgoogle := func(id security.PrivateID, name string) security.PrivateID {
		return derive(bless(id.PublicID(), googleChain, name, nil), id)
	}
	mkcaveat := func(id security.PrivateID) []security.ServiceCaveat {
		c, err := caveat.NewPublicKeyCaveat(alwaysValidCaveat{}, id.PublicID(), fmt.Sprintf("%v location", id.PublicID()), security.ThirdPartyRequirements{})
		if err != nil {
			t.Fatal(err)
		}
		return []security.ServiceCaveat{security.UniversalCaveat(c)}
	}
	var (
		alice = newChain("alice")
		bob   = newChain("bob")
		carol = newChain("carol").PublicID()

		// aliceProximityCaveat is a caveat whose discharge can only be minted by alice.
		aliceProximityCaveat = mkcaveat(alice)
		bobProximityCaveat   = mkcaveat(bob)
	)

	mintDischarge := func(id security.PrivateID, duration time.Duration, caveats []security.ServiceCaveat) security.ThirdPartyDischarge {
		d, err := id.MintDischarge(aliceProximityCaveat[0].Caveat.(security.ThirdPartyCaveat), nil, duration, caveats)
		if err != nil {
			t.Fatalf("%q.MintDischarge failed: %v", id, err)
		}
		return d
	}
	var (
		// Discharges
		dAlice   = mintDischarge(alice, time.Minute, nil)
		dGoogle  = mintDischarge(alice, time.Minute, peerIdentityCaveat("google"))
		dExpired = mintDischarge(alice, 0, nil)
		dInvalid = mintDischarge(bob, time.Minute, nil) // Invalid because bob cannot mint valid discharges for aliceProximityCaveat

		// Contexts
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

		// Contexts that should always end in authorization errors
		errtests = map[security.Context]string{
			ctxEmpty:         "missing discharge",
			ctxGoogleAtOther: "forbids RPCing with peer",
			ctxExpired:       "at this time",
			ctxInvalid:       "invalid signature",
		}
	)

	testdata := []struct {
		id        security.PublicID
		authNames S // For ctxAlice and ctxGoogleAtGoogle
	}{
		// carol blessed by bob with the third-party caveat should be authorized when the context contains a valid discharge
		{
			id:        bless(carol, mkveyron(bob, "bob"), "friend", aliceProximityCaveat),
			authNames: S{"veyron/bob/friend"},
		},
		// veyron/vbob/vfriend with bobProximityCaveat and google/gbob/gfriend with aliceProximityCaveat
		// Only google/gbob/gfriend should be authorized since the discharge for the former is missing
		{
			id: newSetPublicID(
				bless(carol, mkveyron(bob, "vbob"), "vfriend", bobProximityCaveat),
				bless(carol, mkgoogle(bob, "gbob"), "gfriend", aliceProximityCaveat)),
			authNames: S{"google/gbob/gfriend"},
		},
		// veyron/vbob/friend#google/gbob/friend both have the same caveat and both are satisfied
		{
			id:        bless(carol, newSetPrivateID(mkveyron(bob, "vbob"), mkgoogle(bob, "gbob")), "friend", aliceProximityCaveat),
			authNames: S{"veyron/vbob/friend", "google/gbob/friend"},
		},
	}
	for _, test := range testdata {
		if _, err := roundTrip(test.id); err != nil {
			t.Errorf("%q is not round-trippable: %v", test.id, test.id, err)
		}
		for _, ctx := range []security.Context{ctxAlice, ctxGoogleAtGoogle} {
			authID, _ := test.id.Authorize(ctx)
			if err := verifyAuthorizedID(test.id, authID, test.authNames); err != nil {
				t.Errorf("%q.Authorize(%v): %v", test.id, ctx, err)
			}
		}
		for ctx, want := range errtests {
			authID, err := test.id.Authorize(ctx)
			if authID != nil {
				t.Errorf("%q.Authorize(%v) returned %v, should have returned nil", test.id, ctx, authID)
			}
			if !matchesErrorPattern(err, want) {
				t.Errorf("%q.Authorize(%v) returned error: %v, want to match: %q", test.id, ctx, err, want)
			}
		}
	}
}

type SortedThirdPartyCaveats []security.ServiceCaveat

func (s SortedThirdPartyCaveats) Len() int { return len(s) }
func (s SortedThirdPartyCaveats) Less(i, j int) bool {
	return s[i].Caveat.(security.ThirdPartyCaveat).ID() < s[j].Caveat.(security.ThirdPartyCaveat).ID()
}
func (s SortedThirdPartyCaveats) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func TestThirdPartyCaveatAccessors(t *testing.T) {
	mkTPCaveat := func(id security.PublicID) security.ThirdPartyCaveat {
		tpCav, err := caveat.NewPublicKeyCaveat(alwaysValidCaveat{}, id, "someLocation", security.ThirdPartyRequirements{})
		if err != nil {
			t.Fatalf("NewPublicKeyCaveat(%q, ...) failed: %v", id, err)
		}
		return tpCav
	}
	mintDischarge := func(caveat security.ThirdPartyCaveat, id security.PrivateID, caveats []security.ServiceCaveat) security.ThirdPartyDischarge {
		d, err := id.MintDischarge(caveat, nil, time.Minute, caveats)
		if err != nil {
			t.Fatalf("%q.MintDischarge failed: %v", id, err)
		}
		return d
	}

	sortTPCaveats := func(c []security.ServiceCaveat) []security.ServiceCaveat {
		sort.Stable(SortedThirdPartyCaveats(c))
		return c
	}

	var (
		// Principals (type conversions just to protect against accidentally
		// calling the wrong factory function)
		alice       = newChain("alice").(*chainPrivateID)
		cBob        = newChain("bob").(*chainPrivateID)
		cBobBuilder = derive(bless(cBob.PublicID(), cBob, "builder", nil), cBob) // Bob also calls himself bob/builder
		sBob        = newSetPrivateID(cBob, cBobBuilder).(setPrivateID)

		// Caveats
		tpCavService   = security.ServiceCaveat{Service: "someService", Caveat: mkTPCaveat(alice.PublicID())}
		tpCavUniversal = security.UniversalCaveat(mkTPCaveat(alice.PublicID()))
		cav            = methodRestrictionCaveat("someService", nil)[0]
	)

	caveats := []struct {
		firstparty []security.ServiceCaveat
		thirdparty []security.ServiceCaveat
	}{
		{firstparty: nil, thirdparty: nil},
		{firstparty: []security.ServiceCaveat{cav}},
		{thirdparty: []security.ServiceCaveat{tpCavService}},
		{thirdparty: []security.ServiceCaveat{tpCavService, tpCavUniversal}},
		{firstparty: []security.ServiceCaveat{cav}, thirdparty: []security.ServiceCaveat{tpCavService, tpCavUniversal}},
	}
	testdata := []struct {
		privID security.PrivateID
		pubID  security.PublicID
	}{
		{privID: veyronChain, pubID: cBob.PublicID()}, // Chain blessing a chain
		{privID: veyronChain, pubID: sBob.PublicID()}, // Chain blessing a set
		{privID: sBob, pubID: cBob.PublicID()},        // Set blessing a chain
		//{privID: veyronTree, pubID: tBob.PublicID()},
	}
	for _, d := range testdata {
		for _, c := range caveats {
			all := append(c.firstparty, c.thirdparty...)
			// Test ThirdPartyCaveat accessors on security.PublicIDs.
			id := bless(d.pubID, d.privID, "irrelevant", all)
			want := sortTPCaveats(c.thirdparty)
			if got := sortTPCaveats(id.ThirdPartyCaveats()); !reflect.DeepEqual(got, want) {
				t.Errorf("%q(%T) got ThirdPartyCaveats() = %+v, want %+v", id, id, got, want)
			}
			// Test ThirdPartyCaveat accessors on security.ThirdPartyCaveatDischarges.
			dis := mintDischarge(mkTPCaveat(alice.PublicID()), d.privID, all)
			if got := sortTPCaveats(dis.ThirdPartyCaveats()); !reflect.DeepEqual(got, want) {
				t.Errorf("%q got ThirdPartyCaveats() = %+v, want %+v", dis, got, want)
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
	if err := verifyAuthorizedID(veyronAliceBob, authID, S{"veyron/alice/bob@veyron@alice"}); err != nil {
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
	if authID != nil {
		t.Fatal("%q.Authorized returned: %q, want nil", authID)
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
		t.Fatalf("roundTrip(%q) returned: %v want %v", googleBob, err, wire.ErrNoIntegrity)
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
		t.Fatalf("roundTrip(%q) returned: %v want %v", googleBob, err, wire.ErrNoIntegrity)
	}
}

func TestDerive(t *testing.T) {
	var (
		cAlice       = newChain("alice")
		cVeyronAlice = bless(cAlice.PublicID(), veyronChain, "alice", nil)
		cBob         = newChain("bob").PublicID()
		sVeyronAlice = newSetPrivateID(cAlice, derive(cVeyronAlice, cAlice))

		tChain = reflect.TypeOf(cAlice)
		tSet   = reflect.TypeOf(sVeyronAlice)
		tErr   = reflect.TypeOf(nil)
	)
	testdata := []struct {
		priv security.PrivateID
		pub  security.PublicID
		typ  reflect.Type
	}{
		{priv: cAlice, pub: cVeyronAlice, typ: tChain},          // chain.Derive(chain) = chain
		{priv: cAlice, pub: sVeyronAlice.PublicID(), typ: tSet}, // chain.Derive(set) = set
		{priv: cAlice, pub: cBob, typ: tErr},
		{priv: sVeyronAlice, pub: cAlice.PublicID(), typ: tChain},     // set.Derive(chain) = chain
		{priv: sVeyronAlice, pub: sVeyronAlice.PublicID(), typ: tSet}, // set.Derive(set) = set
		{priv: sVeyronAlice, pub: cBob, typ: tErr},
	}
	for _, d := range testdata {
		derivedID, err := d.priv.Derive(d.pub)
		if reflect.TypeOf(derivedID) != d.typ {
			t.Errorf("%T=%q.Derive(%T=%q) yielded (%T, %v), want %v", d.priv, d.priv, d.pub, d.pub, derivedID, err, d.typ)
			continue
		}
		if err != nil {
			// If it was not supposed to be, the previous check
			// would have registered the error.
			continue
		}
		if !reflect.DeepEqual(derivedID.PublicID(), d.pub) {
			t.Errorf("%q.Derive(%q) returned: %q. PublicID mismatch", d.priv, d.pub, derivedID)
		}
		if _, err := roundTrip(derivedID.PublicID()); err != nil {
			t.Errorf("roundTrip(%q=%q.Derive(%q)) failed: %v", derivedID, d.priv, d.pub, err)
		}
	}
}

func TestNewSetFailures(t *testing.T) {
	var (
		alice = newChain("alice")
		bob   = newChain("bob")
	)
	if s, err := NewSetPrivateID(alice, bob); err == nil {
		t.Errorf("Got %v, want error since PrivateKeys do not match", s)
	}
	if s, err := NewSetPublicID(alice.PublicID(), bob.PublicID()); err == nil {
		t.Errorf("Got %v, want error since PublicKeys do not match", s)
	}
}

func TestSetIdentityAmplification(t *testing.T) {
	var (
		alice = newChain("alice").PublicID()
		bob   = newChain("bob").PublicID()

		sAlice = newSetPublicID(bless(alice, veyronChain, "valice", nil), bless(alice, googleChain, "galice", nil))
	)

	// Manipulate sAlice before writing it out to the wire so that it has Bob's authorizations.
	reflect.ValueOf(sAlice).Elem().Index(1).Set(reflect.ValueOf(bob))
	// Encode/decode the identity.
	var buf bytes.Buffer
	if err := vom.NewEncoder(&buf).Encode(sAlice); err != nil {
		t.Fatal(err)
	}
	var decoded security.PublicID
	if err := vom.NewDecoder(&buf).Decode(&decoded); err == nil || decoded != nil {
		t.Fatalf("Got (%v, %v), want wire decode of manipulated identity to fail", decoded, err)
	}
}

func init() {
	vom.Register(alwaysValidCaveat{})
	vom.Register(proximityCaveat{})
}
