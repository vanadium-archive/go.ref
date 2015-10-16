// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package blesser

import (
	"reflect"
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
	if got := security.BlessingNames(user, b); len(got) != 0 {
		t.Errorf("Got %v, want nil")
	}
	// But once it recognizes the provider, it should see exactly the name
	// "provider/testemail@example.com/test-client".
	security.AddToRoots(user, b)
	if got, want := security.BlessingNames(user, b), []string{join("provider", wantExtension)}; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
}

func TestOAuthBlesserWithCaveats(t *testing.T) {
	var (
		provider, user = testutil.NewPrincipal(), testutil.NewPrincipal()
		ctx, call      = fakeContextAndCall(provider, user)
		now            = time.Now()
		expires        = now.Add(time.Minute)
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

	expiryCav, err := security.NewExpiryCaveat(expires)
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
	if got := security.BlessingNames(user, b); len(got) != 0 {
		t.Errorf("Got %v, want nil", got)
	}
	// But once it recognizes the provider, it should see exactly the name
	// "provider/testemail@example.com/test-client".
	security.AddToRoots(user, b)
	allnames := []string{join("provider", wantExtension)}
	if got, want := security.BlessingNames(user, b), allnames; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
	// The presence of caveats will be tested by RemoteBlessingNames
	for _, test := range []struct {
		Time   time.Time
		Method string
		Names  []string
	}{
		{now, "foo", allnames},
		{now, "bar", allnames},
		{now, "baz", nil},
		{expires.Add(time.Nanosecond), "foo", nil},
		{expires.Add(time.Nanosecond), "bar", nil},
		{expires.Add(time.Nanosecond), "baz", nil},
	} {
		call := security.NewCall(&security.CallParams{
			LocalPrincipal:  user,
			RemoteBlessings: b,
			Timestamp:       test.Time,
			Method:          test.Method,
		})
		if got, _ := security.RemoteBlessingNames(ctx, call); !reflect.DeepEqual(got, test.Names) {
			t.Errorf("%#v: Got %v, want %v", test, got, test.Names)
		}
	}
}
