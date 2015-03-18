package ipc

import (
	"testing"

	tsecurity "v.io/x/ref/test/security"

	"v.io/v23"
	"v.io/v23/options"
	"v.io/v23/security"
)

func TestServerAuthorizer(t *testing.T) {
	var (
		pclient = tsecurity.NewPrincipal()
		pserver = tsecurity.NewPrincipal()
		pother  = tsecurity.NewPrincipal()

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
		auth                security.Authorizer
		authorizedServers   []security.Blessings
		unauthorizedServers []security.Blessings
	}{
		{
			// All servers with a non-zero blessing are authorized
			newServerAuthorizer(ctx, nil),
			[]security.Blessings{ali, otherAli, bob, che},
			[]security.Blessings{zero},
		},
		{
			// Only ali, otherAli and bob are authorized
			newServerAuthorizer(ctx, []security.BlessingPattern{"ali", "bob"}),
			[]security.Blessings{ali, otherAli, bob, U(ali, che), U(bob, che)},
			[]security.Blessings{che},
		},
		{
			// Still only ali, otherAli and bob are authorized (che is not
			// authorized since it is not recognized by the client)
			newServerAuthorizer(ctx, []security.BlessingPattern{"ali", "bob", "che"}, nil),
			[]security.Blessings{ali, otherAli, bob, U(ali, che), U(bob, che)},
			[]security.Blessings{che},
		},
		{

			// Only ali and otherAli are authorized (since there is an
			// allowed-servers policy that does not allow "bob")
			newServerAuthorizer(ctx, []security.BlessingPattern{"ali", "bob", "che"}, options.AllowedServersPolicy{"ali", "bob"}, options.AllowedServersPolicy{"ali"}),
			[]security.Blessings{ali, otherAli, U(ali, che), U(ali, bob)},
			[]security.Blessings{bob, che},
		},
		{
			// Only otherAli is authorized (since only pother's public key is
			// authorized)
			newServerAuthorizer(ctx, nil, options.ServerPublicKey{pother.PublicKey()}),
			[]security.Blessings{otherAli},
			[]security.Blessings{ali, bob, che},
		},
	}
	for _, test := range tests {
		for _, s := range test.authorizedServers {
			if err := test.auth.Authorize(security.SetCall(ctx, &mockCall{
				p: pclient,
				r: s,
			})); err != nil {
				t.Errorf("serverAuthorizer: %#v failed to authorize server: %v", test.auth, s)
			}
		}
		for _, s := range test.unauthorizedServers {
			if err := test.auth.Authorize(security.SetCall(ctx, &mockCall{
				p: pclient,
				r: s,
			})); err == nil {
				t.Errorf("serverAuthorizer: %#v authorized server: %v", test.auth, s)
			}
		}
	}
}
