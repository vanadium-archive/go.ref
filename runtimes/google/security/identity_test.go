package security

import (
	"bytes"
	"crypto/elliptic"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"testing"
	"time"

	"veyron/lib/testutil/blackbox"
	"veyron/security/caveat"
	"veyron2/security"
	"veyron2/security/wire"
	"veyron2/vlog"
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
		cTrustedAlice    = bless(cUnknownAlice, veyronChain, "alice")
		cMistrustedAlice = bless(cUnknownAlice, newChain("veyron"), "alice")
		cGoogleAlice     = bless(cUnknownAlice, googleChain, "alice")

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
		pattern security.BlessingPattern
		want    bool
	}
	testdata := []struct {
		id        security.PublicID
		matchData []matchInstance
	}{
		{
			// self-signed alice chain, not a trusted identity provider so should only match "..."
			id: alice.PublicID(),
			matchData: []matchInstance{
				{pattern: "...", want: true},
				{pattern: "alice", want: false},
				{pattern: "alice/...", want: false},
			},
		},
		{
			// veyron/alice: rooted in the trusted "veyron" identity provider
			id: bless(newChain("immaterial").PublicID(), veyronChain, "alice"),
			matchData: []matchInstance{
				{pattern: "...", want: true},
				{pattern: "veyron/...", want: true},
				{pattern: "veyron/alice", want: true},
				{pattern: "veyron/alice/...", want: true},
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
			id: newSetPublicID(alice.PublicID(), bless(alice.PublicID(), veyronChain, "alice"), bless(alice.PublicID(), googleChain, "alice")),
			matchData: []matchInstance{
				{pattern: "...", want: true},
				// Since alice is not a trusted identity provider, the self-blessed identity
				// should not match "alice/..."
				{pattern: "alice", want: false},
				{pattern: "alice/...", want: false},
				{pattern: "veyron/...", want: true},
				{pattern: "veyron/alice", want: true},
				{pattern: "veyron/alice/TV", want: true},
				{pattern: "veyron/alice/...", want: true},
				{pattern: "ali", want: false},
				{pattern: "aliced", want: false},
				{pattern: "veyron", want: false},
				{pattern: "veyron/ali", want: false},
				{pattern: "veyron/aliced", want: false},
				{pattern: "veyron/bob", want: false},
				{pattern: "google/alice", want: true},
				{pattern: "google/alice/TV", want: true},
				{pattern: "google/alice/...", want: true},
			},
		},
	}
	for _, d := range testdata {
		for _, m := range d.matchData {
			if got := m.pattern.MatchedBy(d.id.Names()...); got != m.want {
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
		googleAlice = bless(alice, googleChain, "googler")
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
	nCerts := len(alice.certificates)
	// Tamper with alice's public key, in a way that it can be decoded into a valid key,
	// just different from what was originally encoded.
	xy := alice.certificates[nCerts-1].PublicKey.XY
	x, y := elliptic.Unmarshal(elliptic.P256(), xy)
	y = y.Add(y, big.NewInt(1))
	alice.certificates[nCerts-1].PublicKey.XY = elliptic.Marshal(elliptic.P256(), x, y)
	if _, err := alice.certificates[nCerts-1].PublicKey.Decode(); err != nil {
		t.Fatal(err)
	}
	if _, err := roundTrip(alice); err != wire.ErrNoIntegrity {
		t.Errorf("Got %v want %v from roundTrip(%v)", err, wire.ErrNoIntegrity, alice)
	}
}

func TestBless(t *testing.T) {
	var (
		cAlice       = newChain("alice")
		cBob         = newChain("bob").PublicID()
		cVeyronAlice = derive(bless(cAlice.PublicID(), veyronChain, "alice"), cAlice)
		cGoogleAlice = derive(bless(cAlice.PublicID(), googleChain, "alice"), cAlice)
		cVeyronBob   = bless(cBob, veyronChain, "bob")

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

type unregisteredCaveat struct {
	// TODO(ataly, ashankar): The int is embedded in order to
	// distinguish the signature of this type from other Caveat types
	// and therefore preventing VOM from decoding values of this type
	// into another type. This is still not foolproof, we need to figure
	// out a better way of preventing the accidental decoding into another
	// type.
	UniqueField int
}

func (unregisteredCaveat) Validate(security.Context) error {
	return nil
}

func encodeUnregisteredCaveat([]string) {
	cav, err := security.NewCaveat(unregisteredCaveat{1})
	if err != nil {
		vlog.Fatalf("security.NewCaveat failed: %s", err)
	}
	var buf bytes.Buffer
	if err := vom.NewEncoder(&buf).Encode(cav); err != nil {
		vlog.Fatalf("could not VOM-encode caveat: %s", err)
	}
	fmt.Println(string(buf.Bytes()))
	blackbox.WaitForEOFOnStdin()
}

func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func TestAuthorizeWithCaveats(t *testing.T) {
	var (
		// alice
		pcAlice = newChain("alice")
		cAlice  = pcAlice.PublicID().(*chainPublicID)

		// Some random server called bob
		bob = newChain("bob").PublicID()

		// Caveats
		// Can only call "Play" at the Google service
		cavOnlyPlay = mkCaveat(security.MethodCaveat("Play"))
		// Can only talk to the "Google" service
		cavOnlyGoogle = mkCaveat(security.PeerBlessingsCaveat("google"))
	)

	// We create a Caveat from the CaveatValidator "unregisteredCaveat".
	// Since "unregisteredCaveat" is not registered with VOM, decoding
	// this caveat as part of a PublicID should fail.
	//
	// The Caveat is created within a child process as VOM automaitcally
	// registers all values encoded within the same process.
	child := blackbox.HelperCommand(t, "encodeUnregisteredCaveat")
	child.Cmd.Start()
	defer child.Cleanup()
	encodedCaveat, err := child.ReadLineFromChild()
	if err != nil {
		t.Fatalf("ReadLineFromChild failed: %v", err)
	}
	child.CloseStdin()

	var cavUnregistered security.Caveat
	if err := vom.NewDecoder(bytes.NewReader([]byte(encodedCaveat))).Decode(&cavUnregistered); err != nil {
		t.Fatalf("Failed to decode bytes obtained from ReadLineFromChild: %v", err)
	}

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
			client: bless(cAlice, veyronChain, "alice", cavOnlyPlay),
			tests: []rpc{
				{server: bob, method: "Play", authNames: S{"veyron/alice"}},
				{server: bob, method: "Hello", authErr: `security.methodCaveat=[Play] fails validation for method "Hello"`},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"veyron/alice"}},
				{server: googleChain.PublicID(), method: "Hello", authErr: `security.methodCaveat=[Play] fails validation for method "Hello"`},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyGoogle),
			tests: []rpc{
				{server: bob, method: "Hello", authErr: `security.peerBlessingsCaveat=[google] fails validation for peer with blessings []`},
				{server: googleChain.PublicID(), method: "Hello", authNames: S{"veyron/alice"}},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"veyron/alice"}},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavUnregistered),
			tests: []rpc{
				{server: bob, method: "Play", authErr: "caveat bytes could not be VOM-decoded"},
				{server: bob, method: "Hello", authErr: "caveat bytes could not be VOM-decoded"},
				{server: googleChain.PublicID(), method: "Play", authErr: "caveat bytes could not be VOM-decoded"},
				{server: googleChain.PublicID(), method: "Hello", authErr: "caveat bytes could not be VOM-decoded"},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyGoogle, cavOnlyPlay),
			tests: []rpc{
				{server: bob, method: "Hello", authErr: `security.peerBlessingsCaveat=[google] fails validation for peer with blessings []`},
				{server: bob, method: "Play", authErr: `security.peerBlessingsCaveat=[google] fails validation for peer with blessings []`},
				{server: googleChain.PublicID(), method: "Hello", authErr: `security.methodCaveat=[Play] fails validation for method "Hello"`},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"veyron/alice"}},
			},
		},
		{
			client: bless(cAlice, veyronChain, "alice", cavOnlyGoogle, cavUnregistered),
			tests: []rpc{
				{server: googleChain.PublicID(), method: "Play", authErr: "caveat bytes could not be VOM-decoded"},
				{server: googleChain.PublicID(), method: "Hello", authErr: "caveat bytes could not be VOM-decoded"},
			},
		},
		// client has multiple blessings
		{
			client: newSetPublicID(bless(cAlice, veyronChain, "valice", cavOnlyPlay, cavUnregistered), bless(cAlice, googleChain, "galice", cavOnlyGoogle)),
			tests: []rpc{
				{server: bob, method: "Hello", authErr: "none of the blessings in the set are authorized"},
				{server: bob, method: "Play", authErr: "none of the blessings in the set are authorized"},
				{server: googleChain.PublicID(), method: "Hello", authNames: S{"google/galice"}},
				{server: googleChain.PublicID(), method: "Play", authNames: S{"google/galice"}},
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
				t.Errorf("Bad testdata. Exactly one of authNames and authErr must be non-empty: %+q/%+v", d.client, test)
				continue
			}
			ctx := NewContext(ContextArgs{LocalID: test.server, RemoteID: d.client, Method: test.method})
			authID, err := d.client.Authorize(ctx)
			if !matchesErrorPattern(err, test.authErr) {
				t.Errorf("%q.Authorize(%v) returned error: %v, want to match: %q", d.client, ctx, err, test.authErr)
				continue
			}
			if err := verifyAuthorizedID(d.client, authID, test.authNames); err != nil {
				t.Errorf("%q.Authorize(%v) returned identity: %v want identity with names: %q [%v]", d.client, ctx, authID, test.authNames, err)
				continue
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
	cav, err := caveat.NewPublicKeyCaveat(newCaveat(proximityCaveat{}), minter.PublicID().PublicKey(), "location", security.ThirdPartyRequirements{})
	if err != nil {
		t.Fatalf("security.NewPublicKeyCaveat failed: %s", err)
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
		Discharges: dischargeMap{discharge.ID(): discharge},
		Debug:      "ctxValidateMinting",
	})
	if err = cav.Validate(ctxValidateMinting); err != nil {
		t.Errorf("Failed %q.Validate(%q): %s", cav, ctxValidateMinting, err)
	}
}

func TestAuthorizeWithThirdPartyCaveats(t *testing.T) {
	mkveyron := func(id security.PrivateID, name string) security.PrivateID {
		return derive(bless(id.PublicID(), veyronChain, name), id)
	}
	mkgoogle := func(id security.PrivateID, name string) security.PrivateID {
		return derive(bless(id.PublicID(), googleChain, name), id)
	}
	mkTPCaveat := func(id security.PrivateID) security.ThirdPartyCaveat {
		c, err := caveat.NewPublicKeyCaveat(newCaveat(alwaysValidCaveat{}), id.PublicID().PublicKey(), fmt.Sprintf("%v location", id.PublicID()), security.ThirdPartyRequirements{})
		if err != nil {
			t.Fatalf("NewPublicKeyCaveat with PublicKey of: %v failed: %s", id, err)
		}
		return c
	}
	var (
		alice = newChain("alice")
		bob   = newChain("bob")
		carol = newChain("carol").PublicID()

		// aliceProximityCaveat is a caveat whose discharge can only be minted by alice.
		aliceProximityCaveat = mkTPCaveat(alice)
		bobProximityCaveat   = mkTPCaveat(bob)
	)

	mintDischarge := func(id security.PrivateID, duration time.Duration, caveats ...security.Caveat) security.Discharge {
		d, err := id.MintDischarge(aliceProximityCaveat.(security.ThirdPartyCaveat), nil, duration, caveats)
		if err != nil {
			t.Fatalf("%q.MintDischarge failed: %v", id, err)
		}
		return d
	}
	var (
		// Discharges
		dAlice   = mintDischarge(alice, time.Minute)
		dGoogle  = mintDischarge(alice, time.Minute, mkCaveat(security.PeerBlessingsCaveat("google")))
		dExpired = mintDischarge(alice, 0)
		dInvalid = mintDischarge(bob, time.Minute) // Invalid because bob cannot mint valid discharges for aliceProximityCaveat

		// Contexts
		ctxEmpty = NewContext(ContextArgs{Debug: "ctxEmpty"})
		ctxAlice = NewContext(ContextArgs{
			Discharges: dischargeMap{dAlice.ID(): dAlice},
			Debug:      "ctxAlice",
		})
		// Context containing the discharge dGoogle but the server is not a Google server, so
		// the service caveat is not satisfied
		ctxGoogleAtOther = NewContext(ContextArgs{
			Discharges: dischargeMap{dGoogle.ID(): dGoogle},
			Debug:      "ctxGoogleAtOther",
		})
		// Context containing the discharge dGoogle at a google server.
		ctxGoogleAtGoogle = NewContext(ContextArgs{
			Discharges: dischargeMap{dGoogle.ID(): dGoogle},
			LocalID:    googleChain.PublicID(),
			Debug:      "ctxGoogleAtGoogle",
		})
		ctxExpired = NewContext(ContextArgs{
			Discharges: dischargeMap{dExpired.ID(): dExpired},
			Debug:      "ctxExpired",
		})
		ctxInvalid = NewContext(ContextArgs{
			Discharges: dischargeMap{dInvalid.ID(): dInvalid},
			Debug:      "ctxInvalid",
		})

		// Contexts that should always end in authorization errors
		errtests = map[security.Context]string{
			ctxEmpty:         "missing discharge",
			ctxGoogleAtOther: "security.peerBlessingsCaveat=[google] fails validation",
			ctxExpired:       "security.unixTimeExpiryCaveat",
			ctxInvalid:       "invalid signature",
		}
	)

	testdata := []struct {
		id        security.PublicID
		authNames S // For ctxAlice and ctxGoogleAtGoogle
	}{
		// carol blessed by bob with the third-party caveat should be authorized when the context contains a valid discharge
		{
			id:        bless(carol, mkveyron(bob, "bob"), "friend", newCaveat(aliceProximityCaveat)),
			authNames: S{"veyron/bob/friend"},
		},
		// veyron/vbob/vfriend with bobProximityCaveat and google/gbob/gfriend with aliceProximityCaveat
		// Only google/gbob/gfriend should be authorized since the discharge for the former is missing
		{
			id: newSetPublicID(
				bless(carol, mkveyron(bob, "vbob"), "vfriend", newCaveat(bobProximityCaveat)),
				bless(carol, mkgoogle(bob, "gbob"), "gfriend", newCaveat(aliceProximityCaveat))),
			authNames: S{"google/gbob/gfriend"},
		},
		// veyron/vbob/friend#google/gbob/friend both have the same caveat and both are satisfied
		{
			id:        bless(carol, newSetPrivateID(mkveyron(bob, "vbob"), mkgoogle(bob, "gbob")), "friend", newCaveat(aliceProximityCaveat)),
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

type SortedThirdPartyCaveats []security.ThirdPartyCaveat

func (s SortedThirdPartyCaveats) Len() int { return len(s) }
func (s SortedThirdPartyCaveats) Less(i, j int) bool {
	return s[i].ID() < s[j].ID()
}
func (s SortedThirdPartyCaveats) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func TestThirdPartyCaveatAccessors(t *testing.T) {
	mkTPCaveat := func(id security.PublicID) security.ThirdPartyCaveat {
		tpCav, err := caveat.NewPublicKeyCaveat(newCaveat(alwaysValidCaveat{}), id.PublicKey(), "someLocation", security.ThirdPartyRequirements{})
		if err != nil {
			t.Fatalf("NewPublicKeyCaveat with PublicKey of: %v failed: %s", id, err)
		}
		return tpCav
	}
	mintDischarge := func(caveat security.ThirdPartyCaveat, id security.PrivateID, caveats ...security.Caveat) security.Discharge {
		d, err := id.MintDischarge(caveat, nil, time.Minute, caveats)
		if err != nil {
			t.Fatalf("%q.MintDischarge failed: %v", id, err)
		}
		return d
	}
	sortTPCaveats := func(caveats []security.ThirdPartyCaveat) []security.ThirdPartyCaveat {
		sort.Stable(SortedThirdPartyCaveats(caveats))
		return caveats
	}

	var (
		// Principals (type conversions just to protect against accidentally
		// calling the wrong factory function)
		alice       = newChain("alice").(*chainPrivateID)
		cBob        = newChain("bob").(*chainPrivateID)
		cBobBuilder = derive(bless(cBob.PublicID(), cBob, "builder"), cBob) // Bob also calls himself bob/builder
		sBob        = newSetPrivateID(cBob, cBobBuilder).(setPrivateID)

		// Caveats
		tpCavAlice = mkTPCaveat(alice.PublicID())
		tpCavBob   = mkTPCaveat(alice.PublicID())
		cav        = mkCaveat(security.MethodCaveat(""))
	)

	caveats := []struct {
		firstparty *security.Caveat
		thirdparty []security.ThirdPartyCaveat
	}{
		{firstparty: nil, thirdparty: nil},
		{firstparty: &cav},
		{thirdparty: []security.ThirdPartyCaveat{tpCavAlice}},
		{thirdparty: []security.ThirdPartyCaveat{tpCavAlice, tpCavBob}},
		{firstparty: &cav, thirdparty: []security.ThirdPartyCaveat{tpCavAlice, tpCavBob}},
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
			var all []security.Caveat
			if c.firstparty != nil {
				all = append(all, *c.firstparty)
			}
			for _, tpc := range c.thirdparty {
				all = append(all, newCaveat(tpc))
			}
			// Test ThirdPartyCaveat accessors on security.PublicIDs.
			id := bless(d.pubID, d.privID, "irrelevant", all...)
			want := sortTPCaveats(c.thirdparty)
			if got := sortTPCaveats(id.ThirdPartyCaveats()); !reflect.DeepEqual(got, want) {
				t.Errorf("%q(%T) got ThirdPartyCaveats() = %+v, want %+v", id, id, got, want)
			}
			// Test ThirdPartyCaveat accessors on security.ThirdPartyCaveat discharges.
			dis := mintDischarge(mkTPCaveat(alice.PublicID()), d.privID, all...)
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
		googleAlice = derive(bless(alice.PublicID(), googleChain, "alice"), alice)
		veyronAlice = derive(bless(alice.PublicID(), veyronChain, "alice"), alice)
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
		cVeyronAlice = bless(cAlice.PublicID(), veyronChain, "alice")
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

		sAlice = newSetPublicID(bless(alice, veyronChain, "valice"), bless(alice, googleChain, "galice"))
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
	blackbox.CommandTable["encodeUnregisteredCaveat"] = encodeUnregisteredCaveat

	vom.Register(alwaysValidCaveat{})
	vom.Register(proximityCaveat{})
}
