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
		cOnlyMethodFoo = newCaveat(security.NewMethodCaveat("Foo"))
		ctx, call      = fakeContextAndCall(provider, user)
	)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	blesser := NewMacaroonBlesserServer(key)

	m := oauth.BlessingMacaroon{Creation: time.Now().Add(-1 * time.Hour), Name: "foo"}
	wantErr := "macaroon has expired"
	if _, err := blesser.Bless(ctx, call, newMacaroon(t, key, m)); err == nil || err.Error() != wantErr {
		t.Errorf("Bless(...) failed with error: %v, want: %v", err, wantErr)
	}
	m = oauth.BlessingMacaroon{Creation: time.Now(), Name: "bugsbunny", Caveats: []security.Caveat{cOnlyMethodFoo}}
	b, err := blesser.Bless(ctx, call, newMacaroon(t, key, m))
	if err != nil {
		t.Errorf("Bless failed: %v", err)
	}

	if !reflect.DeepEqual(b.PublicKey(), user.PublicKey()) {
		t.Errorf("Received blessing for public key %v. Client:%v, Blesser:%v", b.PublicKey(), user.PublicKey(), provider.PublicKey())
	}

	// When the user does not recognize the provider, it should not see any strings for
	// the client's blessings.
	if got := user.BlessingsInfo(b); got != nil {
		t.Errorf("Got blessing with info %v, want nil", got)
	}
	// But once it recognizes the provider, it should see exactly the name
	// "provider/users/bugsbunny" for the caveat cOnlyMethodFoo.
	user.AddToRoots(b)
	binfo := user.BlessingsInfo(b)
	if num := len(binfo); num != 1 {
		t.Errorf("Got blessings with %d names, want exactly one name", num)
	}
	wantName := "provider/users/bugsbunny"
	if got, want := binfo[wantName], []security.Caveat{cOnlyMethodFoo}; !reflect.DeepEqual(got, want) {
		t.Errorf("binfo[%q]: Got %v, want %v", wantName, got, want)
	}
}

func newMacaroon(t *testing.T, key []byte, m oauth.BlessingMacaroon) string {
	encMac, err := vom.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(util.NewMacaroon(key, encMac))
}
