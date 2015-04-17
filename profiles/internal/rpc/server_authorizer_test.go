// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"testing"

	"v.io/v23"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/x/ref/profiles/internal/naming"

	"v.io/x/ref/test/testutil"
)

func TestServerAuthorizer(t *testing.T) {
	var (
		pclient = testutil.NewPrincipal()
		pserver = testutil.NewPrincipal()
		pother  = testutil.NewPrincipal()

		ali, _      = pserver.BlessSelf("ali")
		bob, _      = pserver.BlessSelf("bob")
		che, _      = pserver.BlessSelf("che")
		otherAli, _ = pother.BlessSelf("ali")
		zero        = security.Blessings{}

		ctx, shutdown = initForTest()

		U = func(blessings ...security.Blessings) security.Blessings {
			u, err := security.UnionOfBlessings(blessings...)
			if err != nil {
				t.Fatal(err)
			}
			return u
		}
	)
	defer shutdown()
	ctx, _ = v23.SetPrincipal(ctx, pclient)
	// Make client recognize ali, bob and otherAli blessings
	for _, b := range []security.Blessings{ali, bob, otherAli} {
		if err := pclient.AddToRoots(b); err != nil {
			t.Fatal(err)
		}
	}
	// All tests are run as if pclient is the client end and pserver is remote end.
	tests := []struct {
		serverBlessingNames []string
		auth                security.Authorizer
		authorizedServers   []security.Blessings
		unauthorizedServers []security.Blessings
	}{
		{
			// No blessings in the endpoint means that all servers are authorized.
			nil,
			newServerAuthorizer(""),
			[]security.Blessings{ali, otherAli, bob, che},
			[]security.Blessings{zero},
		},
		{
			// Endpoint sets the expectations for "ali" and "bob".
			[]string{"ali", "bob"},
			newServerAuthorizer(""),
			[]security.Blessings{ali, otherAli, bob, U(ali, che), U(bob, che)},
			[]security.Blessings{che},
		},
		{
			// Still only ali, otherAli and bob are authorized (che is not
			// authorized since it is not recognized by the client)
			[]string{"ali", "bob", "che"},
			newServerAuthorizer(""),
			[]security.Blessings{ali, otherAli, bob, U(ali, che), U(bob, che)},
			[]security.Blessings{che},
		},
		{

			// Only ali and otherAli are authorized (since there is an
			// allowed-servers policy that does not allow "bob")
			[]string{"ali", "bob", "che"},
			newServerAuthorizer("", options.AllowedServersPolicy{"ali", "bob"}, options.AllowedServersPolicy{"ali"}),
			[]security.Blessings{ali, otherAli, U(ali, che), U(ali, bob)},
			[]security.Blessings{bob, che},
		},
		{
			// Multiple AllowedServersPolicy are treated as an AND (and individual ones are "ORs")
			nil,
			newServerAuthorizer("", options.AllowedServersPolicy{"ali", "che"}, options.AllowedServersPolicy{"bob", "che"}),
			[]security.Blessings{U(ali, bob)},
			[]security.Blessings{ali, bob, che, U(ali, che), U(bob, che)},
		},
		{
			// Only otherAli is authorized (since only pother's public key is
			// authorized)
			[]string{"ali"},
			newServerAuthorizer("", options.ServerPublicKey{pother.PublicKey()}),
			[]security.Blessings{otherAli},
			[]security.Blessings{ali, bob, che},
		},
		{
			// Blessings in endpoint can be ignored.
			[]string{"ali"},
			newServerAuthorizer("", options.SkipServerEndpointAuthorization{}),
			[]security.Blessings{ali, bob, che, otherAli},
			nil,
		},
		{
			// Pattern specified is respected
			nil,
			newServerAuthorizer("bob"),
			[]security.Blessings{bob, U(ali, bob)},
			[]security.Blessings{ali, otherAli, che},
		},
		{
			// And concatenated with any existing AllowedServersPolicy
			[]string{"ali", "bob", "che"},
			newServerAuthorizer("bob", options.AllowedServersPolicy{"bob", "che"}),
			[]security.Blessings{bob, U(ali, bob), U(ali, bob, che)},
			[]security.Blessings{ali, che},
		},
		{
			// And if the intersection of AllowedServersPolicy and the pattern be empty, then so be it!
			[]string{"ali", "bob", "che"},
			newServerAuthorizer("bob", options.AllowedServersPolicy{"ali", "che"}),
			[]security.Blessings{U(ali, bob), U(ali, bob, che)},
			[]security.Blessings{ali, otherAli, bob, che, U(ali, che)},
		},
	}
	for _, test := range tests {
		for _, s := range test.authorizedServers {
			if err := test.auth.Authorize(ctx, &mockCall{
				p:   pclient,
				r:   s,
				rep: &naming.Endpoint{Blessings: test.serverBlessingNames},
			}); err != nil {
				t.Errorf("serverAuthorizer: %#v failed to authorize server: %v", test.auth, s)
			}
		}
		for _, s := range test.unauthorizedServers {
			if err := test.auth.Authorize(ctx, &mockCall{
				p:   pclient,
				r:   s,
				rep: &naming.Endpoint{Blessings: test.serverBlessingNames},
			}); err == nil {
				t.Errorf("serverAuthorizer: %#v authorized server: %v", test.auth, s)
			}
		}
	}
}
