package mounttable

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime/debug"
	"sort"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
)

// Simulate different processes with different runtimes.
// rootCtx is the one running the mounttable service.
const ttlSecs = 60 * 60

func boom(t *testing.T, f string, v ...interface{}) {
	t.Logf(f, v...)
	t.Fatal(string(debug.Stack()))
}

func doMount(t *testing.T, ctx *context.T, ep, suffix, service string, shouldSucceed bool) {
	name := naming.JoinAddressName(ep, suffix)
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "Mount", []interface{}{service, uint32(ttlSecs), 0}, options.NoResolve{})
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Mount %s onto %s: %s", service, name, err)
	}
	if ierr := call.Finish(&err); ierr != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Mount %s onto %s: %s", service, name, ierr)
	}
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Mount %s onto %s: %s", service, name, err)
	}
}

func doUnmount(t *testing.T, ctx *context.T, ep, suffix, service string, shouldSucceed bool) {
	name := naming.JoinAddressName(ep, suffix)
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "Unmount", []interface{}{service}, options.NoResolve{})
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Mount %s onto %s: %s", service, name, err)
	}
	if ierr := call.Finish(&err); ierr != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Mount %s onto %s: %s", service, name, ierr)
	}
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Mount %s onto %s: %s", service, name, err)
	}
}

func doGetACL(t *testing.T, ctx *context.T, ep, suffix string, shouldSucceed bool) (acl access.TaggedACLMap, etag string) {
	name := naming.JoinAddressName(ep, suffix)
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "GetACL", nil, options.NoResolve{})
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to GetACL %s: %s", name, err)
	}
	if ierr := call.Finish(&acl, &etag, &err); ierr != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to GetACL %s: %s", name, ierr)
	}
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to GetACL %s: %s", name, err)
	}
	return
}

func doSetACL(t *testing.T, ctx *context.T, ep, suffix string, acl access.TaggedACLMap, etag string, shouldSucceed bool) {
	name := naming.JoinAddressName(ep, suffix)
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "SetACL", []interface{}{acl, etag}, options.NoResolve{})
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to SetACL %s: %s", name, err)
	}
	if ierr := call.Finish(&err); ierr != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to SetACL %s: %s", name, ierr)
	}
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to SetACL %s: %s", name, err)
	}
	return
}

func doDeleteNode(t *testing.T, ctx *context.T, ep, suffix string, shouldSucceed bool) {
	name := naming.JoinAddressName(ep, suffix)
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "Delete", []interface{}{false}, options.NoResolve{})
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Delete node %s: %s", name, err)
	}
	if ierr := call.Finish(&err); ierr != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Delete node %s: %s", name, ierr)
	}
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Delete node %s: %s", name, err)
	}
}

func doDeleteSubtree(t *testing.T, ctx *context.T, ep, suffix string, shouldSucceed bool) {
	name := naming.JoinAddressName(ep, suffix)
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "Delete", []interface{}{true}, options.NoResolve{})
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Delete subtree %s: %s", name, err)
	}
	if ierr := call.Finish(&err); ierr != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Delete subtree %s: %s", name, ierr)
	}
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Delete subtree %s: %s", name, err)
	}
}

// resolve assumes that the mount contains 0 or 1 servers.
func resolve(ctx *context.T, name string) (string, error) {
	// Resolve the name one level.
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "ResolveStep", nil, options.NoResolve{})
	if err != nil {
		return "", err
	}
	var entry naming.VDLMountEntry
	if ierr := call.Finish(&entry, &err); ierr != nil {
		return "", ierr
	}
	if len(entry.Servers) < 1 {
		return "", errors.New("resolve returned no servers")
	}
	return naming.JoinAddressName(entry.Servers[0].Server, entry.Name), nil
}

func export(t *testing.T, ctx *context.T, name, contents string) {
	// Resolve the name.
	resolved, err := resolve(ctx, name)
	if err != nil {
		boom(t, "Failed to Export.Resolve %s: %s", name, err)
	}
	// Export the value.
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, resolved, "Export", []interface{}{contents, true}, options.NoResolve{})
	if err != nil {
		boom(t, "Failed to Export.StartCall %s to %s: %s", name, contents, err)
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	if err != nil {
		boom(t, "Failed to Export.StartCall %s to %s: %s", name, contents, err)
	}
}

func checkContents(t *testing.T, ctx *context.T, name, expected string, shouldSucceed bool) {
	// Resolve the name.
	resolved, err := resolve(ctx, name)
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Resolve %s: %s", name, err)
	}
	// Look up the value.
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, resolved, "Lookup", nil, options.NoResolve{})
	if err != nil {
		if shouldSucceed {
			boom(t, "Failed Lookup.StartCall %s: %s", name, err)
		}
		return
	}
	var contents []byte
	if ierr := call.Finish(&contents, &err); ierr != nil {
		err = ierr
	}
	if err != nil {
		if shouldSucceed {
			boom(t, "Failed to Lookup %s: %s", name, err)
		}
		return
	}
	if string(contents) != expected {
		boom(t, "Lookup %s, expected %q, got %q", name, expected, contents)
	}
	if !shouldSucceed {
		boom(t, "Lookup %s, expected failure, got %q", name, contents)
	}
}

func newMT(t *testing.T, acl string, rootCtx *context.T) (ipc.Server, string) {
	server, err := veyron2.NewServer(rootCtx, options.ServesMountTable(true))
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}
	// Add mount table service.
	mt, err := NewMountTable(acl)
	if err != nil {
		boom(t, "NewMountTable: %v", err)
	}
	// Start serving on a loopback address.
	eps, err := server.Listen(veyron2.GetListenSpec(rootCtx))
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	if err := server.ServeDispatcher("", mt); err != nil {
		boom(t, "Failed to register mock collection: %s", err)
	}
	estr := eps[0].String()
	t.Logf("endpoint %s", estr)
	return server, estr
}

func newCollection(t *testing.T, acl string, rootCtx *context.T) (ipc.Server, string) {
	server, err := veyron2.NewServer(rootCtx)
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}
	// Start serving on a loopback address.
	eps, err := server.Listen(veyron2.GetListenSpec(rootCtx))
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	// Add a collection service.  This is just a service we can mount
	// and test against.
	cPrefix := "collection"
	if err := server.ServeDispatcher(cPrefix, newCollectionServer()); err != nil {
		boom(t, "Failed to register mock collection: %s", err)
	}
	estr := eps[0].String()
	t.Logf("endpoint %s", estr)
	return server, estr
}

func TestMountTable(t *testing.T) {
	rootCtx, aliceCtx, bobCtx, shutdown := initTest()
	defer shutdown()

	mt, mtAddr := newMT(t, "testdata/test.acl", rootCtx)
	defer mt.Stop()
	collection, collectionAddr := newCollection(t, "testdata/test.acl", rootCtx)
	defer collection.Stop()

	collectionName := naming.JoinAddressName(collectionAddr, "collection")

	// Mount the collection server into the mount table.
	vlog.Infof("Mount the collection server into the mount table.")
	doMount(t, rootCtx, mtAddr, "stuff", collectionName, true)

	// Create a few objects and make sure we can read them.
	vlog.Infof("Create a few objects.")
	export(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/the/rain"), "the rain")
	export(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/in/spain"), "in spain")
	export(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/falls"), "falls mainly on the plain")
	vlog.Infof("Make sure we can read them.")
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/the/rain"), "the rain", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/in/spain"), "in spain", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/falls"), "falls mainly on the plain", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "/stuff/falls"), "falls mainly on the plain", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/nonexistant"), "falls mainly on the plain", false)
	checkContents(t, bobCtx, naming.JoinAddressName(mtAddr, "stuff/the/rain"), "the rain", true)
	checkContents(t, aliceCtx, naming.JoinAddressName(mtAddr, "stuff/the/rain"), "the rain", false)

	// Test multiple mounts.
	vlog.Infof("Multiple mounts.")
	doMount(t, rootCtx, mtAddr, "a/b", collectionName, true)
	doMount(t, rootCtx, mtAddr, "x/y", collectionName, true)
	doMount(t, rootCtx, mtAddr, "alpha//beta", collectionName, true)
	vlog.Infof("Make sure we can read them.")
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "stuff/falls"), "falls mainly on the plain", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "a/b/falls"), "falls mainly on the plain", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "x/y/falls"), "falls mainly on the plain", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "alpha/beta/falls"), "falls mainly on the plain", true)
	checkContents(t, aliceCtx, naming.JoinAddressName(mtAddr, "a/b/falls"), "falls mainly on the plain", true)
	checkContents(t, bobCtx, naming.JoinAddressName(mtAddr, "a/b/falls"), "falls mainly on the plain", false)

	// Test getting/setting ACLs.
	acl, etag := doGetACL(t, rootCtx, mtAddr, "stuff", true)
	doSetACL(t, rootCtx, mtAddr, "stuff", acl, "xyzzy", false) // bad etag
	doSetACL(t, rootCtx, mtAddr, "stuff", acl, etag, true)     // good etag
	_, netag := doGetACL(t, rootCtx, mtAddr, "stuff", true)
	if netag == etag {
		boom(t, "etag didn't change after SetACL: %s", netag)
	}
	doSetACL(t, rootCtx, mtAddr, "stuff", acl, "", true) // no etag

	// Bob should be able to create nodes under the mounttable root but not alice.
	doSetACL(t, aliceCtx, mtAddr, "onlybob", acl, "", false)
	doSetACL(t, bobCtx, mtAddr, "onlybob", acl, "", true)

	// Test generic unmount.
	vlog.Info("Test generic unmount.")
	doUnmount(t, rootCtx, mtAddr, "a/b", "", true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "a/b/falls"), "falls mainly on the plain", false)

	// Test specific unmount.
	vlog.Info("Test specific unmount.")
	doMount(t, rootCtx, mtAddr, "a/b", collectionName, true)
	doUnmount(t, rootCtx, mtAddr, "a/b", collectionName, true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "a/b/falls"), "falls mainly on the plain", false)

	// Try timing out a mount.
	vlog.Info("Try timing out a mount.")
	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, rootCtx, mtAddr, "stuffWithTTL", collectionName, true)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "stuffWithTTL/the/rain"), "the rain", true)
	ft.advance(time.Duration(ttlSecs+4) * time.Second)
	checkContents(t, rootCtx, naming.JoinAddressName(mtAddr, "stuffWithTTL/the/rain"), "the rain", false)

	// Test unauthorized mount.
	vlog.Info("Test unauthorized mount.")
	doMount(t, bobCtx, mtAddr, "/a/b", collectionName, false)
	doMount(t, aliceCtx, mtAddr, "/a/b", collectionName, false)

	doUnmount(t, bobCtx, mtAddr, "x/y", collectionName, false)
}

func doGlobX(t *testing.T, ctx *context.T, ep, suffix, pattern string, joinServer bool) []string {
	name := naming.JoinAddressName(ep, suffix)
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, ipc.GlobMethod, []interface{}{pattern}, options.NoResolve{})
	if err != nil {
		boom(t, "Glob.StartCall %s %s: %s", name, pattern, err)
	}
	var reply []string
	for {
		var e naming.VDLMountEntry
		err := call.Recv(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			boom(t, "Glob.StartCall %s: %s", name, pattern, err)
		}
		if joinServer && len(e.Servers) > 0 {
			reply = append(reply, naming.JoinAddressName(e.Servers[0].Server, e.Name))
		} else {
			reply = append(reply, e.Name)
		}
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	if err != nil {
		boom(t, "Glob.Finish %s: %s", name, pattern, err)
	}
	return reply
}

func doGlob(t *testing.T, ctx *context.T, ep, suffix, pattern string) []string {
	return doGlobX(t, ctx, ep, suffix, pattern, false)
}

// checkMatch verified that the two slices contain the same string items, albeit
// not necessarily in the same order.  Item repetitions are allowed, but their
// numbers need to match as well.
func checkMatch(t *testing.T, want []string, got []string) {
	if len(want) == 0 && len(got) == 0 {
		return
	}
	w := sort.StringSlice(want)
	w.Sort()
	g := sort.StringSlice(got)
	g.Sort()
	if !reflect.DeepEqual(w, g) {
		boom(t, "Glob expected %v got %v", want, got)
	}
}

// checkExists makes sure a name exists (or not).
func checkExists(t *testing.T, ctx *context.T, ep, suffix string, shouldSucceed bool) {
	x := doGlobX(t, ctx, ep, "", suffix, false)
	if len(x) != 1 || x[0] != suffix {
		if shouldSucceed {
			boom(t, "Failed to find %s", suffix)
		}
		return
	}
	if !shouldSucceed {
		boom(t, "%s exists but shouldn't", suffix)
	}
}

func TestGlob(t *testing.T) {
	rootCtx, shutdown := testutil.InitForTest()
	defer shutdown()

	server, estr := newMT(t, "", rootCtx)
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, rootCtx, estr, "one/bright/day", fakeServer, true)
	doMount(t, rootCtx, estr, "in/the/middle", fakeServer, true)
	doMount(t, rootCtx, estr, "of/the/night", fakeServer, true)

	// Try various globs.
	tests := []struct {
		in       string
		expected []string
	}{
		{"*", []string{"one", "in", "of"}},
		{"...", []string{"", "one", "in", "of", "one/bright", "in/the", "of/the", "one/bright/day", "in/the/middle", "of/the/night"}},
		{"*/...", []string{"one", "in", "of", "one/bright", "in/the", "of/the", "one/bright/day", "in/the/middle", "of/the/night"}},
		{"one/...", []string{"one", "one/bright", "one/bright/day"}},
		{"of/the/night/two/dead/boys", []string{"of/the/night"}},
		{"*/the", []string{"in/the", "of/the"}},
		{"*/the/...", []string{"in/the", "of/the", "in/the/middle", "of/the/night"}},
		{"o*", []string{"one", "of"}},
		{"", []string{""}},
	}
	for _, test := range tests {
		out := doGlob(t, rootCtx, estr, "", test.in)
		checkMatch(t, test.expected, out)
	}

	// Test Glob on a name that is under a mounted server. The result should the
	// the address the mounted server with the extra suffix.
	{
		results := doGlobX(t, rootCtx, estr, "of/the/night/two/dead/boys/got/up/to/fight", "*", true)
		if len(results) != 1 {
			boom(t, "Unexpected number of results. Got %v, want 1", len(results))
		}
		_, suffix := naming.SplitAddressName(results[0])
		if expected := "quux/two/dead/boys/got/up/to/fight"; suffix != expected {
			boom(t, "Unexpected suffix. Got %v, want %v", suffix, expected)
		}
	}
}

func TestACLTemplate(t *testing.T) {
	rootCtx, aliceCtx, bobCtx, shutdown := initTest()
	defer shutdown()

	server, estr := newMT(t, "testdata/test.acl", rootCtx)
	defer server.Stop()
	fakeServer := naming.JoinAddressName(estr, "quux")

	// Noone should be able to mount on someone else's names.
	doMount(t, aliceCtx, estr, "users/ted", fakeServer, false)
	doMount(t, bobCtx, estr, "users/carol", fakeServer, false)
	doMount(t, rootCtx, estr, "users/george", fakeServer, false)

	// Anyone should be able to mount on their own names.
	doMount(t, aliceCtx, estr, "users/alice", fakeServer, true)
	doMount(t, bobCtx, estr, "users/bob", fakeServer, true)
	doMount(t, rootCtx, estr, "users/root", fakeServer, true)
}

func TestGlobACLs(t *testing.T) {
	rootCtx, aliceCtx, bobCtx, shutdown := initTest()
	defer shutdown()

	server, estr := newMT(t, "testdata/test.acl", rootCtx)
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, aliceCtx, estr, "one/bright/day", fakeServer, false) // Fails because alice can't mount there.
	doMount(t, bobCtx, estr, "one/bright/day", fakeServer, true)
	doMount(t, rootCtx, estr, "a/b/c", fakeServer, true)

	// Try various globs.
	tests := []struct {
		ctx      *context.T
		in       string
		expected []string
	}{
		{rootCtx, "*", []string{"one", "a", "stuff", "users"}},
		{aliceCtx, "*", []string{"one", "a", "users"}},
		{bobCtx, "*", []string{"one", "stuff", "users"}},
		// bob, alice, and root have different visibility to the space.
		{rootCtx, "*/...", []string{"one", "a", "one/bright", "a/b", "one/bright/day", "a/b/c", "stuff", "users"}},
		{aliceCtx, "*/...", []string{"one", "a", "one/bright", "a/b", "one/bright/day", "a/b/c", "users"}},
		{bobCtx, "*/...", []string{"one", "one/bright", "one/bright/day", "stuff", "users"}},
	}
	for _, test := range tests {
		out := doGlob(t, test.ctx, estr, "", test.in)
		checkMatch(t, test.expected, out)
	}
}

func TestCleanup(t *testing.T) {
	rootCtx, shutdown := testutil.InitForTest()
	defer shutdown()

	server, estr := newMT(t, "", rootCtx)
	defer server.Stop()

	// Set up one mount.
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, rootCtx, estr, "one/bright/day", fakeServer, true)
	checkMatch(t, []string{"one", "one/bright", "one/bright/day"}, doGlob(t, rootCtx, estr, "", "*/..."))

	// After the unmount nothing should be left
	doUnmount(t, rootCtx, estr, "one/bright/day", "", true)
	checkMatch(t, nil, doGlob(t, rootCtx, estr, "", "*/..."))

	// Set up a mount, then set the ACL.
	doMount(t, rootCtx, estr, "one/bright/day", fakeServer, true)
	checkMatch(t, []string{"one", "one/bright", "one/bright/day"}, doGlob(t, rootCtx, estr, "", "*/..."))
	acl := access.TaggedACLMap{"Read": access.ACL{In: []security.BlessingPattern{security.BlessingPattern("...")}}}
	doSetACL(t, rootCtx, estr, "one/bright", acl, "", true)

	// After the unmount we should still have everything above the ACL.
	doUnmount(t, rootCtx, estr, "one/bright/day", "", true)
	checkMatch(t, []string{"one", "one/bright"}, doGlob(t, rootCtx, estr, "", "*/..."))
}

func TestDelete(t *testing.T) {
	rootCtx, aliceCtx, bobCtx, shutdown := initTest()
	defer shutdown()

	server, estr := newMT(t, "testdata/test.acl", rootCtx)
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, bobCtx, estr, "one/bright/day", fakeServer, true)
	doMount(t, rootCtx, estr, "a/b/c", fakeServer, true)

	// It shouldn't be possible to delete anything with children unless explicitly requested.
	doDeleteNode(t, rootCtx, estr, "a/b", false)
	checkExists(t, rootCtx, estr, "a/b", true)
	doDeleteSubtree(t, rootCtx, estr, "a/b", true)
	checkExists(t, rootCtx, estr, "a/b", false)

	// Alice shouldn't be able to delete what bob created but bob and root should.
	doDeleteNode(t, aliceCtx, estr, "one/bright/day", false)
	checkExists(t, rootCtx, estr, "one/bright/day", true)
	doDeleteNode(t, rootCtx, estr, "one/bright/day", true)
	checkExists(t, rootCtx, estr, "one/bright/day", false)
	doDeleteNode(t, bobCtx, estr, "one/bright", true)
	checkExists(t, rootCtx, estr, "one/bright", false)
}

func TestServerFormat(t *testing.T) {
	rootCtx, shutdown := testutil.InitForTest()
	defer shutdown()

	server, estr := newMT(t, "", rootCtx)
	defer server.Stop()

	doMount(t, rootCtx, estr, "endpoint", naming.JoinAddressName(estr, "life/on/the/mississippi"), true)
	doMount(t, rootCtx, estr, "hostport", "/atrampabroad:8000", true)
	doMount(t, rootCtx, estr, "hostport-endpoint-platypus", "/@atrampabroad:8000@@", true)
	doMount(t, rootCtx, estr, "invalid/not/rooted", "atrampabroad:8000", false)
	doMount(t, rootCtx, estr, "invalid/no/port", "/atrampabroad", false)
	doMount(t, rootCtx, estr, "invalid/endpoint", "/@following the equator:8000@@@", false)
}

func TestExpiry(t *testing.T) {
	rootCtx, shutdown := testutil.InitForTest()
	defer shutdown()

	server, estr := newMT(t, "", rootCtx)
	defer server.Stop()
	collection, collectionAddr := newCollection(t, "testdata/test.acl", rootCtx)
	defer collection.Stop()

	collectionName := naming.JoinAddressName(collectionAddr, "collection")

	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, rootCtx, estr, "a1/b1", collectionName, true)
	doMount(t, rootCtx, estr, "a1/b2", collectionName, true)
	doMount(t, rootCtx, estr, "a2/b1", collectionName, true)
	doMount(t, rootCtx, estr, "a2/b2/c", collectionName, true)

	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, rootCtx, estr, "", "*/b1/..."))
	ft.advance(time.Duration(ttlSecs/2) * time.Second)
	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, rootCtx, estr, "", "*/b1/..."))
	checkMatch(t, []string{"c"}, doGlob(t, rootCtx, estr, "a2/b2", "*"))
	// Refresh only a1/b1.  All the other mounts will expire upon the next
	// ft advance.
	doMount(t, rootCtx, estr, "a1/b1", collectionName, true)
	ft.advance(time.Duration(ttlSecs/2+4) * time.Second)
	checkMatch(t, []string{"a1", "a1/b1"}, doGlob(t, rootCtx, estr, "", "*/..."))
	checkMatch(t, []string{"a1/b1"}, doGlob(t, rootCtx, estr, "", "*/b1/..."))
}

func TestBadACLs(t *testing.T) {
	_, err := NewMountTable("testdata/invalid.acl")
	if err == nil {
		boom(t, "Expected json parse error in acl file")
	}
	_, err = NewMountTable("testdata/doesntexist.acl")
	if err == nil {
		boom(t, "Expected error from missing acl file")
	}
}

func initTest() (rootCtx *context.T, aliceCtx *context.T, bobCtx *context.T, shutdown veyron2.Shutdown) {
	testutil.Init()
	ctx, shutdown := testutil.InitForTest()

	var err error
	if rootCtx, err = veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("root")); err != nil {
		panic("failed to set root principal")
	}
	if aliceCtx, err = veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("alice")); err != nil {
		panic("failed to set alice principal")
	}
	if bobCtx, err = veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("bob")); err != nil {
		panic("failed to set bob principal")
	}

	// A hack to set the namespace roots to a value that won't work.
	for _, r := range []*context.T{rootCtx, aliceCtx, bobCtx} {
		veyron2.GetNamespace(r).SetRoots()
	}

	// And setup their blessings so that they present "root", "alice" and "bob"
	// and these blessings are recognized by the others.
	principals := map[string]security.Principal{
		"root":  veyron2.GetPrincipal(rootCtx),
		"alice": veyron2.GetPrincipal(aliceCtx),
		"bob":   veyron2.GetPrincipal(bobCtx),
	}
	for name, p := range principals {
		blessing, err := p.BlessSelf(name)
		if err != nil {
			panic(fmt.Sprintf("BlessSelf(%q) failed: %v", name, err))
		}
		// Share this blessing with all servers and use it when serving clients.
		if err = p.BlessingStore().SetDefault(blessing); err != nil {
			panic(fmt.Sprintf("%v: %v", blessing, err))
		}
		if _, err = p.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
			panic(fmt.Sprintf("%v: %v", blessing, err))
		}
		// Have all principals trust the root of this blessing.
		for _, other := range principals {
			if err := other.AddToRoots(blessing); err != nil {
				panic(err)
			}
		}
	}

	return rootCtx, aliceCtx, bobCtx, shutdown
}
