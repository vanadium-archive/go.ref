package mounttable_test

import (
	"runtime/debug"
	"testing"
	"time"

	_ "veyron/lib/testutil"
	"veyron/runtimes/google/naming/mounttable"
	service "veyron/services/mounttable/lib"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

func boom(t *testing.T, f string, v ...interface{}) {
	t.Logf(f, v...)
	t.Fatal(string(debug.Stack()))
}

func doGlob(t *testing.T, ctx context.T, mt naming.MountTable, pattern string) []string {
	var replies []string

	rc, err := mt.Glob(ctx, pattern)
	if err != nil {
		boom(t, "Glob(%s): %s", pattern, err)
	}
	for s := range rc {
		replies = append(replies, s.Name)
	}
	return replies
}

func checkMatch(t *testing.T, pattern string, expected []string, got []string) {
L:
	for _, e := range expected {
		for _, g := range got {
			if g == e {
				continue L
			}
		}
		boom(t, "Glob %s expected %v got %v", pattern, expected, got)
	}
L2:
	for _, g := range got {
		for _, e := range expected {
			if g == e {
				continue L2
			}
		}
		boom(t, "Glob %s expected %v got %v", pattern, expected, got)
	}
}

type testServer struct{}

func (*testServer) KnockKnock(call ipc.ServerCall) string {
	return "Who's there?"
}

func knockKnock(t *testing.T, runtime veyron2.Runtime, name string) {
	client := runtime.Client()
	ctx := runtime.NewContext()
	call, err := client.StartCall(ctx, name, "KnockKnock", nil)
	if err != nil {
		boom(t, "StartCall failed: %s", err)
	}
	var result string
	if err := call.Finish(&result); err != nil {
		boom(t, "Finish returned an error: %s", err)
	}
	if result != "Who's there?" {
		boom(t, "Wrong result: %v", result)
	}
}

func TestBadRoots(t *testing.T) {
	r, _ := rt.New()
	if _, err := mounttable.New(r); err != nil {
		t.Errorf("mounttable.New should not have failed with no roots")
	}
	if _, err := mounttable.New(r, "not a rooted name"); err == nil {
		t.Errorf("mounttable.New should have failed with an unrooted name")
	}
}

const (
	mt1Prefix = "mt1"
	mt2Prefix = "mt2"
	mt3Prefix = "mt3"
	mt4Prefix = "mt4"
	mt5Prefix = "mt5"
)

func testResolveToMountTable(t *testing.T, ctx context.T, mt naming.MountTable, name, want string) {
	servers, err := mt.ResolveToMountTable(ctx, name)
	if err != nil {
		boom(t, "Failed to ResolveToMountTable %q: %s", name, err)
	}
	if len(servers) != 1 || servers[0] != want {
		boom(t, "ResolveToMountTable %q returned wrong servers: got %v, want %v", name, servers, want)
	}
}

func testResolve(t *testing.T, ctx context.T, mt naming.MountTable, name, want string) {
	servers, err := mt.Resolve(ctx, name)
	if err != nil {
		boom(t, "Failed to Resolve %q: %s", name, err)
	}
	if len(servers) != 1 || servers[0] != want {
		boom(t, "Resolve %q returned wrong servers: got %v, want %v", name, servers, want)
	}
}

func newMountTable(t *testing.T) ipc.Dispatcher {
	mt, err := service.NewMountTable("")
	if err != nil {
		boom(t, "NewMountTable returned error: %v", err)
	}
	return mt
}

func runServer(t *testing.T) (ipc.Server, naming.Endpoint) {
	// We are also running a server on this runtime using stubs so we must
	// use rt.Init(). If the server were in a separate address as per usual,
	// this wouldn't be needed and we could use rt.New.
	sr := rt.Init()
	vlog.Infof("TestNamespace")
	server, err := sr.NewServer()
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}

	// Add some mount table servers.
	if err := server.Register(mt1Prefix, newMountTable(t)); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt2Prefix, newMountTable(t)); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt3Prefix, newMountTable(t)); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt4Prefix, newMountTable(t)); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt5Prefix, newMountTable(t)); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	// Add a few simple services.
	if err := server.Register("joke1", ipc.SoloDispatcher(new(testServer), nil)); err != nil {
		boom(t, "Failed to register test service: %s", err)
	}
	if err := server.Register("joke2", ipc.SoloDispatcher(new(testServer), nil)); err != nil {
		boom(t, "Failed to register test service: %s", err)
	}
	if err := server.Register("joke3", ipc.SoloDispatcher(new(testServer), nil)); err != nil {
		boom(t, "Failed to register test service: %s", err)
	}

	// Start serving on a loopback address.
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		boom(t, "Failed to Listen: %s", err)
	}
	t.Logf("endpoint %s", ep)
	return server, ep
}

func TestNamespace(t *testing.T) {
	// Run a MountTable server, which is serving MountTables on:
	// /<estr>/{mt1,mt2,mt3,mt4,mt5}
	server, ep := runServer(t)
	defer server.Stop()

	estr := ep.String()

	// Run a client, creating a new runtime for it and intializing its
	// MountTable root to point to the server created above on /<ep>/mt1.
	// This means that any relative names mounted using this local MountTable
	// will appear below mt1.
	r, err := rt.New(veyron2.MountTableRoots([]string{naming.JoinAddressName(estr, mt1Prefix)}))
	if err != nil {
		boom(t, "Failed to create client runtime: %s", err)
	}
	mt := r.MountTable()

	ctx := r.NewContext()

	// Create a DAG of mount table servers using relative addresses.
	ttl := time.Duration(100) * time.Second
	// Mount using a relative name starting with //.  This means don't walk out of the
	// namespace's root mount table even if there is already something mounted at mt2.
	mt2Name := naming.JoinAddressName(estr, mt2Prefix)
	if err := mt.Mount(ctx, "//mt2", mt2Name, ttl); err != nil {
		boom(t, "Failed to Mount //mt2: %s", err)
	}
	// Mount using the relative name not starting with //.  This means walk through mt3
	// if it already exists and mount at its root.  However, since it doesn't exist, this is the
	// same as if we'd mounted at //mt3.
	//
	// NB: if we mount two replica mount table servers at the same place in the namespace,
	// we MUST use the // form or it will try to mount the second inside the first rather
	// than at the same place as the first.
	mt3Name := naming.JoinAddressName(estr, mt3Prefix)
	if err := mt.Mount(ctx, "mt3", mt3Name, ttl); err != nil {
		boom(t, "Failed to Mount mt3: %s", err)
	}

	mt1MT := naming.MakeTerminal(naming.JoinAddressName(estr, mt1Prefix))
	mt2MT := naming.MakeTerminal(naming.JoinAddressName(estr, naming.Join(mt1Prefix, mt2Prefix)))
	mt3MT := naming.MakeTerminal(naming.JoinAddressName(estr, naming.Join(mt1Prefix, mt3Prefix)))

	// After the mounts above we have MountTables at /<estr>/mt1{//mt2,//mt3},
	// with server addresses as per below.
	testResolveToMountTable(t, ctx, mt, "", mt1MT)
	testResolveToMountTable(t, ctx, mt, "mt2", mt2MT)
	testResolveToMountTable(t, ctx, mt, "mt3", mt3MT)
	testResolveToMountTable(t, ctx, mt, "//mt3", naming.JoinAddressName(estr, "//mt1//mt3"))

	// We can resolve to the MountTables using rooted, terminal names
	// as follows, both mt1 and mt1/{mt2,mt3} are served by the
	// top-level MountTable
	testResolve(t, ctx, mt, naming.JoinAddressName(estr, "//mt1"), mt1MT)
	testResolve(t, ctx, mt, naming.JoinAddressName(estr, "//mt1/mt2"), mt2MT)
	testResolve(t, ctx, mt, naming.JoinAddressName(estr, "//mt1/mt3"), mt3MT)

	// returns [mt2, mt3]
	vlog.Infof("GLOB: %s", doGlob(t, ctx, mt, "*"))

	// Perform two mounts that have to actually walk through other mount tables.
	if err := mt.Mount(ctx, "mt2/mt4", naming.JoinAddressName(estr, mt4Prefix), ttl); err != nil {
		boom(t, "Failed to Mount mt2/mt4: %s", err)
	}
	if err := mt.Mount(ctx, "mt3/mt4", naming.JoinAddressName(estr, mt4Prefix), ttl); err != nil {
		boom(t, "Failed to Mount mt3/mt4: %s", err)
	}

	// After the mounts above we now have /<estr>{/mt1/mt2/mt4,/mt1/mt3/mt4}.
	testResolveToMountTable(t, ctx, mt, "mt2/mt4", naming.JoinAddressName(estr, "//mt2/mt4"))
	testResolveToMountTable(t, ctx, mt, "mt3/mt4", naming.JoinAddressName(estr, "//mt3/mt4"))

	testResolve(t, ctx, mt, naming.JoinAddressName(estr, "//mt1/mt2/mt4"), naming.JoinAddressName(estr, "//mt1/mt2/mt4"))

	// Perform a mount that uses a global name as the mount point rather than
	// one relative to our namespace's root.
	global := naming.JoinAddressName(estr, "mt3/mt4/mt5")
	if err := mt.Mount(ctx, global, naming.JoinAddressName(estr, mt5Prefix), ttl); err != nil {
		boom(t, "Failed to Mount %s: %s", global, err)
	}

	// This mounts the service OA (ep/joke1) as joke1.
	if err := mt.Mount(ctx, "joke1", naming.JoinAddressName(estr, "//joke1"), ttl); err != nil {
		boom(t, "Failed to Mount joke1: %s", err)
	}
	// This mounts the raw server endpoint as joke2 -- like Publish would.
	if err := mt.Mount(ctx, "joke2", naming.JoinAddressName(estr, "")+"//", ttl); err != nil {
		boom(t, "Failed to Mount joke2: %s", err)
	}
	// This mounts the raw server endpoint as joke3 in mt3 -- like Publish would.
	if err := mt.Mount(ctx, "mt3/joke3", naming.JoinAddressName(estr, "")+"//", ttl); err != nil {
		boom(t, "Failed to Mount joke3: %s", err)
	}

	// After the mounts above we have:
	// /<estr>/mt3/mt4/mt5 - the global mount above
	// /<estr>/mt1/{joke1,joke2,mt3/joker3}

	// Now try resolving inside the namespace.   This guarantees both that the mounts did
	// what we expected AND that we can actually resolve the results.

	// Get back an error since this will walk through mt5 to its root.
	_, err = mt.Resolve(ctx, "mt3/mt4/mt5")
	if err == nil {
		boom(t, "Should have failed to mt3/mt4/mt5")
	}

	// Resolving m3/mt4/mt5 to a MountTable using the local MountTable gives
	// us /<estr>//mt4/mt5.
	testResolveToMountTable(t, ctx, mt, "mt3/mt4/mt5", naming.JoinAddressName(estr, "//mt4/mt5"))
	testResolveToMountTable(t, ctx, mt, "mt3/mt4//mt5", naming.JoinAddressName(estr, "//mt4//mt5"))

	// But looking up mt4/mt5 in the local MountTable will give us
	// /<estr>//mt1/mt4/mt5 since the localMountTable has mt1 as its root!
	testResolveToMountTable(t, ctx, mt, "mt4/mt5", naming.JoinAddressName(estr, "//mt1/mt4/mt5"))

	// Looking mt3//mt4/mt5 will return the MountTable that serves //mt4/mt5.
	testResolveToMountTable(t, ctx, mt, "mt3//mt4/mt5", naming.JoinAddressName(estr, "//mt3//mt4/mt5"))
	// And the MountTable that serves //mt4/mt5 is /<epstr>//mt1/mt4/mt5
	testResolveToMountTable(t, ctx, mt, "//mt4/mt5", naming.JoinAddressName(estr, "//mt1//mt4/mt5"))

	vlog.Infof("\n-------------------------------------------------")
	jokeTests := []struct {
		name, resolved, resolvedToMT string
	}{
		{"joke1", naming.JoinAddressName(estr, "//joke1"), naming.JoinAddressName(estr, "//mt1/joke1")},
		{"joke2", naming.JoinAddressName(estr, "") + "//", naming.JoinAddressName(estr, "//mt1/joke2")},
		{"mt3/joke3", naming.JoinAddressName(estr, "") + "//", naming.JoinAddressName(estr, "//mt3/joke3")},
	}
	for _, test := range jokeTests {

		servers, err := mt.Resolve(ctx, test.name)
		if err != nil {
			boom(t, "Failed to Resolve %s: %s", test.name, err)
		}
		if len(servers) != 1 || servers[0] != test.resolved {
			boom(t, "Resolve %s returned wrong servers: %v, expected: %s", test.name, servers, test.resolved)
		}

		servers, err = mt.ResolveToMountTable(ctx, test.name)
		if err != nil {
			boom(t, "Failed to ResolveToMountTable %s: %s", test.name, err)
		}
		if len(servers) != 1 || servers[0] != test.resolvedToMT {
			boom(t, "ResolveToMountTable %s returned wrong servers: %v, expected: %s", test.name, servers, test.resolvedToMT)
		}
	}

	knockKnock(t, r, "joke1")
	knockKnock(t, r, "joke2/joke2")
	knockKnock(t, r, "mt3/joke3/joke3")

	// Try various globs.
	globTests := []struct {
		pattern  string
		expected []string
	}{
		{"*", []string{"mt2", "mt3", "joke1", "joke2"}},

		{"*/...", []string{"mt2", "mt3", "mt2/mt4", "mt3/mt4", "mt2/mt4/mt5", "mt3/mt4/mt5", "joke1", "joke2", "mt3/joke3"}},
		{"*/m?4/*5", []string{"mt2/mt4/mt5", "mt3/mt4/mt5"}},
		{"*2*/*/*5", []string{"mt2/mt4/mt5"}},
		{"mt2/*/*5", []string{"mt2/mt4/mt5"}},
		{"mt2/mt4/*5", []string{"mt2/mt4/mt5"}},
	}
	for _, test := range globTests {
		out := doGlob(t, ctx, mt, test.pattern)
		checkMatch(t, test.pattern, test.expected, out)
	}

}
