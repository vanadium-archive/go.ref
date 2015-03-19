package rpc

import (
	"testing"

	"v.io/v23/context"
	"v.io/v23/security"
	tsecurity "v.io/x/ref/test/security"
)

func TestDefaultAuthorizer(t *testing.T) {
	var (
		pali = tsecurity.NewPrincipal()
		pbob = tsecurity.NewPrincipal()
		pche = tsecurity.NewPrincipal()
		pdis = tsecurity.NewPrincipal() // third-party caveat discharger

		che, _ = pche.BlessSelf("che")
		ali, _ = pali.BlessSelf("ali")
		bob, _ = pbob.BlessSelf("bob")

		tpcav  = mkThirdPartyCaveat(pdis.PublicKey(), "someLocation", security.UnconstrainedUse())
		dis, _ = pdis.MintDischarge(tpcav, security.UnconstrainedUse())

		// bless(ali, bob, "friend") will generate a blessing for ali, calling him "bob/friend".
		bless = func(target, extend security.Blessings, extension string, caveats ...security.Caveat) security.Blessings {
			var p security.Principal
			switch extend.PublicKey() {
			case ali.PublicKey():
				p = pali
			case bob.PublicKey():
				p = pbob
			case che.PublicKey():
				p = pche
			default:
				panic(extend)
			}
			if len(caveats) == 0 {
				caveats = []security.Caveat{security.UnconstrainedUse()}
			}
			ret, err := p.Bless(target.PublicKey(), extend, extension, caveats[0], caveats[1:]...)
			if err != nil {
				panic(err)
			}
			return ret
		}

		U = func(blessings ...security.Blessings) security.Blessings {
			u, err := security.UnionOfBlessings(blessings...)
			if err != nil {
				panic(err)
			}
			return u
		}

		// Shorthands for getting blessings for Ali and Bob.
		A = func(as security.Blessings, extension string, caveats ...security.Caveat) security.Blessings {
			return bless(ali, as, extension, caveats...)
		}
		B = func(as security.Blessings, extension string, caveats ...security.Caveat) security.Blessings {
			return bless(bob, as, extension, caveats...)
		}

		authorizer defaultAuthorizer
	)
	// Make ali, bob (the two ends) recognize all three blessings
	for ip, p := range []security.Principal{pali, pbob} {
		for _, b := range []security.Blessings{ali, bob, che} {
			if err := p.AddToRoots(b); err != nil {
				t.Fatalf("%d: %v - %v", ip, b, err)
			}
		}
	}
	// All tests are run as if "ali" is the local end and "bob" is the remote.
	tests := []struct {
		local, remote security.Blessings
		call          *mockCall
		authorized    bool
	}{
		{
			local:      ali,
			remote:     ali,
			call:       &mockCall{},
			authorized: true,
		},
		{
			local:      ali,
			remote:     bob,
			call:       &mockCall{},
			authorized: false,
		},
		{
			// ali talking to ali/friend (invalid caveat)
			local:      ali,
			remote:     B(ali, "friend", tpcav),
			call:       &mockCall{},
			authorized: false,
		},
		{
			// ali talking to ali/friend
			local:      ali,
			remote:     B(ali, "friend", tpcav),
			call:       &mockCall{rd: dis},
			authorized: true,
		},
		{
			// bob/friend talking to bob (local blessing has an invalid caveat, but it is not checked)
			local:      A(bob, "friend", tpcav),
			remote:     bob,
			call:       &mockCall{},
			authorized: true,
		},
		{
			// che/friend talking to che/family
			local:      A(che, "friend"),
			remote:     B(che, "family"),
			call:       &mockCall{},
			authorized: false,
		},
		{
			// {ali, bob/friend, che/friend} talking to {bob/friend/spouse, che/family}
			local:      U(ali, A(bob, "friend"), A(che, "friend")),
			remote:     U(B(bob, "friend/spouse", tpcav), B(che, "family")),
			call:       &mockCall{rd: dis},
			authorized: true,
		},
	}
	ctx, shutdown := initForTest()
	defer shutdown()
	for _, test := range tests {
		test.call.p, test.call.l, test.call.r, test.call.c = pali, test.local, test.remote, ctx
		ctx, cancel := context.RootContext()
		defer cancel()
		err := authorizer.Authorize(security.SetCall(ctx, test.call))
		if (err == nil) != test.authorized {
			t.Errorf("call: %v. Got %v", test.call, err)
		}
	}
}
