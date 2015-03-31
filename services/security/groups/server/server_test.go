// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server_test

import (
	"reflect"
	"runtime/debug"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/groups"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/services/security/groups/memstore"
	"v.io/x/ref/services/security/groups/server"
	"v.io/x/ref/test/testutil"
)

//go:generate v23 test generate

func getEntriesOrDie(g groups.GroupClientStub, ctx *context.T, t *testing.T) map[groups.BlessingPatternChunk]struct{} {
	res, _, err := g.Get(ctx, groups.GetRequest{}, "")
	if err != nil {
		debug.PrintStack()
		t.Fatal("Get failed: ", err)
	}
	return res.Entries
}

func getAccessListOrDie(g groups.GroupClientStub, ctx *context.T, t *testing.T) access.Permissions {
	res, _, err := g.GetPermissions(ctx)
	if err != nil {
		debug.PrintStack()
		t.Fatal("GetPermissions failed: ", err)
	}
	return res
}

func getEtagOrDie(g groups.GroupClientStub, ctx *context.T, t *testing.T) string {
	_, etag, err := g.Get(ctx, groups.GetRequest{}, "")
	if err != nil {
		debug.PrintStack()
		t.Fatal("Get failed: ", err)
	}
	return etag
}

func bpc(chunk string) groups.BlessingPatternChunk {
	return groups.BlessingPatternChunk(chunk)
}

func bpcSet(chunks ...string) map[groups.BlessingPatternChunk]struct{} {
	res := map[groups.BlessingPatternChunk]struct{}{}
	for _, chunk := range chunks {
		res[bpc(chunk)] = struct{}{}
	}
	return res
}

func bpcSlice(chunks ...string) []groups.BlessingPatternChunk {
	res := []groups.BlessingPatternChunk{}
	for _, chunk := range chunks {
		res = append(res, bpc(chunk))
	}
	return res
}

func entriesEqual(a, b map[groups.BlessingPatternChunk]struct{}) bool {
	// Unlike DeepEqual, we treat nil and empty maps as equivalent.
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func newServer(ctx *context.T) (string, func()) {
	s, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatal("v23.NewServer() failed: ", err)
	}
	eps, err := s.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		vlog.Fatal("s.Listen() failed: ", err)
	}

	// TODO(sadovsky): Pass in an AccessList and test AccessList-checking in Group.Create().
	acl := access.Permissions{}
	m := server.NewManager(memstore.New(), acl)

	if err := s.ServeDispatcher("", m); err != nil {
		vlog.Fatal("s.ServeDispatcher() failed: ", err)
	}

	name := naming.JoinAddressName(eps[0].String(), "")
	return name, func() {
		s.Stop()
	}
}

func setupOrDie() (clientCtx *context.T, serverName string, cleanup func()) {
	ctx, shutdown := v23.Init()
	cp, sp := testutil.NewPrincipal("client"), testutil.NewPrincipal("server")

	// Have the server principal bless the client principal as "client".
	blessings, err := sp.Bless(cp.PublicKey(), sp.BlessingStore().Default(), "client", security.UnconstrainedUse())
	if err != nil {
		vlog.Fatal("sp.Bless() failed: ", err)
	}
	// Have the client present its "client" blessing when talking to the server.
	if _, err := cp.BlessingStore().Set(blessings, "server"); err != nil {
		vlog.Fatal("cp.BlessingStore().Set() failed: ", err)
	}
	// Have the client treat the server's public key as an authority on all
	// blessings that match the pattern "server".
	if err := cp.AddToRoots(blessings); err != nil {
		vlog.Fatal("cp.AddToRoots() failed: ", err)
	}

	clientCtx, err = v23.SetPrincipal(ctx, cp)
	if err != nil {
		vlog.Fatal("v23.SetPrincipal() failed: ", err)
	}
	serverCtx, err := v23.SetPrincipal(ctx, sp)
	if err != nil {
		vlog.Fatal("v23.SetPrincipal() failed: ", err)
	}

	serverName, stopServer := newServer(serverCtx)
	cleanup = func() {
		stopServer()
		shutdown()
	}
	return
}

////////////////////////////////////////
// Test cases

// TODO(sadovsky): Currently, to be safe, the implementation always returns
// NoExistOrNoAccess, and never NoExist or NoAccess. Once the implementation is
// enhanced to differentiate between these cases, we should add corresponding
// tests.

func TestCreate(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default AccessList and no entries.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Verify AccessList of created group.
	acl := access.Permissions{}
	for _, tag := range access.AllTypicalTags() {
		acl.Add(security.BlessingPattern("server/client"), string(tag))
	}
	wantAccessList, gotAccessList := acl, getAccessListOrDie(g, ctx, t)
	if !reflect.DeepEqual(wantAccessList, gotAccessList) {
		t.Errorf("AccessLists do not match: want %v, got %v", wantAccessList, gotAccessList)
	}
	// Verify entries of created group.
	want, got := bpcSet(), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}

	// Creating same group again should fail, since the group already exists.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); verror.ErrorID(err) != verror.ErrExist.ID {
		t.Fatal("Create should have failed")
	}

	// Create a group with an AccessList and a few entries, including some redundant
	// ones.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	acl = access.Permissions{}
	// Allow Admin and Read so that we can call GetPermissions and Get.
	for _, tag := range []access.Tag{access.Admin, access.Read} {
		acl.Add(security.BlessingPattern("server/client"), string(tag))
	}
	if err := g.Create(ctx, acl, bpcSlice("foo", "bar", "foo")); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Verify AccessList of created group.
	wantAccessList, gotAccessList = acl, getAccessListOrDie(g, ctx, t)
	if !reflect.DeepEqual(wantAccessList, gotAccessList) {
		t.Errorf("AccessLists do not match: want %v, got %v", wantAccessList, gotAccessList)
	}
	// Verify entries of created group.
	want, got = bpcSet("foo", "bar"), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}
}

func TestDelete(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default AccessList and no entries, check that we can delete
	// it.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Delete with bad etag should fail.
	if err := g.Delete(ctx, "20"); verror.ErrorID(err) != verror.ErrBadEtag.ID {
		t.Fatal("Delete should have failed with etag error")
	}
	// Delete with correct etag should succeed.
	etag := getEtagOrDie(g, ctx, t)
	if err := g.Delete(ctx, etag); err != nil {
		t.Fatal("Delete failed: ", err)
	}
	// Check that the group was actually deleted.
	if _, _, err := g.Get(ctx, groups.GetRequest{}, ""); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
		t.Fatal("Group was not deleted")
	}

	// Create a group with several entries, check that we can delete it.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	if err := g.Create(ctx, nil, bpcSlice("foo", "bar", "foo")); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Delete with empty etag should succeed.
	if err := g.Delete(ctx, ""); err != nil {
		t.Fatal("Delete failed: ", err)
	}
	// Check that the group was actually deleted.
	if _, _, err := g.Get(ctx, groups.GetRequest{}, ""); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
		t.Fatal("Group was not deleted")
	}
	// Check that we can recreate a group that was deleted.
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatal("Create failed: ", err)
	}

	// Create a group with an AccessList that disallows Delete(), check that Delete()
	// fails.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpC"))
	acl := access.Permissions{}
	acl.Add(security.BlessingPattern("server/client"), string(access.Admin))
	if err := g.Create(ctx, acl, nil); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Delete should fail (no access).
	if err := g.Delete(ctx, ""); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
		t.Fatal("Delete should have failed with access error")
	}
}

func TestAccessListMethods(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default AccessList and no entries.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatal("Create failed: ", err)
	}

	myacl := access.Permissions{}
	myacl.Add(security.BlessingPattern("server/client"), string(access.Admin))
	// Demonstrate that myacl differs from the default AccessList.
	if reflect.DeepEqual(myacl, getAccessListOrDie(g, ctx, t)) {
		t.Fatal("AccessLists should not match: %v", myacl)
	}

	var aclBefore, aclAfter access.Permissions
	var etagBefore, etagAfter string

	getAccessListAndEtagOrDie := func() (access.Permissions, string) {
		// Doesn't use getEtagOrDie since that requires access.Read permission.
		acl, etag, err := g.GetPermissions(ctx)
		if err != nil {
			debug.PrintStack()
			t.Fatal("GetPermissions failed: ", err)
		}
		return acl, etag
	}

	// SetPermissions with bad etag should fail.
	aclBefore, etagBefore = getAccessListAndEtagOrDie()
	if err := g.SetPermissions(ctx, myacl, "20"); verror.ErrorID(err) != verror.ErrBadEtag.ID {
		t.Fatal("SetPermissions should have failed with etag error")
	}
	// Since SetPermissions failed, the AccessList and etag should not have changed.
	aclAfter, etagAfter = getAccessListAndEtagOrDie()
	if !reflect.DeepEqual(aclBefore, aclAfter) {
		t.Errorf("AccessLists do not match: want %v, got %v", aclBefore, aclAfter)
	}
	if etagBefore != etagAfter {
		t.Errorf("Etags do not match: want %v, got %v", etagBefore, etagAfter)
	}

	// SetPermissions with correct etag should succeed.
	aclBefore, etagBefore = aclAfter, etagAfter
	if err := g.SetPermissions(ctx, myacl, etagBefore); err != nil {
		t.Fatal("SetPermissions failed: ", err)
	}
	// Check that the AccessList and etag actually changed.
	aclAfter, etagAfter = getAccessListAndEtagOrDie()
	if !reflect.DeepEqual(myacl, aclAfter) {
		t.Errorf("AccessLists do not match: want %v, got %v", myacl, aclAfter)
	}
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// SetPermissions with empty etag should succeed.
	aclBefore, etagBefore = aclAfter, etagAfter
	myacl.Add(security.BlessingPattern("server/client"), string(access.Read))
	if err := g.SetPermissions(ctx, myacl, ""); err != nil {
		t.Fatal("SetPermissions failed: ", err)
	}
	// Check that the AccessList and etag actually changed.
	aclAfter, etagAfter = getAccessListAndEtagOrDie()
	if !reflect.DeepEqual(myacl, aclAfter) {
		t.Errorf("AccessLists do not match: want %v, got %v", myacl, aclAfter)
	}
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// SetPermissions with unchanged AccessList should succeed, and etag should still change.
	aclBefore, etagBefore = aclAfter, etagAfter
	if err := g.SetPermissions(ctx, myacl, ""); err != nil {
		t.Fatal("SetPermissions failed: ", err)
	}
	// Check that the AccessList did not change and the etag did change.
	aclAfter, etagAfter = getAccessListAndEtagOrDie()
	if !reflect.DeepEqual(aclBefore, aclAfter) {
		t.Errorf("AccessLists do not match: want %v, got %v", aclBefore, aclAfter)
	}
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// Take away our access. SetPermissions and GetPermissions should fail.
	if err := g.SetPermissions(ctx, access.Permissions{}, ""); err != nil {
		t.Fatal("SetPermissions failed: ", err)
	}
	if _, _, err := g.GetPermissions(ctx); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
		t.Fatal("GetPermissions should have failed with access error")
	}
	if err := g.SetPermissions(ctx, myacl, ""); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
		t.Fatal("SetPermissions should have failed with access error")
	}
}

// Mirrors TestRemove.
func TestAdd(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default AccessList and no entries.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Verify entries of created group.
	want, got := bpcSet(), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}

	var etagBefore, etagAfter string
	etagBefore = getEtagOrDie(g, ctx, t)
	// Add with bad etag should fail.
	if err := g.Add(ctx, bpc("foo"), "20"); verror.ErrorID(err) != verror.ErrBadEtag.ID {
		t.Fatal("Add should have failed with etag error")
	}
	// Etag should not have changed.
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore != etagAfter {
		t.Errorf("Etags do not match: want %v, got %v", etagBefore, etagAfter)
	}

	// Add an entry, verify it was added and the etag changed.
	etagBefore = etagAfter
	if err := g.Add(ctx, bpc("foo"), etagBefore); err != nil {
		t.Fatal("Add failed: ", err)
	}
	want, got = bpcSet("foo"), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// Add another entry, verify it was added and the etag changed.
	etagBefore = etagAfter
	// Add with empty etag should succeed.
	if err := g.Add(ctx, bpc("bar"), ""); err != nil {
		t.Fatal("Add failed: ", err)
	}
	want, got = bpcSet("foo", "bar"), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// Add "bar" again, verify entries are still ["foo", "bar"] and the etag
	// changed.
	etagBefore = etagAfter
	if err := g.Add(ctx, bpc("bar"), etagBefore); err != nil {
		t.Fatal("Add failed: ", err)
	}
	want, got = bpcSet("foo", "bar"), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// Create a group with an AccessList that disallows Add(), check that Add() fails.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	acl := access.Permissions{}
	acl.Add(security.BlessingPattern("server/client"), string(access.Admin))
	if err := g.Create(ctx, acl, nil); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Add should fail (no access).
	if err := g.Add(ctx, bpc("foo"), ""); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
		t.Fatal("Add should have failed with access error")
	}
}

// Mirrors TestAdd.
func TestRemove(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default AccessList and two entries.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, bpcSlice("foo", "bar")); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Verify entries of created group.
	want, got := bpcSet("foo", "bar"), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}

	var etagBefore, etagAfter string
	etagBefore = getEtagOrDie(g, ctx, t)
	// Remove with bad etag should fail.
	if err := g.Remove(ctx, bpc("foo"), "20"); verror.ErrorID(err) != verror.ErrBadEtag.ID {
		t.Fatal("Remove should have failed with etag error")
	}
	// Etag should not have changed.
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore != etagAfter {
		t.Errorf("Etags do not match: want %v, got %v", etagBefore, etagAfter)
	}

	// Remove an entry, verify it was removed and the etag changed.
	etagBefore = etagAfter
	if err := g.Remove(ctx, bpc("foo"), etagBefore); err != nil {
		t.Fatal("Remove failed: ", err)
	}
	want, got = bpcSet("bar"), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// Remove another entry, verify it was removed and the etag changed.
	etagBefore = etagAfter
	// Remove with empty etag should succeed.
	if err := g.Remove(ctx, bpc("bar"), ""); err != nil {
		t.Fatal("Remove failed: ", err)
	}
	want, got = bpcSet(), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// Remove "bar" again, verify entries are still [] and the etag changed.
	etagBefore = etagAfter
	if err := g.Remove(ctx, bpc("bar"), etagBefore); err != nil {
		t.Fatal("Remove failed: ", err)
	}
	want, got = bpcSet(), getEntriesOrDie(g, ctx, t)
	if !entriesEqual(want, got) {
		t.Errorf("Entries do not match: want %v, got %v", want, got)
	}
	etagAfter = getEtagOrDie(g, ctx, t)
	if etagBefore == etagAfter {
		t.Errorf("Etags should not match: %v", etagBefore)
	}

	// Create a group with an AccessList that disallows Remove(), check that Remove()
	// fails.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	acl := access.Permissions{}
	acl.Add(security.BlessingPattern("server/client"), string(access.Admin))
	if err := g.Create(ctx, acl, bpcSlice("foo", "bar")); err != nil {
		t.Fatal("Create failed: ", err)
	}
	// Remove should fail (no access).
	if err := g.Remove(ctx, bpc("foo"), ""); verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
		t.Fatal("Remove should have failed with access error")
	}
}

func TestGet(t *testing.T) {
	// TODO(sadovsky): Implement.
}

func TestRest(t *testing.T) {
	// TODO(sadovsky): Implement.
}
