package security

import (
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"veyron.io/veyron/veyron2/security"
)

func saveACLToTempFile(acl security.ACL) string {
	f, err := ioutil.TempFile("", "saved_acl")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := SaveACL(f, acl); err != nil {
		defer os.Remove(f.Name())
		panic(err)
	}
	return f.Name()
}

func updateACLInFile(fileName string, acl security.ACL) {
	f, err := os.OpenFile(fileName, os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := SaveACL(f, acl); err != nil {
		panic(err)
	}
}

func testSelfRPCs(t *testing.T, authorizer security.Authorizer) {
	_, file, line, _ := runtime.Caller(1)
	var (
		pserver, server = newPrincipal("server")
		_, imposter     = newPrincipal("server")
		palice, alice   = newPrincipal("alice")
		aliceServer     = bless(palice, pserver, alice, "server")

		ctxp  = &security.ContextParams{LocalPrincipal: pserver, LocalBlessings: server}
		tests = []struct {
			remote       security.Blessings
			isAuthorized bool
		}{
			{server, true},
			{imposter, false},
			// A principal talking to itself (even if with a different blessing) is authorized.
			// TODO(ashankar,ataly): Is this a desired property?
			{aliceServer, true},
		}
	)
	for _, test := range tests {
		ctxp.RemoteBlessings = test.remote
		ctx := security.NewContext(ctxp)
		if got, want := authorizer.Authorize(ctx), test.isAuthorized; (got == nil) != want {
			t.Errorf("%s:%d: %+v.Authorize(%v) returned error: %v, want error: %v", file, line, authorizer, ctx, got, !want)
		}
	}
}

func testNothingPermitted(t *testing.T, authorizer security.Authorizer) {
	_, file, line, _ := runtime.Caller(1)
	var (
		pserver, server = newPrincipal("server")
		palice, alice   = newPrincipal("alice")
		pbob, bob       = newPrincipal("random")

		serverAlice       = bless(pserver, palice, server, "alice")
		serverAliceFriend = bless(palice, pbob, serverAlice, "friend")
		serverBob         = bless(pserver, pbob, server, "bob")

		users = []security.Blessings{
			// blessings not recognized by "server" (since they are rooted at public
			// keys not recognized as roots by pserver)
			alice,
			bob,
			// blessings recognized by "server" (since they are its delegates)
			serverAlice,
			serverAliceFriend,
			serverBob,
		}

		invalidLabel = security.Label(3)
	)
	// No principal (other than the server itself - self-RPCs are allowed) should have access to any
	// valid or invalid label.
	for _, u := range users {
		for _, l := range security.ValidLabels {
			ctx := security.NewContext(&security.ContextParams{
				LocalPrincipal:  pserver,
				LocalBlessings:  server,
				RemoteBlessings: u,
				MethodTags:      []interface{}{l},
			})
			if got := authorizer.Authorize(ctx); got == nil {
				t.Errorf("%s:%d: %+v.Authorize(%v) returns nil, want error", file, line, authorizer, ctx)
			}
		}

		ctx := security.NewContext(&security.ContextParams{
			LocalPrincipal:  pserver,
			LocalBlessings:  server,
			RemoteBlessings: u,
			MethodTags:      []interface{}{invalidLabel},
		})
		if got := authorizer.Authorize(ctx); got == nil {
			t.Errorf("%s:%d: %+v.Authorize(%v) returns nil, want error", file, line, authorizer, ctx)
		}
	}
}

func TestACLAuthorizer(t *testing.T) {
	const (
		// Shorthands
		R = security.ReadLabel
		W = security.WriteLabel
		A = security.AdminLabel
		D = security.DebugLabel
		M = security.MonitoringLabel
		X = security.ResolveLabel
	)
	type Expectations map[security.Blessings]security.LabelSet
	// Principals to test
	var (
		// Principals
		pserver, server = newPrincipal("server")
		palice, alice   = newPrincipal("alice")
		pbob, bob       = newPrincipal("bob")
		pche, che       = newPrincipal("che")

		// Blessings from the server
		serverAlice       = bless(pserver, palice, server, "alice")
		serverBob         = bless(pserver, pbob, server, "bob")
		serverChe         = bless(pserver, pche, server, "che")
		serverAliceFriend = bless(palice, pbob, serverAlice, "friend")
		serverCheFriend   = bless(pche, pbob, serverChe, "friend")

		authorizer security.Authorizer // the authorizer to test.

		runTests = func(expectations Expectations) {
			_, file, line, _ := runtime.Caller(1)
			for user, labels := range expectations {
				for _, l := range security.ValidLabels {
					ctx := security.NewContext(&security.ContextParams{
						LocalPrincipal:  pserver,
						LocalBlessings:  server,
						RemoteBlessings: user,
						MethodTags:      []interface{}{l},
					})
					if got, want := authorizer.Authorize(ctx), labels.HasLabel(l); (got == nil) != want {
						t.Errorf("%s:%d: %+v.Authorize(%v) returned error: %v, want error: %v", file, line, authorizer, ctx, got, !want)
					}
				}
			}
		}
	)
	// Convenience function for combining Labels into a LabelSet.
	LS := func(labels ...security.Label) security.LabelSet {
		var ret security.LabelSet
		for _, l := range labels {
			ret = ret | security.LabelSet(l)
		}
		return ret
	}

	// ACL for testing
	acl := security.ACL{
		In: map[security.BlessingPattern]security.LabelSet{
			"...":              LS(R),
			"server/alice/...": LS(W, R),
			"server/alice":     LS(A, D, M),
			"server/bob":       LS(D, M),
			"server/che/...":   LS(W, R),
			"server/che":       LS(W, R),
		},
		NotIn: map[string]security.LabelSet{
			"server/che/friend": LS(W),
		},
	}

	// Authorizations for the above ACL.
	expectations := Expectations{
		alice:             LS(R),              // "..." ACL entry.
		bob:               LS(R),              // "..." ACL entry.
		che:               LS(R),              // "..." ACL entry.
		server:            security.AllLabels, // self RPC
		serverAlice:       LS(R, W, A, D, M),  // "server/alice/..." ACL entry
		serverBob:         LS(R, D, M),        // "..." and "server/bob" ACL entries
		serverAliceFriend: LS(W, R),           // "server/alice/..." ACL entry.
		serverChe:         LS(W, R),           // "server/che" ACL entry.
		serverCheFriend:   LS(R),              // "server/che/..." ACL entry, with the "server/che/friend" NotIn exception.
		nil:               LS(R),              // No blessings presented, same authorizations as "..." ACL entry.
	}
	// Create an aclAuthorizer based on the ACL and verify the authorizations.
	authorizer = NewACLAuthorizer(acl)
	runTests(expectations)
	testSelfRPCs(t, authorizer)

	// Create a fileACLAuthorizer by saving the ACL in a file, and verify the
	// authorizations.
	fileName := saveACLToTempFile(acl)
	defer os.Remove(fileName)
	authorizer = NewFileACLAuthorizer(fileName)
	runTests(expectations)
	testSelfRPCs(t, authorizer)

	// Modify the ACL stored in the file and verify that the authorizations appropriately
	// change for the fileACLAuthorizer.
	acl.In["server/bob"] = LS(R, W, A, D, M)
	expectations[serverBob] = LS(R, W, A, D, M)
	updateACLInFile(fileName, acl)
	runTests(expectations)
	testSelfRPCs(t, authorizer)

	// Update the ACL file with invalid contents and verify that no requests are
	// authorized.
	f, err := os.OpenFile(fileName, os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	f.Write([]byte("invalid ACL"))
	f.Close()
	testNothingPermitted(t, authorizer)
}

func TestFileACLAuthorizerOnNonExistentFile(t *testing.T) {
	testNothingPermitted(t, NewFileACLAuthorizer("fileDoesNotExist"))
}

func TestNilACLAuthorizer(t *testing.T) {
	authorizer := NewACLAuthorizer(nullACL)
	testNothingPermitted(t, authorizer)
	testSelfRPCs(t, authorizer)
}
