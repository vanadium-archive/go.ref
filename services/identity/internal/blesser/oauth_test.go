// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package blesser

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/test/testutil"

	"v.io/v23/security"
)

func join(elements ...string) string {
	return strings.Join(elements, security.ChainSeparator)
}

func TestOAuthBlesser(t *testing.T) {
	var (
		provider, user = testutil.NewPrincipal(), testutil.NewPrincipal()
		ctx, call      = fakeContextAndCall(provider, user)
	)
	mockEmail := "testemail@example.com"
	mockClientID := "test-client-id"
	mockClientName := "test-client"
	blesser := NewOAuthBlesserServer(OAuthBlesserParams{
		OAuthProvider:    oauth.NewMockOAuth(mockEmail, mockClientID),
		BlessingDuration: time.Hour,
		AccessTokenClients: []oauth.AccessTokenClient{
			oauth.AccessTokenClient{
				Name:     mockClientName,
				ClientID: mockClientID,
			},
		},
	})

	b, extension, err := blesser.BlessUsingAccessToken(ctx, call, "test-access-token")
	if err != nil {
		t.Errorf("BlessUsingAccessToken failed: %v", err)
	}

	wantExtension := join(mockEmail, mockClientName)
	if extension != wantExtension {
		t.Errorf("got extension: %s, want: %s", extension, wantExtension)
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
	// "provider/testemail@example.com/test-client".
	user.AddToRoots(b)
	binfo := user.BlessingsInfo(b)
	if num := len(binfo); num != 1 {
		t.Errorf("Got blessings with %d names, want exactly one name", num)
	}
	if _, ok := binfo[join("provider", wantExtension)]; !ok {
		t.Errorf("BlessingsInfo %v does not have name %s", binfo, wantExtension)
	}
}

func TestOAuthBlesserWithCaveats(t *testing.T) {
	var (
		provider, user = testutil.NewPrincipal(), testutil.NewPrincipal()
		ctx, call      = fakeContextAndCall(provider, user)
	)
	mockEmail := "testemail@example.com"
	mockClientID := "test-client-id"
	mockClientName := "test-client"
	blesser := NewOAuthBlesserServer(OAuthBlesserParams{
		OAuthProvider:    oauth.NewMockOAuth(mockEmail, mockClientID),
		BlessingDuration: time.Hour,
		AccessTokenClients: []oauth.AccessTokenClient{
			oauth.AccessTokenClient{
				Name:     mockClientName,
				ClientID: mockClientID,
			},
		},
	})

	expiryCav, err := security.NewExpiryCaveat(time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	methodCav, err := security.NewMethodCaveat("foo", "bar")
	if err != nil {
		t.Fatal(err)
	}
	caveats := []security.Caveat{expiryCav, methodCav}

	b, extension, err := blesser.BlessUsingAccessTokenWithCaveats(ctx, call, "test-access-token", caveats)
	if err != nil {
		t.Errorf("BlessUsingAccessToken failed: %v", err)
	}

	wantExtension := join(mockEmail, mockClientName)
	if extension != wantExtension {
		t.Errorf("got extension: %s, want: %s", extension, wantExtension)
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
	// "provider/testemail@example.com/test-client".
	user.AddToRoots(b)
	binfo := user.BlessingsInfo(b)
	if num := len(binfo); num != 1 {
		t.Errorf("Got blessings with %d names, want exactly one name", num)
	}
	cavs, ok := binfo[join("provider", wantExtension)]
	if !ok {
		t.Errorf("BlessingsInfo %v does not have name %s", binfo, wantExtension)
	}
	if !caveatsMatch(cavs, caveats) {
		t.Errorf("got %v, want %v", cavs, caveats)
	}
}

func caveatsMatch(got, want []security.Caveat) bool {
	if len(got) != len(want) {
		return false
	}
	gotStrings := make([]string, len(got))
	wantStrings := make([]string, len(want))
	for i := 0; i < len(got); i++ {
		gotStrings[i] = got[i].String()
		wantStrings[i] = want[i].String()
	}
	sort.Strings(gotStrings)
	sort.Strings(wantStrings)
	return reflect.DeepEqual(gotStrings, wantStrings)
}
