package mounttable

import (
	"errors"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"

	_ "veyron/lib/testutil"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mounttable"
	"veyron2/services/mounttable/types"
	"veyron2/vlog"
)

// stupidNS is a version of naming.Namespace that we can control.  This exists so that we have some
// firm ground to stand on vis a vis the stub interface.
type stupidNS struct {
	id ipc.ClientOpt
}

var (
	rootID  = veyron2.LocalID(security.FakePublicID("root"))
	bobID   = veyron2.LocalID(security.FakePublicID("bob"))
	aliceID = veyron2.LocalID(security.FakePublicID("alice"))
)

const ttlSecs = 60 * 60

func boom(t *testing.T, f string, v ...interface{}) {
	t.Logf(f, v...)
	t.Fatal(string(debug.Stack()))
}

// quuxClient returns an ipc.Client that uses the simple namespace for name
// resolution.
func quuxClient(id ipc.ClientOpt) ipc.Client {
	ns := stupidNS{id}
	c, err := rt.R().NewClient(id, veyron2.Namespace(ns))
	if err != nil {
		panic(err)
	}
	return c
}

func (stupidNS) Mount(context.T, string, string, time.Duration) error {
	return errors.New("unimplemented")
}

func (stupidNS) Unmount(context.T, string, string) error {
	return errors.New("unimplemented")
}

// Resolve will only go one level deep, i.e., it doesn't walk past the first mount point.
func (ns stupidNS) Resolve(ctx context.T, name string) ([]string, error) {
	vlog.VI(1).Infof("MyResolve %q", name)
	address, suffix := naming.SplitAddressName(name)
	if len(address) == 0 {
		return nil, naming.ErrNoSuchName
	}
	if strings.HasPrefix(suffix, "//") {
		// We're done, the server at address will handle the name.
		return []string{naming.JoinAddressName(address, suffix)}, nil
	}

	// Resolve via another
	objectPtr, err := mounttable.BindMountTable("/"+address+"//"+suffix, quuxClient(ns.id))
	if err != nil {
		return nil, err
	}
	ss, suffix, err := objectPtr.ResolveStep(rt.R().NewContext())
	if err != nil {
		return nil, err
	}
	var servers []string
	for _, s := range ss {
		servers = append(servers, naming.Join(s.Server, suffix))
	}
	vlog.VI(1).Infof("-> %v", servers)
	return servers, nil
}

func (s stupidNS) Unresolve(ctx context.T, name string) ([]string, error) {
	return s.Resolve(ctx, name)
}

func (stupidNS) ResolveToMountTable(ctx context.T, name string) ([]string, error) {
	return nil, errors.New("ResolveToMountTable is not implemented in this MountTable")
}

func (stupidNS) FlushCacheEntry(name string) bool {
	return false
}

func (stupidNS) CacheCtl(ctls ...naming.CacheCtl) []naming.CacheCtl {
	return nil
}

// Glob implements naming.MountTable.Glob.
func (stupidNS) Glob(ctx context.T, pattern string) (chan naming.MountEntry, error) {
	return nil, errors.New("Glob is not implemented in this MountTable")
}

func (s stupidNS) SetRoots(...string) error {
	return nil
}

func (s stupidNS) Roots() []string {
	return []string{}
}

func doMount(t *testing.T, name, service string, shouldSucceed bool, id ipc.ClientOpt) {
	mtpt, err := mounttable.BindMountTable(name, quuxClient(id))
	if err != nil {
		boom(t, "Failed to BindMountTable: %s", err)
	}
	if err := mtpt.Mount(rt.R().NewContext(), service, uint32(ttlSecs)); err != nil {
		if shouldSucceed {
			boom(t, "Failed to Mount %s onto %s: %s", service, name, err)
		}
	} else if !shouldSucceed {
		boom(t, "doMount %s onto %s, expected failure but succeeded", service, name)
	}
}

func doUnmount(t *testing.T, name, service string, shouldSucceed bool, id ipc.ClientOpt) {
	mtpt, err := mounttable.BindMountTable(name, quuxClient(id))
	if err != nil {
		boom(t, "Failed to BindMountTable: %s", err)
	}
	if err := mtpt.Unmount(rt.R().NewContext(), service); err != nil {
		if shouldSucceed {
			boom(t, "Failed to Unmount %s onto %s: %s", service, name, err)
		}
	} else if !shouldSucceed {
		boom(t, "doUnmount %s onto %s, expected failure but succeeded", service, name)
	}
}

func create(t *testing.T, name, contents string) {
	objectPtr, err := BindCollection(name, quuxClient(rootID))
	if err != nil {
		boom(t, "Failed to BindCollection: %s", err)
	}
	if err := objectPtr.Export(rt.R().NewContext(), contents, true); err != nil {
		boom(t, "Failed to Export %s to %s: %s", name, contents, err)
	}
}

func checkContents(t *testing.T, name, expected string, shouldSucceed bool, id ipc.ClientOpt) {
	objectPtr, err := BindCollection(name, quuxClient(id))
	if err != nil {
		boom(t, "Failed to BindCollection: %s", err)
	}
	contents, err := objectPtr.Lookup(rt.R().NewContext())
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
	// It is necessary for the private key of runtime's identity and
	// the public key of the LocalIDOpts passed to clients to correspond.
	// Since the LocalIDOpts are FakePublicIDs, we initialize the runtime
	// below with a FakePrivateID. (Note all FakePublicIDs and FakePrivateIDs
	// always have corresponding public and private keys respectively.)
	r := rt.Init(veyron2.RuntimeID(security.FakePrivateID("irrelevant")))
	server, err := r.NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}
	// Add mount table service.
	mt, err := NewMountTable(acl)
	if err != nil {
		boom(t, "NewMountTable: %v", err)
	}
	// Start serving on a loopback address.
	e, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	if err := server.Serve("", mt); err != nil {
		boom(t, "Failed to register mock collection: %s", err)
	}
	estr := e.String()
	t.Logf("endpoint %s", estr)
	return server, estr
}

func newCollection(t *testing.T, acl string) (ipc.Server, string) {
	r := rt.Init()
	server, err := r.NewServer()
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}
	// Start serving on a loopback address.
	e, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	// Add a collection service.  This is just a service we can mount
	// and test against.
	cPrefix := "collection"
	if err := server.Serve(cPrefix, newCollectionServer()); err != nil {
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
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable/stuff"), collectionName, true, rootID)

	// Create a few objects and make sure we can read them.
	vlog.Infof("Create a few objects.")
	create(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain")
	create(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/in/spain"), "in spain")
	create(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/falls"), "falls mainly on the plain")
	vlog.Infof("Make sure we can read them.")
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/in/spain"), "in spain", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable//stuff/falls"), "falls mainly on the plain", false, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/nonexistant"), "falls mainly on the plain", false, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain", true, bobID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/the/rain"), "the rain", false, aliceID)

	// Test multiple mounts.
	vlog.Infof("Multiple mounts.")
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable//a/b"), collectionName, true, rootID)
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable/x/y"), collectionName, true, rootID)
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable/alpha//beta"), collectionName, true, rootID)
	vlog.Infof("Make sure we can read them.")
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuff/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/x/y/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/alpha/beta/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", true, aliceID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", false, bobID)

	// Test generic unmount.
	vlog.Info("Test generic unmount.")
	doUnmount(t, naming.JoinAddressName(mtAddr, "//mounttable/a/b"), "", true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", false, rootID)

	// Test specific unmount.
	vlog.Info("Test specific unmount.")
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable/a/b"), collectionName, true, rootID)
	doUnmount(t, naming.JoinAddressName(mtAddr, "//mounttable/a/b"), collectionName, true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/a/b/falls"), "falls mainly on the plain", false, rootID)

	// Try timing out a mount.
	vlog.Info("Try timing out a mount.")
	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable/stuffWithTTL"), collectionName, true, rootID)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuffWithTTL/the/rain"), "the rain", true, rootID)
	ft.advance(time.Duration(ttlSecs+4) * time.Second)
	checkContents(t, naming.JoinAddressName(mtAddr, "mounttable/stuffWithTTL/the/rain"), "the rain", false, rootID)

	// Test unauthorized mount.
	vlog.Info("Test unauthorized mount.")
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable//a/b"), collectionName, false, bobID)
	doMount(t, naming.JoinAddressName(mtAddr, "//mounttable//a/b"), collectionName, false, aliceID)

	doUnmount(t, naming.JoinAddressName(mtAddr, "//mounttable/x/y"), collectionName, false, bobID)
}

func doGlob(t *testing.T, name, pattern string, id ipc.ClientOpt) []string {
	mtpt, err := mounttable.BindMountTable(name, quuxClient(id))
	if err != nil {
		boom(t, "Failed to BindMountTable: %s", err)
	}
	stream, err := mtpt.Glob(rt.R().NewContext(), pattern)
	if err != nil {
		boom(t, "Failed call to %s.Glob(%s): %s", name, pattern, err)
	}
	var reply []string
	rStream := stream.RecvStream()
	for rStream.Advance() {
		e := rStream.Value()
		reply = append(reply, e.Name)
	}

	if err := rStream.Err(); err != nil {
		boom(t, "Glob %s: %s", name, err)
	}
	return reply
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
	fakeServer := naming.JoinAddressName(estr, "//quux")
	doMount(t, naming.JoinAddressName(estr, "//one/bright/day"), fakeServer, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//in/the/middle"), fakeServer, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//of/the/night"), fakeServer, true, rootID)

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
		out := doGlob(t, naming.JoinAddressName(estr, "//"), test.in, rootID)
		checkMatch(t, test.expected, out)
	}

	// Test Glob on a name that is under a mounted server. The result should the
	// the address the mounted server with the extra suffix.
	{
		name := naming.JoinAddressName(estr, "//of/the/night/two/dead/boys/got/up/to/fight")
		pattern := "*"
		m, err := mounttable.BindGlobbable(name, quuxClient(rootID))
		if err != nil {
			boom(t, "Failed to BindMountTable: %s", err)
		}
		stream, err := m.Glob(rt.R().NewContext(), pattern)
		if err != nil {
			boom(t, "Failed call to %s.Glob(%s): %s", name, pattern, err)
		}
		var results []types.MountEntry
		iterator := stream.RecvStream()
		for iterator.Advance() {
			results = append(results, iterator.Value())
		}
		if err := iterator.Err(); err != nil {
			boom(t, "Glob %s: %s", name, err)
		}
		if len(results) != 1 {
			boom(t, "Unexpected number of results. Got %v, want 1", len(results))
		}
		if results[0].Name != "" {
			boom(t, "Unexpected name. Got %v, want ''", results[0].Name)
		}
		_, suffix := naming.SplitAddressName(results[0].Servers[0].Server)
		if expected := "//quux/two/dead/boys/got/up/to/fight"; suffix != expected {
			boom(t, "Unexpected suffix. Got %v, want %v", suffix, expected)
		}
	}
}

func TestGlobACLs(t *testing.T) {
	server, estr := newMT(t, "testdata/test.acl")
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, naming.JoinAddressName(estr, "//one/bright/day"), fakeServer, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//a/b/c"), fakeServer, true, rootID)

	// Try various globs.
	tests := []struct {
		id       ipc.ClientOpt
		in       string
		expected []string
	}{
		{rootID, "*", []string{"one", "a"}},
		{aliceID, "*", []string{"one", "a"}},
		{bobID, "*", []string{"one"}},
		{rootID, "*/...", []string{"one", "a", "one/bright", "a/b", "one/bright/day", "a/b/c"}},
		{aliceID, "*/...", []string{"one", "a", "one/bright", "a/b", "one/bright/day", "a/b/c"}},
		{bobID, "*/...", []string{"one", "one/bright", "one/bright/day"}},
	}
	for _, test := range tests {
		out := doGlob(t, naming.JoinAddressName(estr, "//"), test.in, test.id)
		checkMatch(t, test.expected, out)
	}
}

func TestServerFormat(t *testing.T) {
	server, estr := newMT(t, "")
	defer server.Stop()

	doMount(t, naming.JoinAddressName(estr, "//mounttable/endpoint"), naming.JoinAddressName(estr, "life/on/the/mississippi"), true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/hostport"), "/atrampabroad:8000", true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/hostport-endpoint-platypus"), "/@atrampabroad:8000@@", true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/invalid/not/rooted"), "atrampabroad:8000", false, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/invalid/no/port"), "/atrampabroad", false, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/invalid/endpoint"), "/@following the equator:8000@@@", false, rootID)
}

func TestExpiry(t *testing.T) {
	server, estr := newMT(t, "")
	defer server.Stop()
	collection, collectionAddr := newCollection(t, "testdata/test.acl")
	defer collection.Stop()

	collectionName := naming.JoinAddressName(collectionAddr, "collection")

	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a1/b1"), collectionName, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a1/b2"), collectionName, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a2/b1"), collectionName, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a2/b2/c"), collectionName, true, rootID)

	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable"), "*/b1/...", rootID))
	ft.advance(time.Duration(ttlSecs/2) * time.Second)
	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable"), "*/b1/...", rootID))
	checkMatch(t, []string{"c"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable/a2/b2"), "*", rootID))
	// Refresh only a1/b1.  All the other mounts will expire upon the next
	// ft advance.
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a1/b1"), collectionName, true, rootID)
	ft.advance(time.Duration(ttlSecs/2+4) * time.Second)
	checkMatch(t, []string{"a1"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable"), "*", rootID))
	checkMatch(t, []string{"a1/b1"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable"), "*/b1/...", rootID))
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
