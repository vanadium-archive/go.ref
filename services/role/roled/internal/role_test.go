// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/services/role"
	irole "v.io/x/ref/services/role/roled/internal"
	"v.io/x/ref/test/testutil"

	_ "v.io/x/ref/profiles"
)

func TestSeekBlessings(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	workdir, err := ioutil.TempDir("", "test-role-server-")
	if err != nil {
		t.Fatal("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(workdir)

	// Role A is a restricted role, i.e. it can be used in sensitive ACLs.
	roleAConf := irole.Config{
		Members: []security.BlessingPattern{
			"root/users/user1/_role",
			"root/users/user2/_role",
			"root/users/user3", // _role implied
		},
		Extend: true,
	}
	irole.WriteConfig(t, roleAConf, filepath.Join(workdir, "A.conf"))

	// Role B is an unrestricted role.
	roleBConf := irole.Config{
		Members: []security.BlessingPattern{
			"root/users/user1/_role",
			"root/users/user3/_role",
		},
		Audit:  true,
		Extend: false,
	}
	irole.WriteConfig(t, roleBConf, filepath.Join(workdir, "B.conf"))

	root := testutil.NewIDProvider("root")

	var (
		user1  = newPrincipalContext(t, ctx, root, "users/user1")
		user1R = newPrincipalContext(t, ctx, root, "users/user1/_role")
		user2  = newPrincipalContext(t, ctx, root, "users/user2")
		user2R = newPrincipalContext(t, ctx, root, "users/user2/_role")
		user3  = newPrincipalContext(t, ctx, root, "users/user3")
		user3R = newPrincipalContext(t, ctx, root, "users/user3", "users/user3/_role/foo", "users/user3/_role/bar")
	)

	testServerCtx := newPrincipalContext(t, ctx, root, "testserver")
	server, testAddr := newServer(t, testServerCtx)
	tDisp := &testDispatcher{}
	if err := server.ServeDispatcher("", tDisp); err != nil {
		t.Fatalf("server.ServeDispatcher failed: %v", err)
	}

	const noErr = ""
	testcases := []struct {
		ctx       *context.T
		role      string
		errID     verror.ID
		blessings []string
	}{
		{user1, "", verror.ErrUnknownMethod.ID, nil},
		{user1, "unknown", verror.ErrNoAccess.ID, nil},
		{user2, "unknown", verror.ErrNoAccess.ID, nil},
		{user3, "unknown", verror.ErrNoAccess.ID, nil},

		{user1, "A", verror.ErrNoAccess.ID, nil},
		{user1R, "A", noErr, []string{"root/roles/A/root/users/user1"}},
		{user2, "A", verror.ErrNoAccess.ID, nil},
		{user2R, "A", noErr, []string{"root/roles/A/root/users/user2"}},
		{user3, "A", verror.ErrNoAccess.ID, nil},
		{user3R, "A", noErr, []string{"root/roles/A/root/users/user3/_role/bar", "root/roles/A/root/users/user3/_role/foo"}},

		{user1, "B", verror.ErrNoAccess.ID, nil},
		{user1R, "B", noErr, []string{"root/roles/B"}},
		{user2, "B", verror.ErrNoAccess.ID, nil},
		{user2R, "B", verror.ErrNoAccess.ID, nil},
		{user3, "B", verror.ErrNoAccess.ID, nil},
		{user3R, "B", noErr, []string{"root/roles/B"}},
	}
	addr := newRoleServer(t, newPrincipalContext(t, ctx, root, "roles"), workdir)
	for _, tc := range testcases {
		user := v23.GetPrincipal(tc.ctx).BlessingStore().Default()
		c := role.RoleClient(naming.Join(addr, tc.role))
		blessings, err := c.SeekBlessings(tc.ctx)
		if verror.ErrorID(err) != tc.errID {
			t.Errorf("unexpected error ID for (%q, %q). Got %#v, expected %#v", user, tc.role, verror.ErrorID(err), tc.errID)
		}
		if err != nil {
			continue
		}
		previousBlessings, _ := v23.GetPrincipal(tc.ctx).BlessingStore().Set(blessings, security.AllPrincipals)
		blessingNames, rejected := callTest(t, tc.ctx, testAddr)
		if !reflect.DeepEqual(blessingNames, tc.blessings) {
			t.Errorf("unexpected blessings for (%q, %q). Got %q, expected %q", user, tc.role, blessingNames, tc.blessings)
		}
		if len(rejected) != 0 {
			t.Errorf("unexpected rejected blessings for (%q, %q): %q", user, tc.role, rejected)
		}
		v23.GetPrincipal(tc.ctx).BlessingStore().Set(previousBlessings, security.AllPrincipals)
	}
}

func TestPeerBlessingCaveats(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	workdir, err := ioutil.TempDir("", "test-role-server-")
	if err != nil {
		t.Fatal("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(workdir)

	roleConf := irole.Config{
		Members: []security.BlessingPattern{"root/users/user/_role"},
		Peers: []security.BlessingPattern{
			security.BlessingPattern("root/peer1"),
			security.BlessingPattern("root/peer3"),
		},
	}
	irole.WriteConfig(t, roleConf, filepath.Join(workdir, "role.conf"))

	var (
		root  = testutil.NewIDProvider("root")
		user  = newPrincipalContext(t, ctx, root, "users/user/_role")
		peer1 = newPrincipalContext(t, ctx, root, "peer1")
		peer2 = newPrincipalContext(t, ctx, root, "peer2")
		peer3 = newPrincipalContext(t, ctx, root, "peer3")
	)

	roleAddr := newRoleServer(t, newPrincipalContext(t, ctx, root, "roles"), workdir)

	tDisp := &testDispatcher{}
	server1, testPeer1 := newServer(t, peer1)
	if err := server1.ServeDispatcher("", tDisp); err != nil {
		t.Fatalf("server.ServeDispatcher failed: %v", err)
	}
	server2, testPeer2 := newServer(t, peer2)
	if err := server2.ServeDispatcher("", tDisp); err != nil {
		t.Fatalf("server.ServeDispatcher failed: %v", err)
	}
	server3, testPeer3 := newServer(t, peer3)
	if err := server3.ServeDispatcher("", tDisp); err != nil {
		t.Fatalf("server.ServeDispatcher failed: %v", err)
	}

	c := role.RoleClient(naming.Join(roleAddr, "role"))
	blessings, err := c.SeekBlessings(user)
	if err != nil {
		t.Errorf("unexpected erro:", err)
	}
	v23.GetPrincipal(user).BlessingStore().Set(blessings, security.AllPrincipals)

	testcases := []struct {
		peer          string
		blessingNames []string
		rejectedNames []string
	}{
		{testPeer1, []string{"root/roles/role"}, nil},
		{testPeer2, nil, []string{"root/roles/role"}},
		{testPeer3, []string{"root/roles/role"}, nil},
	}
	for i, tc := range testcases {
		blessingNames, rejected := callTest(t, user, tc.peer)
		var rejectedNames []string
		for _, r := range rejected {
			rejectedNames = append(rejectedNames, r.Blessing)
		}
		if !reflect.DeepEqual(blessingNames, tc.blessingNames) {
			t.Errorf("Unexpected blessing names for #%d. Got %q, expected %q", i, blessingNames, tc.blessingNames)
		}
		if !reflect.DeepEqual(rejectedNames, tc.rejectedNames) {
			t.Errorf("Unexpected rejected names for #%d. Got %q, expected %q", i, rejectedNames, tc.rejectedNames)
		}
	}
}

func TestGlob(t *testing.T) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	workdir, err := ioutil.TempDir("", "test-role-server-")
	if err != nil {
		t.Fatal("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(workdir)
	os.Mkdir(filepath.Join(workdir, "sub1"), 0700)
	os.Mkdir(filepath.Join(workdir, "sub1", "sub2"), 0700)
	os.Mkdir(filepath.Join(workdir, "sub3"), 0700)

	// Role that user1 has access to.
	roleAConf := irole.Config{Members: []security.BlessingPattern{"root/user1"}}
	irole.WriteConfig(t, roleAConf, filepath.Join(workdir, "A.conf"))
	irole.WriteConfig(t, roleAConf, filepath.Join(workdir, "sub1/B.conf"))
	irole.WriteConfig(t, roleAConf, filepath.Join(workdir, "sub1/C.conf"))
	irole.WriteConfig(t, roleAConf, filepath.Join(workdir, "sub1/sub2/D.conf"))

	// Role that user2 has access to.
	roleBConf := irole.Config{Members: []security.BlessingPattern{"root/user2"}}
	irole.WriteConfig(t, roleBConf, filepath.Join(workdir, "sub1/sub2/X.conf"))

	root := testutil.NewIDProvider("root")
	user1 := newPrincipalContext(t, ctx, root, "user1/_role")
	user2 := newPrincipalContext(t, ctx, root, "user2/_role")
	user3 := newPrincipalContext(t, ctx, root, "user3/_role")
	addr := newRoleServer(t, newPrincipalContext(t, ctx, root, "roles"), workdir)

	testcases := []struct {
		user    *context.T
		name    string
		pattern string
		results []string
	}{
		{user1, "", "*", []string{"A", "sub1"}},
		{user1, "sub1", "*", []string{"B", "C", "sub2"}},
		{user1, "sub1/sub2", "*", []string{"D"}},
		{user1, "", "...", []string{"", "A", "sub1", "sub1/B", "sub1/C", "sub1/sub2", "sub1/sub2/D"}},
		{user2, "", "*", []string{"sub1"}},
		{user2, "", "...", []string{"", "sub1", "sub1/sub2", "sub1/sub2/X"}},
		{user3, "", "*", []string{}},
		{user3, "", "...", []string{""}},
	}
	for i, tc := range testcases {
		matches, _, _ := testutil.GlobName(tc.user, naming.Join(addr, tc.name), tc.pattern)
		if !reflect.DeepEqual(matches, tc.results) {
			t.Errorf("unexpected results for tc #%d. Got %q, expected %q", i, matches, tc.results)
		}
	}
}

func newPrincipalContext(t *testing.T, ctx *context.T, root *testutil.IDProvider, names ...string) *context.T {
	principal := testutil.NewPrincipal()
	var blessings []security.Blessings
	for _, n := range names {
		blessing, err := root.NewBlessings(principal, n)
		if err != nil {
			t.Fatal("root.Bless failed for %q: %v", n, err)
		}
		blessings = append(blessings, blessing)
	}
	bUnion, err := security.UnionOfBlessings(blessings...)
	if err != nil {
		t.Fatal("security.UnionOfBlessings failed: %v", err)
	}
	vsecurity.SetDefaultBlessings(principal, bUnion)
	ctx, err = v23.SetPrincipal(ctx, principal)
	if err != nil {
		t.Fatal("v23.SetPrincipal failed: %v", err)
	}
	return ctx
}

func newRoleServer(t *testing.T, ctx *context.T, dir string) string {
	server, addr := newServer(t, ctx)
	if err := server.ServeDispatcher("", irole.NewDispatcher(dir, addr)); err != nil {
		t.Fatalf("ServeDispatcher failed: %v", err)
	}
	return addr
}

func newServer(t *testing.T, ctx *context.T) (rpc.Server, string) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	spec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	endpoints, err := server.Listen(spec)
	if err != nil {
		t.Fatalf("Listen(%v) failed: %v", spec, err)
	}
	return server, endpoints[0].Name()
}

func callTest(t *testing.T, ctx *context.T, addr string) (blessingNames []string, rejected []security.RejectedBlessing) {
	call, err := v23.GetClient(ctx).StartCall(ctx, addr, "Test", nil)
	if err != nil {
		t.Fatalf("StartCall failed: %v", err)
	}
	if err := call.Finish(&blessingNames, &rejected); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}
	return
}

type testDispatcher struct {
}

func (d *testDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return d, d, nil
}

func (d *testDispatcher) Authorize(*context.T, security.Call) error {
	return nil
}

func (d *testDispatcher) Test(ctx *context.T, call rpc.ServerCall) ([]string, []security.RejectedBlessing, error) {
	blessings, rejected := security.RemoteBlessingNames(ctx, call.Security())
	return blessings, rejected, nil
}
