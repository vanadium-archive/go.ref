// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package blesser

import (
	"crypto/rand"
	"reflect"
	"testing"
	"time"

	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/services/identity/internal/util"
	"v.io/x/ref/test/testutil"

	"v.io/v23/security"
	"v.io/v23/vom"
)

func TestMacaroonBlesser(t *testing.T) {
	var (
		key            = make([]byte, 16)
		provider, user = testutil.NewPrincipal(), testutil.NewPrincipal()
		userKey, _     = user.PublicKey().MarshalBinary()
		cOnlyMethodFoo = newCaveat(security.NewMethodCaveat("Foo"))
		ctx, call      = fakeContextAndCall(provider, user)
	)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	blesser := NewMacaroonBlesserServer(key)

	m := oauth.BlessingMacaroon{Creation: time.Now().Add(-1 * time.Hour), Name: "foo", PublicKey: userKey}
	wantErr := "macaroon has expired"
	if _, err := blesser.Bless(ctx, call, newMacaroon(t, key, m)); err == nil || err.Error() != wantErr {
		t.Errorf("Bless(...) failed with error: %v, want: %v", err, wantErr)
	}

	otherKey, _ := testutil.NewPrincipal().PublicKey().MarshalBinary()
	m = oauth.BlessingMacaroon{Creation: time.Now(), Name: "foo", PublicKey: otherKey}
	wantErr = "remote end's public key does not match public key in macaroon"
	if _, err := blesser.Bless(ctx, call, newMacaroon(t, key, m)); err == nil || err.Error() != wantErr {
		t.Errorf("Bless(...) failed with error: %v, want: %v", err, wantErr)
	}

	m = oauth.BlessingMacaroon{Creation: time.Now(), PublicKey: userKey, Name: "bugsbunny", Caveats: []security.Caveat{cOnlyMethodFoo}}
	b, err := blesser.Bless(ctx, call, newMacaroon(t, key, m))
	if err != nil {
		t.Errorf("Bless failed: %v", err)
	}

	if !reflect.DeepEqual(b.PublicKey(), user.PublicKey()) {
		t.Errorf("Received blessing for public key %v. Client:%v, Blesser:%v", b.PublicKey(), user.PublicKey(), provider.PublicKey())
	}

	// When the user does not recognize the provider, it should not see any strings for
	// the client's blessings.
	if got := security.BlessingNames(user, b); len(got) != 0 {
		t.Errorf("Got %v, want nil", got)
	}
	// But once it recognizes the provider, it should see exactly the name
	// "provider:bugsbunny" for the caveat cOnlyMethodFoo.
	security.AddToRoots(user, b)
	if got, want := security.BlessingNames(user, b), []string{"provider:bugsbunny"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
	// RemoteBlessingNames should see "provider:bugsbunny" only when caveats are met.
	for idx, test := range []struct {
		params security.CallParams
		names  []string
	}{
		{
			params: security.CallParams{LocalPrincipal: user, RemoteBlessings: b, Method: "Foo"},
			names:  []string{"provider:bugsbunny"},
		},
		{
			params: security.CallParams{LocalPrincipal: user, RemoteBlessings: b, Method: "Bar"},
			names:  nil,
		},
	} {
		got, _ := security.RemoteBlessingNames(ctx, security.NewCall(&test.params))
		if !reflect.DeepEqual(got, test.names) {
			t.Errorf("#%d) Got %v, want %v", idx, got, test.names)
		}
	}
}

func newMacaroon(t *testing.T, key []byte, m oauth.BlessingMacaroon) string {
	encMac, err := vom.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(util.NewMacaroon(key, encMac))
}
