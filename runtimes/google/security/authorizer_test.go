package security

import (
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"veyron2/security"
)

type authMap map[security.PublicID]security.LabelSet

func saveACLToTempFile(acl security.ACL) string {
	f, err := ioutil.TempFile("", "saved_acl")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := saveACL(f, acl); err != nil {
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
	if err := saveACL(f, acl); err != nil {
		panic(err)
	}
}

func testSelfRPCs(t *testing.T, authorizer security.Authorizer) {
	_, file, line, _ := runtime.Caller(1)
	var (
		cAlice        = newChain("alice")
		cVeyronAlice  = bless(cAlice.PublicID(), veyronChain, "alice", nil)
		tAlice        = newTree("alice")
		tVeyronAlice  = bless(tAlice.PublicID(), veyronTree, "alice", nil)
		tBlessedAlice = bless(tVeyronAlice, googleTree, "alice", nil)
	)
	testData := []struct {
		localID, remoteID security.PublicID
		isAuthorized      bool
	}{
		{cAlice.PublicID(), cAlice.PublicID(), false},
		{cVeyronAlice, cVeyronAlice, true},
		{tVeyronAlice, tBlessedAlice, true},
	}
	for _, d := range testData {
		ctx := NewContext(ContextArgs{LocalID: d.localID, RemoteID: d.remoteID})
		if got, want := authorizer.Authorize(ctx), d.isAuthorized; (got == nil) != want {
			t.Errorf("%s:%d: %+v.Authorize(%v) returned error: %v, want nil: %v", file, line, authorizer, ctx, got, want)
		}
	}
}

func testAuthorizations(t *testing.T, authorizer security.Authorizer, authorizations authMap) {
	_, file, line, _ := runtime.Caller(1)
	for user, labels := range authorizations {
		for _, l := range security.ValidLabels {
			ctx := NewContext(ContextArgs{RemoteID: user, Label: l})
			if got, want := authorizer.Authorize(ctx), labels.HasLabel(l); (got == nil) != want {
				t.Errorf("%s:%d: %+v.Authorize(%v) returned error: %v, want error: %v", file, line, authorizer, ctx, got, want)
			}
		}
	}
}

func testNothingPermitted(t *testing.T, authorizer security.Authorizer) {
	_, file, line, _ := runtime.Caller(1)
	var (
		cRandom            = newChain("random").PublicID()
		cAlice             = newChain("alice")
		cVeyronAlice       = bless(cAlice.PublicID(), veyronChain, "alice", nil)
		cVeyronAliceFriend = bless(cRandom, derive(cVeyronAlice, cAlice), "friend", nil)
		cVeyronBob         = bless(cRandom, veyronChain, "bob", nil)

		tRandom = newTree("random").PublicID()
		tAlice  = newTree("alice")
		// alice#veyron/alice#google/alice
		tBlessedAlice = bless(bless(tAlice.PublicID(), veyronTree, "alice", nil), googleTree, "alice", nil)
	)
	users := []security.PublicID{
		veyronChain.PublicID(),
		veyronTree.PublicID(),
		cRandom,
		cAlice.PublicID(),
		cVeyronAlice,
		cVeyronAliceFriend,
		cVeyronBob,
		tRandom,
		tAlice.PublicID(),
		tBlessedAlice,
	}
	// No principal (whether the identity provider is trusted or not)
	// should have access to any valid or invalid label.
	for _, u := range users {
		for _, l := range security.ValidLabels {
			ctx := NewContext(ContextArgs{RemoteID: u, Label: l})
			if got := authorizer.Authorize(ctx); got == nil {
				t.Errorf("%s:%d: %+v.Authorize(%v) returns nil, want error", file, line, authorizer, ctx)
			}
		}
		invalidLabel := security.Label(3)
		ctx := NewContext(ContextArgs{RemoteID: u, Label: invalidLabel})
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
	)
	// Principals to test
	var (
		// Chain principals
		cVeyron = veyronChain.PublicID()
		pcAlice = newChain("alice")
		cAlice  = pcAlice.PublicID()
		cBob    = newChain("bob").PublicID()
		cCarol  = newChain("carol").PublicID()

		// Blessed chain principals
		cVeyronAlice       = bless(cAlice, veyronChain, "alice", nil)
		cVeyronBob         = bless(cBob, veyronChain, "bob", nil)
		cVeyronCarol       = bless(cCarol, veyronChain, "carol", nil)
		cVeyronAliceFriend = bless(cCarol, derive(cVeyronAlice, pcAlice), "friend", nil)

		// Tree principals
		ptAlice = newTree("alice")
		tAlice  = ptAlice.PublicID()
		tBob    = newTree("bob").PublicID()
		tCarol  = newTree("carol").PublicID()

		// Blessed tree principals.
		tVeyronAlice       = bless(tAlice, veyronTree, "alice", nil)
		tBlessedBob        = bless(bless(tBob, veyronTree, "bob", nil), googleTree, "bob", nil)
		tVeyronAliceFriend = bless(tCarol, derive(tVeyronAlice, ptAlice), "friend", nil)
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
		"*":              LS(R),
		"alice/*":        LS(W, R),
		"veyron/alice/*": LS(W, R),
		"veyron/bob":     LS(W),
		"veyron/alice":   LS(A, D, M),
		"google/bob/*":   LS(D, M),
	}

	// Authorizations for the above ACL.
	authorizations := authMap{
		// Self-signed identities (untrusted identity providers) have only what "*" has.
		cAlice: LS(R),
		cBob:   LS(R),
		cCarol: LS(R),
		// Self-blessed identities (tree-based ones) will also only match "*".
		tAlice: LS(R),
		tBob:   LS(R),
		tCarol: LS(R),
		// Chained identities blessed by trusted providers have more permissions.
		cVeyron:            LS(R, W, A, D, M),
		cVeyronAlice:       LS(R, W, A, D, M),
		cVeyronBob:         LS(R, W),
		cVeyronCarol:       LS(R),
		cVeyronAliceFriend: LS(R, W),
		// And tree-identities with multiple blessings will have more permissions too.
		tVeyronAlice:       LS(R, W, A, D, M),
		tBlessedBob:        LS(R, W, D, M),
		tVeyronAliceFriend: LS(R, W),
		// nil PublicIDs are not authorized.
		nil: LS(),
	}
	// Create an aclAuthorizer based on the ACL and verify the authorizations.
	authorizer := NewACLAuthorizer(acl)
	testAuthorizations(t, authorizer, authorizations)
	testSelfRPCs(t, authorizer)

	// Create a fileACLAuthorizer by saving the ACL in a file, and verify the authorizations.
	fileName := saveACLToTempFile(acl)
	defer os.Remove(fileName)
	fileAuthorizer := NewFileACLAuthorizer(fileName)
	testAuthorizations(t, fileAuthorizer, authorizations)
	testSelfRPCs(t, fileAuthorizer)

	// Modify the ACL stored in the file and verify that the authorizations appropriately change
	// for the fileACLAuthorizer.
	acl["veyron/bob"] = LS(R, W, A, D, M)
	updateACLInFile(fileName, acl)

	authorizations[cVeyronBob] = LS(R, W, A, D, M)
	authorizations[tBlessedBob] = LS(R, W, A, D, M)
	testAuthorizations(t, fileAuthorizer, authorizations)
	testSelfRPCs(t, fileAuthorizer)

	// Update the ACL file with invalid contents and verify that no requests are authorized.
	f, err := os.OpenFile(fileName, os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	f.Write([]byte("invalid ACL"))
	f.Close()
	testNothingPermitted(t, fileAuthorizer)

	// Verify that a fileACLAuthorizer based on a nonexistent file does not authorize any
	// requests.
	fileAuthorizer = NewFileACLAuthorizer("fileDoesNotExist")
	testNothingPermitted(t, fileAuthorizer)
}

func TestNilACLAuthorizer(t *testing.T) {
	authorizer := NewACLAuthorizer(nil)
	testNothingPermitted(t, authorizer)
	testSelfRPCs(t, authorizer)
}

func TestIDAuthorizer(t *testing.T) {
	// Principals to test
	var (
		// Chain principals
		cVeyron = veyronChain.PublicID()
		pcAlice = newChain("alice")
		cAlice  = pcAlice.PublicID()
		cBob    = newChain("bob").PublicID()
		cCarol  = newChain("carol").PublicID()

		// Blessed chain principals
		cVeyronAlice       = bless(cAlice, veyronChain, "alice", nil)
		cVeyronBob         = bless(cBob, veyronChain, "bob", nil)
		cVeyronAliceFriend = bless(cCarol, derive(cVeyronAlice, pcAlice), "friend", nil)

		// Tree principals
		tVeyron = veyronTree.PublicID()
		tGoogle = googleTree.PublicID()
		ptAlice = newTree("alice")
		tAlice  = ptAlice.PublicID()
		tBob    = newTree("bob").PublicID()
		tCarol  = newTree("carol").PublicID()

		// Blessed tree principals.
		tVeyronAlice            = bless(tAlice, veyronTree, "alice", nil)
		tBlessedBob             = bless(bless(tBob, veyronTree, "bob", nil), googleTree, "bob", nil)
		tGoogleCarol            = bless(tCarol, googleTree, "carol", nil)
		tVeyronAliceCarol       = bless(tCarol, derive(tVeyronAlice, ptAlice), "friend", nil)
		tVeyronAliceGoogleCarol = bless(tGoogleCarol, derive(tVeyronAlice, ptAlice), "friend", nil)
	)

	var noLabels, allLabels security.LabelSet
	for _, l := range security.ValidLabels {
		allLabels |= security.LabelSet(l)
	}

	testdata := []struct {
		authorizer     security.Authorizer
		authorizations authMap
	}{
		{
			authorizer: NewIDAuthorizer(cVeyronAlice),
			authorizations: authMap{
				cVeyron:                 allLabels,
				cVeyronAlice:            allLabels,
				cVeyronAliceFriend:      allLabels,
				tVeyron:                 allLabels,
				tVeyronAlice:            allLabels,
				tVeyronAliceCarol:       allLabels,
				tVeyronAliceGoogleCarol: allLabels,
				nil:          noLabels,
				cAlice:       noLabels,
				cBob:         noLabels,
				cCarol:       noLabels,
				cVeyronBob:   noLabels,
				tGoogle:      noLabels,
				tAlice:       noLabels,
				tBob:         noLabels,
				tCarol:       noLabels,
				tBlessedBob:  noLabels,
				tGoogleCarol: noLabels,
			},
		},
		{
			authorizer: NewIDAuthorizer(tVeyronAliceGoogleCarol),
			authorizations: authMap{
				cVeyron:                 allLabels,
				cVeyronAlice:            allLabels,
				cVeyronAliceFriend:      allLabels,
				tVeyron:                 allLabels,
				tGoogle:                 allLabels,
				tVeyronAlice:            allLabels,
				tGoogleCarol:            allLabels,
				tVeyronAliceCarol:       allLabels,
				tVeyronAliceGoogleCarol: allLabels,
				nil:         noLabels,
				cAlice:      noLabels,
				cBob:        noLabels,
				cCarol:      noLabels,
				cVeyronBob:  noLabels,
				tAlice:      noLabels,
				tBob:        noLabels,
				tCarol:      noLabels,
				tBlessedBob: noLabels,
			},
		},
	}
	for _, d := range testdata {
		testAuthorizations(t, d.authorizer, d.authorizations)
		testSelfRPCs(t, d.authorizer)
	}
}

func TestNilIDAuthorizer(t *testing.T) {
	authorizer := NewIDAuthorizer(nil)
	testNothingPermitted(t, authorizer)
	testSelfRPCs(t, authorizer)
}
