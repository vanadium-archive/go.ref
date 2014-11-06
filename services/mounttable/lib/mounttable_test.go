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

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mounttable/types"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/profiles"
)

// Simulate different processes with different runtimes.
// rootRT is the one running the mounttable service.
var rootRT, aliceRT, bobRT veyron2.Runtime

const ttlSecs = 60 * 60

func boom(t *testing.T, f string, v ...interface{}) {
	t.Logf(f, v...)
	t.Fatal(string(debug.Stack()))
}

func doMount(t *testing.T, ep, suffix, service string, shouldSucceed bool, as veyron2.Runtime) {
	name := naming.JoinAddressName(ep, suffix)
	ctx := as.NewContext()
	client := as.Client()
	call, err := client.StartCall(ctx, name, "Mount", []interface{}{service, uint32(ttlSecs), 0}, options.NoResolve(true))
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

func doUnmount(t *testing.T, ep, suffix, service string, shouldSucceed bool, as veyron2.Runtime) {
	name := naming.JoinAddressName(ep, suffix)
	ctx := as.NewContext()
	client := as.Client()
	call, err := client.StartCall(ctx, name, "Unmount", []interface{}{service}, options.NoResolve(true))
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

// resolve assumes that the mount contains 0 or 1 servers.
func resolve(name string, as veyron2.Runtime) (string, error) {
	// Resolve the name one level.
	ctx := as.NewContext()
	client := as.Client()
	call, err := client.StartCall(ctx, name, "ResolveStepX", nil, options.NoResolve(true))
	if err != nil {
		return "", err
	}
	var entry types.MountEntry
	if ierr := call.Finish(&entry, &err); ierr != nil {
		return "", ierr
	}
	if len(entry.Servers) < 1 {
		return "", errors.New("resolve returned no servers")
	}
	return naming.JoinAddressName(entry.Servers[0].Server, entry.Name), nil
}

func export(t *testing.T, name, contents string, as veyron2.Runtime) {
	// Resolve the name.
	resolved, err := resolve(name, as)
	if err != nil {
		boom(t, "Failed to Export.Resolve %s: %s", name, err)
	}
	// Export the value.
	ctx := as.NewContext()
	client := as.Client()
	call, err := client.StartCall(ctx, resolved, "Export", []interface{}{contents, true}, options.NoResolve(true))
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

func checkContents(t *testing.T, name, expected string, shouldSucceed bool, as veyron2.Runtime) {
	// Resolve the name.
	resolved, err := resolve(name, as)
	if err != nil {
		if !shouldSucceed {
			return
		}
		boom(t, "Failed to Resolve %s: %s", name, err)
	}
	// Look up the value.
	ctx := as.NewContext()
	client := as.Client()
	call, err := client.StartCall(ctx, resolved, "Lookup", nil, options.NoResolve(true))
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

func newMT(t *testing.T, acl string) (ipc.Server, string) {
	server, err := rootRT.NewServer(options.ServesMountTable(true))
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}
	// Add mount table service.
	mt, err := NewMountTable(acl)
	if err != nil {
		boom(t, "NewMountTable: %v", err)
	}
	// Start serving on a loopback address.
	e, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	if err := server.ServeDispatcher("", mt); err != nil {
		boom(t, "Failed to register mock collection: %s", err)
	}
	estr := e.String()
	t.Logf("endpoint %s", estr)
	return server, estr
}

func newCollection(t *testing.T, acl string) (ipc.Server, string) {
	server, err := rootRT.NewServer()
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}
	// Start serving on a loopback address.
	e, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	// Add a collection service.  This is just a service we can mount
	// and test against.
	cPrefix := "collection"
	if err := server.ServeDispatcher(cPrefix, newCollectionServer()); err != nil {
		boom(t, "Failed to register mock collection: %s", err)
	}
	estr := e.String()
	t.Logf("endpoint %s", estr)
	return server, estr
}

func TestMountTable(t *testing.T) {
	mt, mtAddr := newMT(t, "testdata/test.acl")
	defer mt.Stop()
	collection, collectionAddr := newCollection(t, "testdata/test.acl")
	defer collection.Stop()

	collectionName := naming.JoinAddressName(collectionAddr, "collection")

	// Mount the collection server into the mount table.
	vlog.Infof("Mount the collection server into the mount table.")
	doMount(t, mtAddr, "mounttable/stuff", collectionName, true, rootRT)

	// Create a few objects and make sure we can read them.
	vlog.Infof("Create a few objects.")
	export(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain", rootRT)
	export(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/in/spain"), "in spain", rootRT)
	export(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/falls"), "falls mainly on the plain", rootRT)
	vlog.Infof("Make sure we can read them.")
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/in/spain"), "in spain", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/falls"), "falls mainly on the plain", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable//stuff/falls"), "falls mainly on the plain", false, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/nonexistant"), "falls mainly on the plain", false, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain", true, bobRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain", false, aliceRT)

	// Test multiple mounts.
	vlog.Infof("Multiple mounts.")
	doMount(t, mtAddr, "mounttable/a/b", collectionName, true, rootRT)
	doMount(t, mtAddr, "mounttable/x/y", collectionName, true, rootRT)
	doMount(t, mtAddr, "mounttable/alpha//beta", collectionName, true, rootRT)
	vlog.Infof("Make sure we can read them.")
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/falls"), "falls mainly on the plain", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/x/y/falls"), "falls mainly on the plain", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/alpha/beta/falls"), "falls mainly on the plain", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", true, aliceRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", false, bobRT)

	// Test generic unmount.
	vlog.Info("Test generic unmount.")
	doUnmount(t, mtAddr, "mounttable/a/b", "", true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", false, rootRT)

	// Test specific unmount.
	vlog.Info("Test specific unmount.")
	doMount(t, mtAddr, "mounttable/a/b", collectionName, true, rootRT)
	doUnmount(t, mtAddr, "mounttable/a/b", collectionName, true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", false, rootRT)

	// Try timing out a mount.
	vlog.Info("Try timing out a mount.")
	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, mtAddr, "mounttable/stuffWithTTL", collectionName, true, rootRT)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuffWithTTL/the/rain"), "the rain", true, rootRT)
	ft.advance(time.Duration(ttlSecs+4) * time.Second)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuffWithTTL/the/rain"), "the rain", false, rootRT)

	// Test unauthorized mount.
	vlog.Info("Test unauthorized mount.")
	doMount(t, mtAddr, "mounttable//a/b", collectionName, false, bobRT)
	doMount(t, mtAddr, "mounttable//a/b", collectionName, false, aliceRT)

	doUnmount(t, mtAddr, "mounttable/x/y", collectionName, false, bobRT)
}

func doGlobX(t *testing.T, ep, suffix, pattern string, as veyron2.Runtime, joinServer bool) []string {
	name := naming.JoinAddressName(ep, suffix)
	ctx := as.NewContext()
	client := as.Client()
	call, err := client.StartCall(ctx, name, "Glob", []interface{}{pattern}, options.NoResolve(true))
	if err != nil {
		boom(t, "Glob.StartCall %s %s: %s", name, pattern, err)
	}
	var reply []string
	for {
		var e types.MountEntry
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

func doGlob(t *testing.T, ep, suffix, pattern string, as veyron2.Runtime) []string {
	return doGlobX(t, ep, suffix, pattern, as, false)
}

// checkMatch verified that the two slices contain the same string items, albeit
// not necessarily in the same order.  Item repetitions are allowed, but their
// numbers need to match as well.
func checkMatch(t *testing.T, want []string, got []string) {
	w := sort.StringSlice(want)
	w.Sort()
	g := sort.StringSlice(got)
	g.Sort()
	if !reflect.DeepEqual(w, g) {
		boom(t, "Glob expected %v got %v", want, got)
	}
}

func TestGlob(t *testing.T) {
	server, estr := newMT(t, "")
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, estr, "one/bright/day", fakeServer, true, rootRT)
	doMount(t, estr, "in/the/middle", fakeServer, true, rootRT)
	doMount(t, estr, "of/the/night", fakeServer, true, rootRT)

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
		out := doGlob(t, estr, "", test.in, rootRT)
		checkMatch(t, test.expected, out)
	}

	// Test Glob on a name that is under a mounted server. The result should the
	// the address the mounted server with the extra suffix.
	{
		results := doGlobX(t, estr, "of/the/night/two/dead/boys/got/up/to/fight", "*", rootRT, true)
		if len(results) != 1 {
			boom(t, "Unexpected number of results. Got %v, want 1", len(results))
		}
		_, suffix := naming.SplitAddressName(results[0])
		if expected := "quux/two/dead/boys/got/up/to/fight"; suffix != expected {
			boom(t, "Unexpected suffix. Got %v, want %v", suffix, expected)
		}
	}
}

func TestGlobACLs(t *testing.T) {
	t.Skip("Skipped until ACLs are correctly implemented for mounttable.Glob.")

	server, estr := newMT(t, "testdata/test.acl")
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, estr, "one/bright/day", fakeServer, true, rootRT)
	doMount(t, estr, "a/b/c", fakeServer, true, rootRT)

	// Try various globs.
	tests := []struct {
		as       veyron2.Runtime
		in       string
		expected []string
	}{
		{rootRT, "*", []string{"one", "a"}},
		{aliceRT, "*", []string{"one", "a"}},
		{bobRT, "*", []string{"one"}},
		{rootRT, "*/...", []string{"one", "a", "one/bright", "a/b", "one/bright/day", "a/b/c"}},
		{aliceRT, "*/...", []string{"one", "a", "one/bright", "a/b", "one/bright/day", "a/b/c"}},
		{bobRT, "*/...", []string{"one", "one/bright", "one/bright/day"}},
	}
	for _, test := range tests {
		out := doGlob(t, estr, "", test.in, test.as)
		checkMatch(t, test.expected, out)
	}
}

func TestServerFormat(t *testing.T) {
	server, estr := newMT(t, "")
	defer server.Stop()

	doMount(t, estr, "mounttable/endpoint", naming.JoinAddressName(estr, "life/on/the/mississippi"), true, rootRT)
	doMount(t, estr, "mounttable/hostport", "/atrampabroad:8000", true, rootRT)
	doMount(t, estr, "mounttable/hostport-endpoint-platypus", "/@atrampabroad:8000@@", true, rootRT)
	doMount(t, estr, "mounttable/invalid/not/rooted", "atrampabroad:8000", false, rootRT)
	doMount(t, estr, "mounttable/invalid/no/port", "/atrampabroad", false, rootRT)
	doMount(t, estr, "mounttable/invalid/endpoint", "/@following the equator:8000@@@", false, rootRT)
}

func TestExpiry(t *testing.T) {
	server, estr := newMT(t, "")
	defer server.Stop()
	collection, collectionAddr := newCollection(t, "testdata/test.acl")
	defer collection.Stop()

	collectionName := naming.JoinAddressName(collectionAddr, "collection")

	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, estr, "mounttable/a1/b1", collectionName, true, rootRT)
	doMount(t, estr, "mounttable/a1/b2", collectionName, true, rootRT)
	doMount(t, estr, "mounttable/a2/b1", collectionName, true, rootRT)
	doMount(t, estr, "mounttable/a2/b2/c", collectionName, true, rootRT)

	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, estr, "mounttable", "*/b1/...", rootRT))
	ft.advance(time.Duration(ttlSecs/2) * time.Second)
	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, estr, "mounttable", "*/b1/...", rootRT))
	checkMatch(t, []string{"c"}, doGlob(t, estr, "mounttable/a2/b2", "*", rootRT))
	// Refresh only a1/b1.  All the other mounts will expire upon the next
	// ft advance.
	doMount(t, estr, "mounttable/a1/b1", collectionName, true, rootRT)
	ft.advance(time.Duration(ttlSecs/2+4) * time.Second)
	checkMatch(t, []string{"a1"}, doGlob(t, estr, "mounttable", "*", rootRT))
	checkMatch(t, []string{"a1/b1"}, doGlob(t, estr, "mounttable", "*/b1/...", rootRT))
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
	_, err = NewMountTable("testdata/noroot.acl")
	if err == nil {
		boom(t, "Expected error for missing '/' acl")
	}
}

func init() {
	testutil.Init()
	// Create the runtime for each of the three "processes"
	rootRT = rt.Init()
	var err error
	if aliceRT, err = rt.New(); err != nil {
		panic(err)
	}
	if bobRT, err = rt.New(); err != nil {
		panic(err)
	}

	// A hack to set the namespace roots to a value that won't work.
	for _, r := range []veyron2.Runtime{rootRT, aliceRT, bobRT} {
		r.Namespace().SetRoots()
	}

	// And setup their blessings so that they present "root", "alice" and "bob"
	// and these blessings are recognized by the others.
	principals := map[string]security.Principal{
		"root":  rootRT.Principal(),
		"alice": aliceRT.Principal(),
		"bob":   bobRT.Principal(),
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
}
