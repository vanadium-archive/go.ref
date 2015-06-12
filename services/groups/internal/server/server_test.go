// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server_test

import (
	"io/ioutil"
	"os"
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
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/groups/internal/server"
	"v.io/x/ref/services/groups/internal/store"
	"v.io/x/ref/services/groups/internal/store/gkv"
	"v.io/x/ref/services/groups/internal/store/memstore"
	"v.io/x/ref/test/testutil"
)

func Fatal(t *testing.T, args ...interface{}) {
	debug.PrintStack()
	t.Fatal(args...)
}

func Fatalf(t *testing.T, format string, args ...interface{}) {
	debug.PrintStack()
	t.Fatalf(format, args...)
}

func getEntriesOrDie(t *testing.T, ctx *context.T, g groups.GroupClientStub) map[groups.BlessingPatternChunk]struct{} {
	res, _, err := g.Get(ctx, groups.GetRequest{}, "")
	if err != nil {
		Fatalf(t, "Get failed: %v", err)
	}
	return res.Entries
}

func getPermsOrDie(t *testing.T, ctx *context.T, g groups.GroupClientStub) access.Permissions {
	res, _, err := g.GetPermissions(ctx)
	if err != nil {
		Fatalf(t, "GetPermissions failed: %v", err)
	}
	return res
}

func getVersionOrDie(t *testing.T, ctx *context.T, g groups.GroupClientStub) string {
	_, version, err := g.Get(ctx, groups.GetRequest{}, "")
	if err != nil {
		Fatalf(t, "Get failed: %v", err)
	}
	return version
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

// TODO(sadovsky): Write storage engine tests, then maybe drop this constant.
const useMemstore = false

func newServer(ctx *context.T) (string, func()) {
	s, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatal("v23.NewServer() failed: ", err)
	}
	eps, err := s.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		vlog.Fatal("s.Listen() failed: ", err)
	}

	// TODO(sadovsky): Pass in perms and test perms-checking in Group.Create().
	perms := access.Permissions{}
	var st store.Store
	var file *os.File

	if useMemstore {
		st = memstore.New()
	} else {
		file, err = ioutil.TempFile("", "")
		if err != nil {
			vlog.Fatal("ioutil.TempFile() failed: ", err)
		}
		st, err = gkv.New(file.Name())
		if err != nil {
			vlog.Fatal("gkv.New() failed: ", err)
		}
	}

	m := server.NewManager(st, perms)

	if err := s.ServeDispatcher("", m); err != nil {
		vlog.Fatal("s.ServeDispatcher() failed: ", err)
	}

	name := naming.JoinAddressName(eps[0].String(), "")
	return name, func() {
		s.Stop()
		if file != nil {
			os.Remove(file.Name())
		}
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

	clientCtx, err = v23.WithPrincipal(ctx, cp)
	if err != nil {
		vlog.Fatal("v23.WithPrincipal() failed: ", err)
	}
	serverCtx, err := v23.WithPrincipal(ctx, sp)
	if err != nil {
		vlog.Fatal("v23.WithPrincipal() failed: ", err)
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

func TestCreate(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default perms and no entries.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Verify perms of created group.
	perms := access.Permissions{}
	for _, tag := range access.AllTypicalTags() {
		perms.Add(security.BlessingPattern("server/client"), string(tag))
	}
	gotPermissions, wantPermissions := getPermsOrDie(t, ctx, g), perms
	if !reflect.DeepEqual(gotPermissions, wantPermissions) {
		t.Errorf("Permissions do not match: got %v, want %v", gotPermissions, wantPermissions)
	}
	// Verify entries of created group.
	got, want := getEntriesOrDie(t, ctx, g), bpcSet()
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}

	// Creating same group again should fail, since the group already exists.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); verror.ErrorID(err) != verror.ErrExist.ID {
		t.Fatalf("Create should have failed: %v", err)
	}

	// Create a group with perms and a few entries, including some redundant ones.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	perms = access.Permissions{}
	// Allow Admin and Read so that we can call GetPermissions and Get.
	for _, tag := range []access.Tag{access.Admin, access.Read} {
		perms.Add(security.BlessingPattern("server/client"), string(tag))
	}
	if err := g.Create(ctx, perms, bpcSlice("foo", "bar", "foo")); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Verify perms of created group.
	gotPermissions, wantPermissions = getPermsOrDie(t, ctx, g), perms
	if !reflect.DeepEqual(gotPermissions, wantPermissions) {
		t.Errorf("Permissions do not match: got %v, want %v", gotPermissions, wantPermissions)
	}
	// Verify entries of created group.
	got, want = getEntriesOrDie(t, ctx, g), bpcSet("foo", "bar")
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}
}

func TestDelete(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default perms and no entries, check that we can
	// delete it.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Delete with bad version should fail.
	if err := g.Delete(ctx, "20"); verror.ErrorID(err) != verror.ErrBadVersion.ID {
		t.Fatalf("Delete should have failed with version error: %v", err)
	}
	// Delete with correct version should succeed.
	version := getVersionOrDie(t, ctx, g)
	if err := g.Delete(ctx, version); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	// Check that the group was actually deleted.
	if _, _, err := g.Get(ctx, groups.GetRequest{}, ""); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatal("Group was not deleted")
	}

	// Create a group with several entries, check that we can delete it.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	if err := g.Create(ctx, nil, bpcSlice("foo", "bar", "foo")); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Delete with empty version should succeed.
	if err := g.Delete(ctx, ""); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	// Check that the group was actually deleted.
	if _, _, err := g.Get(ctx, groups.GetRequest{}, ""); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Fatal("Group was not deleted")
	}
	// Check that Delete is idempotent.
	if err := g.Delete(ctx, ""); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	// Check that we can recreate a group that was deleted.
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Create a group with perms that disallow Delete(), check that Delete()
	// fails.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpC"))
	perms := access.Permissions{}
	perms.Add(security.BlessingPattern("server/client"), string(access.Admin))
	if err := g.Create(ctx, perms, nil); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Delete should fail (no access).
	if err := g.Delete(ctx, ""); verror.ErrorID(err) != verror.ErrNoAccess.ID {
		t.Fatalf("Delete should have failed with access error: %v", err)
	}
}

func TestPerms(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default perms.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Use "ac" so the code below can exactly match code in syncbase.
	// TODO(sadovsky): All Vanadium {Set,Get}Permissions tests ought to share this
	// test implementation.
	ac := g

	// Mirrors syncbase/v23/syncbase/testutil/layer.go.
	myperms := access.Permissions{}
	myperms.Add(security.BlessingPattern("server/client"), string(access.Admin))
	// Demonstrate that myperms differs from the current perms.
	if reflect.DeepEqual(myperms, getPermsOrDie(t, ctx, ac)) {
		t.Fatalf("Permissions should not match: %v", myperms)
	}

	var permsBefore, permsAfter access.Permissions
	var versionBefore, versionAfter string

	getPermsAndVersionOrDie := func() (access.Permissions, string) {
		perms, version, err := ac.GetPermissions(ctx)
		if err != nil {
			// Use Fatalf rather than t.Fatalf so we get a stack trace.
			Fatalf(t, "GetPermissions failed: %v", err)
		}
		return perms, version
	}

	// SetPermissions with bad version should fail.
	permsBefore, versionBefore = getPermsAndVersionOrDie()
	if err := ac.SetPermissions(ctx, myperms, "20"); verror.ErrorID(err) != verror.ErrBadVersion.ID {
		t.Fatalf("SetPermissions should have failed with version error: %v", err)
	}
	// Since SetPermissions failed, perms and version should not have changed.
	permsAfter, versionAfter = getPermsAndVersionOrDie()
	if !reflect.DeepEqual(permsAfter, permsBefore) {
		t.Errorf("Perms do not match: got %v, want %v", permsAfter, permsBefore)
	}
	if versionAfter != versionBefore {
		t.Errorf("Versions do not match: got %v, want %v", versionAfter, versionBefore)
	}

	// SetPermissions with correct version should succeed.
	permsBefore, versionBefore = permsAfter, versionAfter
	if err := ac.SetPermissions(ctx, myperms, versionBefore); err != nil {
		t.Fatalf("SetPermissions failed: %v", err)
	}
	// Check that perms and version actually changed.
	permsAfter, versionAfter = getPermsAndVersionOrDie()
	if !reflect.DeepEqual(permsAfter, myperms) {
		t.Errorf("Perms do not match: got %v, want %v", permsAfter, myperms)
	}
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// SetPermissions with empty version should succeed.
	permsBefore, versionBefore = permsAfter, versionAfter
	myperms.Add(security.BlessingPattern("server/client"), string(access.Read))
	if err := ac.SetPermissions(ctx, myperms, ""); err != nil {
		t.Fatalf("SetPermissions failed: %v", err)
	}
	// Check that perms and version actually changed.
	permsAfter, versionAfter = getPermsAndVersionOrDie()
	if !reflect.DeepEqual(permsAfter, myperms) {
		t.Errorf("Perms do not match: got %v, want %v", permsAfter, myperms)
	}
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// SetPermissions with unchanged perms should succeed, and version should
	// still change.
	permsBefore, versionBefore = permsAfter, versionAfter
	if err := ac.SetPermissions(ctx, myperms, ""); err != nil {
		t.Fatalf("SetPermissions failed: %v", err)
	}
	// Check that perms did not change and version did change.
	permsAfter, versionAfter = getPermsAndVersionOrDie()
	if !reflect.DeepEqual(permsAfter, permsBefore) {
		t.Errorf("Perms do not match: got %v, want %v", permsAfter, permsBefore)
	}
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// Take away our access. SetPermissions and GetPermissions should fail.
	if err := ac.SetPermissions(ctx, access.Permissions{}, ""); err != nil {
		t.Fatalf("SetPermissions failed: %v", err)
	}
	if _, _, err := ac.GetPermissions(ctx); verror.ErrorID(err) != verror.ErrNoAccess.ID {
		t.Fatalf("GetPermissions should have failed with access error: %v", err)
	}
	if err := ac.SetPermissions(ctx, myperms, ""); verror.ErrorID(err) != verror.ErrNoAccess.ID {
		t.Fatalf("SetPermissions should have failed with access error: %v", err)
	}
}

// Mirrors TestRemove.
func TestAdd(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default perms and no entries.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, nil); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Verify entries of created group.
	got, want := getEntriesOrDie(t, ctx, g), bpcSet()
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}

	var versionBefore, versionAfter string
	versionBefore = getVersionOrDie(t, ctx, g)
	// Add with bad version should fail.
	if err := g.Add(ctx, bpc("foo"), "20"); verror.ErrorID(err) != verror.ErrBadVersion.ID {
		t.Fatalf("Add should have failed with version error: %v", err)
	}
	// Version should not have changed.
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionAfter != versionBefore {
		t.Errorf("Versions do not match: got %v, want %v", versionAfter, versionBefore)
	}

	// Add an entry, verify it was added and the version changed.
	versionBefore = versionAfter
	if err := g.Add(ctx, bpc("foo"), versionBefore); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	got, want = getEntriesOrDie(t, ctx, g), bpcSet("foo")
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// Add another entry, verify it was added and the version changed.
	versionBefore = versionAfter
	// Add with empty version should succeed.
	if err := g.Add(ctx, bpc("bar"), ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	got, want = getEntriesOrDie(t, ctx, g), bpcSet("foo", "bar")
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// Add "bar" again, verify entries are still ["foo", "bar"] and the version
	// changed.
	versionBefore = versionAfter
	if err := g.Add(ctx, bpc("bar"), versionBefore); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	got, want = getEntriesOrDie(t, ctx, g), bpcSet("foo", "bar")
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// Create a group with perms that disallow Add(), check that Add() fails.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	perms := access.Permissions{}
	perms.Add(security.BlessingPattern("server/client"), string(access.Admin))
	if err := g.Create(ctx, perms, nil); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Add should fail (no access).
	if err := g.Add(ctx, bpc("foo"), ""); verror.ErrorID(err) != verror.ErrNoAccess.ID {
		t.Fatalf("Add should have failed with access error: %v", err)
	}
}

// Mirrors TestAdd.
func TestRemove(t *testing.T) {
	ctx, serverName, cleanup := setupOrDie()
	defer cleanup()

	// Create a group with a default perms and two entries.
	g := groups.GroupClient(naming.JoinAddressName(serverName, "grpA"))
	if err := g.Create(ctx, nil, bpcSlice("foo", "bar")); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Verify entries of created group.
	got, want := getEntriesOrDie(t, ctx, g), bpcSet("foo", "bar")
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}

	var versionBefore, versionAfter string
	versionBefore = getVersionOrDie(t, ctx, g)
	// Remove with bad version should fail.
	if err := g.Remove(ctx, bpc("foo"), "20"); verror.ErrorID(err) != verror.ErrBadVersion.ID {
		t.Fatalf("Remove should have failed with version error: %v", err)
	}
	// Version should not have changed.
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionAfter != versionBefore {
		t.Errorf("Versions do not match: got %v, want %v", versionAfter, versionBefore)
	}

	// Remove an entry, verify it was removed and the version changed.
	versionBefore = versionAfter
	if err := g.Remove(ctx, bpc("foo"), versionBefore); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	got, want = getEntriesOrDie(t, ctx, g), bpcSet("bar")
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// Remove another entry, verify it was removed and the version changed.
	versionBefore = versionAfter
	// Remove with empty version should succeed.
	if err := g.Remove(ctx, bpc("bar"), ""); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	got, want = getEntriesOrDie(t, ctx, g), bpcSet()
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// Remove "bar" again, verify entries are still [] and the version changed.
	versionBefore = versionAfter
	if err := g.Remove(ctx, bpc("bar"), versionBefore); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	got, want = getEntriesOrDie(t, ctx, g), bpcSet()
	if !entriesEqual(got, want) {
		t.Errorf("Entries do not match: got %v, want %v", got, want)
	}
	versionAfter = getVersionOrDie(t, ctx, g)
	if versionBefore == versionAfter {
		t.Errorf("Versions should not match: %v", versionBefore)
	}

	// Create a group with perms that disallow Remove(), check that Remove()
	// fails.
	g = groups.GroupClient(naming.JoinAddressName(serverName, "grpB"))
	perms := access.Permissions{}
	perms.Add(security.BlessingPattern("server/client"), string(access.Admin))
	if err := g.Create(ctx, perms, bpcSlice("foo", "bar")); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Remove should fail (no access).
	if err := g.Remove(ctx, bpc("foo"), ""); verror.ErrorID(err) != verror.ErrNoAccess.ID {
		t.Fatalf("Remove should have failed with access error: %v", err)
	}
}

func TestGet(t *testing.T) {
	// TODO(sadovsky): Implement.
}

func TestRest(t *testing.T) {
	// TODO(sadovsky): Implement.
}
