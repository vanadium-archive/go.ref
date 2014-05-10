package mounttable_test

import (
	"runtime/debug"
	"testing"
	"time"

	_ "veyron/lib/testutil"
	"veyron/runtimes/google/naming/mounttable"
	service "veyron/services/mounttable/lib"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

func boom(t *testing.T, f string, v ...interface{}) {
	t.Logf(f, v...)
	t.Fatal(string(debug.Stack()))
}

func doGlob(t *testing.T, mt naming.MountTable, pattern string) []string {
	var replies []string

	rc, err := mt.Glob(pattern)
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

func knockKnock(t *testing.T, client ipc.Client, name string) {
	call, err := client.StartCall(name, "KnockKnock", nil)
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
	if err := server.Register(mt1Prefix, service.NewMountTable()); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt2Prefix, service.NewMountTable()); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt3Prefix, service.NewMountTable()); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt4Prefix, service.NewMountTable()); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	if err := server.Register(mt5Prefix, service.NewMountTable()); err != nil {
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
	// Run a mounttable server
	server, ep := runServer(t)
	defer server.Stop()

	estr := naming.JoinAddressNameFixed(ep.String(), "")

	// Run a client, creating a new runtime for it and intializing its
	// mount table roots to point to the server created above.
	r, err := rt.New(veyron2.MountTableRoots([]string{naming.JoinAddressName(estr, mt1Prefix)}))
	if err != nil {
		boom(t, "Failed to create client runtime: %s", err)
	}
	mt := r.MountTable()

	// Create a DAG of mount table servers using relative addresses.
	ttl := time.Duration(100) * time.Second
	// Mount using a relative name starting with //.  This means don't walk out of the
	// namespace's root mount table even if there is already something mounted at mt2.
	if err := mt.Mount("//mt2", naming.JoinAddressName(estr, mt2Prefix), ttl); err != nil {
		boom(t, "Failed to Mount //mt2: %s", err)
	}
	// Mount using the relative name not starting with //.  This means walk through mt3
	// if it already exists and mount at its root.  However, since it doesn't exist, this is the
	// same as if we'd mounted at //mt3.
	//
	// NB: if we mount two replica mount table servers at the same place in the namespace,
	// we MUST use the // form or it will try to mount the second inside the first rather
	// than at the same place as the first.
	if err := mt.Mount("mt3", naming.JoinAddressName(estr, mt3Prefix), ttl); err != nil {
		boom(t, "Failed to Mount mt3: %s", err)
	}
	// Perform two mounts that have to actually walk through other mount tables.
	if err := mt.Mount("mt2/mt4", naming.JoinAddressName(estr, mt4Prefix), ttl); err != nil {
		boom(t, "Failed to Mount mt2/mt4: %s", err)
	}
	if err := mt.Mount("mt3/mt4", naming.JoinAddressName(estr, mt4Prefix), ttl); err != nil {
		boom(t, "Failed to Mount mt3/mt4: %s", err)
	}
	// Perform a mount that uses a global name as the mount point rather than one relative
	// to our namespace's root.
	global := naming.JoinAddressName(estr, "mt3/mt4/mt5")
	if err := mt.Mount(global, naming.JoinAddressName(estr, mt5Prefix), ttl); err != nil {
		boom(t, "Failed to Mount %s: %s", global, err)
	}

	// This mounts the service OA (ep/joke1) as joke1.
	if err := mt.Mount("joke1", naming.JoinAddressName(estr, "joke1"), ttl); err != nil {
		boom(t, "Failed to Mount joke1: %s", err)
	}
	// This mounts the raw server endpoint as joke2 -- like Publish would.
	if err := mt.Mount("joke2", estr, ttl); err != nil {
		boom(t, "Failed to Mount joke2: %s", err)
	}
	// This mounts the raw server endpoint as joke3 in mt3 -- like Publish would.
	if err := mt.Mount("mt3/joke3", estr, ttl); err != nil {
		boom(t, "Failed to Mount joke3: %s", err)
	}

	// Now try resolving inside the namespace.   This guarantees both that the mounts did
	// what we expected AND that we can actually resolve the results.

	// Get back an error since this will walk through mt5 to its root.
	_, err = mt.Resolve("mt3/mt4/mt5")
	if err == nil {
		boom(t, "Should have failed to mt3/mt4/mt5")
	}

	// ResolveToMountTable of mt3/mt4/mt5 does not go all the way to the root of mt5.
	servers, err := mt.ResolveToMountTable("mt3/mt4/mt5")
	if err != nil {
		boom(t, "Failed to ResolveToMountTable mt3/mt4/mt5: %s", err)
	}
	if len(servers) == 0 || servers[0] != naming.JoinAddressNameFixed(estr, "mt4/mt5") {
		boom(t, "ResolveToMountTable mt3/mt4/mt5 returned wrong servers: %v", servers)
	}

	servers, err = mt.ResolveToMountTable("mt3/mt4//mt5")
	if err != nil {
		boom(t, "Failed to ResolveToMountTable mt3/mt4//mt5: %s", err)
	}
	if len(servers) == 0 || servers[0] != naming.JoinAddressNameFixed(estr, "mt4//mt5") {
		boom(t, "ResolveToMountTable mt3/mt4//mt5 returned wrong servers: %v", servers)
	}

	servers, err = mt.ResolveToMountTable("mt3//mt4/mt5")
	if err != nil {
		boom(t, "Failed to ResolveToMountTable mt3//mt4/mt5: %s", err)
	}
	if len(servers) == 0 || servers[0] != naming.JoinAddressNameFixed(estr, "mt3//mt4/mt5") {
		boom(t, "ResolveToMountTable mt3//mt4/mt5 returned wrong servers: %v", servers)
	}

	jokeTests := []struct {
		name, resolved, resolvedToMT string
	}{
		{"joke1", naming.JoinAddressNameFixed(estr, "joke1"), naming.JoinAddressNameFixed(estr, "mt1/joke1")},
		{"joke2", estr, naming.JoinAddressNameFixed(estr, "mt1/joke2")},
		{"mt3/joke3", estr, naming.JoinAddressNameFixed(estr, "mt3/joke3")},
	}
	for _, test := range jokeTests {
		servers, err := mt.Resolve(test.name)
		if err != nil {
			boom(t, "Failed to Resolve %s: %s", test.name, err)
		}
		if len(servers) != 1 || servers[0] != test.resolved {
			boom(t, "Resolve %s returned wrong servers: %v, expected: %s", test.name, servers, test.resolved)
		}
		servers, err = mt.ResolveToMountTable(test.name)
		if err != nil {
			boom(t, "Failed to ResolveToMountTable %s: %s", test.name, err)
		}
		if len(servers) != 1 || servers[0] != test.resolvedToMT {
			boom(t, "ResolveToMountTable %s returned wrong servers: %v, expected: %s", test.name, servers, test.resolvedToMT)
		}
	}

	knockKnock(t, r.Client(), "joke1")
	knockKnock(t, r.Client(), "joke2/joke2")
	knockKnock(t, r.Client(), "mt3/joke3/joke3")

	// Try various globs.
	globTests := []struct {
		pattern  string
		expected []string
	}{
		{"*", []string{"mt2", "mt3", "joke1", "joke2"}},
		{"*/...", []string{"mt2", "mt3", "mt2/mt4", "mt3/mt4", "mt2/mt4/mt5", "mt3/mt4/mt5", "joke1", "joke2", "mt3/joke3"}},
		{"*/m?4/*5", []string{"mt2/mt4/mt5", "mt3/mt4/mt5"}},
		{"*2*/*/*5", []string{"mt2/mt4/mt5"}},
	}
	for _, test := range globTests {
		out := doGlob(t, mt, test.pattern)
		checkMatch(t, test.pattern, test.expected, out)
	}
}
