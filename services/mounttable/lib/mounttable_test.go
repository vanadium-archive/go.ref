package mounttable

import (
	"errors"
	"io"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"

	_ "veyron/lib/testutil"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mounttable"
	"veyron2/vlog"
)

// stupidMT is a version of naming.MountTable that we can control.  This exists so that we have some
// firm ground to stand on vis a vis the stub interface.
type stupidMT struct {
	id ipc.ClientOpt
}

var (
	rootID = veyron2.LocalID(security.FakePrivateID("root"))
	bobID = veyron2.LocalID(security.FakePrivateID("bob"))
	aliceID = veyron2.LocalID(security.FakePrivateID("alice"))
)

const ttlSecs = 60 * 60

func boom(t *testing.T, f string, v ...interface{}) {
	t.Logf(f, v...)
	t.Fatal(string(debug.Stack()))
}

// quuxClient returns an ipc.Client that uses the quux mounttable for name
// resolution.
func quuxClient(id ipc.ClientOpt) ipc.Client {
	mt := stupidMT{id}
	c, err := rt.R().NewClient(id, veyron2.MountTable(mt))
	if err != nil {
		panic(err)
	}
	return c
}

func (stupidMT) Mount(string, string, time.Duration) error {
	return errors.New("unimplemented")
}

func (stupidMT) Unmount(string, string) error {
	return errors.New("unimplemented")
}

// Resolve will only go one level deep, i.e., it doesn't walk past the first mount point.
func (mt stupidMT) Resolve(name string) ([]string, error) {
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
	objectPtr, err := mounttable.BindMountTable("/"+address+"//"+suffix, quuxClient(mt.id))
	if err != nil {
		return nil, err
	}
	ss, suffix, err := objectPtr.ResolveStep()
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

func (s stupidMT) Unresolve(name string) ([]string, error) {
	return s.Resolve(name)
}

func (stupidMT) ResolveToMountTable(name string) ([]string, error) {
	return nil, errors.New("ResolveToMountTable is not implemented in this MountTable")
}

// Glob implements naming.MountTable.Glob.
func (stupidMT) Glob(pattern string) (chan naming.MountEntry, error) {
	return nil, errors.New("Glob is not implemented in this MountTable")
}

func doMount(t *testing.T, name, service string, shouldSucceed bool, id ipc.ClientOpt) {
	mtpt, err := mounttable.BindMountTable(name, quuxClient(id))
	if err != nil {
		boom(t, "Failed to BindMountTable: %s", err)
	}
	if err := mtpt.Mount(service, uint32(ttlSecs)); err != nil {
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
	if err := mtpt.Unmount(service); err != nil {
		if (shouldSucceed) {
			boom(t, "Failed to Unmount %s onto %s: %s", service, name, err)
		}
	} else if (!shouldSucceed) {
		boom(t, "doUnmount %s onto %s, expected failure but succeeded", service, name)
	}
}

func create(t *testing.T, name, contents string) {
	objectPtr, err := BindCollection(name, quuxClient(rootID))
	if err != nil {
		boom(t, "Failed to BindCollection: %s", err)
	}
	if err := objectPtr.Export(contents, true); err != nil {
		boom(t, "Failed to Export %s to %s: %s", name, contents, err)
	}
}

func checkContents(t *testing.T, name, expected string, shouldSucceed bool, id ipc.ClientOpt) {
	objectPtr, err := BindCollection(name, quuxClient(id))
	if err != nil {
		boom(t, "Failed to BindCollection: %s", err)
	}
	contents, err := objectPtr.Lookup()
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

func newServer(acl string, t *testing.T) (ipc.Server, string) {
	r := rt.Init()
	server, err := r.NewServer()
	if err != nil {
		boom(t, "r.NewServer: %s", err)
	}

	// Add mount table service.
	mt, err := NewMountTable(acl)
	if err != nil {
		boom(t, "NewMountTable: %v", err)
	}
	if err := server.Register("mounttable", mt); err != nil {
		boom(t, "Failed to register mount table: %s", err)
	}

	// Start serving on a loopback address.
	e, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		boom(t, "Failed to Listen mount table: %s", err)
	}
	estr := e.String()
	t.Logf("endpoint %s", estr)
	return server, estr
}

func TestMountTable(t *testing.T) {
	server, estr := newServer("testdata/test.acl", t)
	defer server.Stop()
	// Add a collection service.  This is just a service we can mount
	// and test against.
	cPrefix := "collection"
	if err := server.Register(cPrefix, newCollectionServer()); err != nil {
		boom(t, "Failed to register mock collection: %s", err)
	}

	// Mount the collection server into the mount table.
	doMount(t, naming.JoinAddressName(estr, "//mounttable/stuff"), naming.JoinAddressName(estr, "collection"), true, rootID)

	// Create a few objects and make sure we can read them.
	create(t, naming.JoinAddressName(estr, "mounttable/stuff/the/rain"), "the rain")
	create(t, naming.JoinAddressName(estr, "mounttable/stuff/in/spain"), "in spain")
	create(t, naming.JoinAddressName(estr, "mounttable/stuff/falls"), "falls mainly on the plain")
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuff/the/rain"), "the rain", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuff/in/spain"), "in spain", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuff/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable//stuff/falls"), "falls mainly on the plain", false, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuff/nonexistant"), "falls mainly on the plain", false, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuff/the/rain"), "the rain", true, bobID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuff/the/rain"), "the rain", false, aliceID)

	// Test multiple mounts.
	doMount(t, naming.JoinAddressName(estr, "//mounttable//a/b"), naming.JoinAddressName(estr, "collection"), true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/x/y"), naming.JoinAddressName(estr, "collection"), true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/alpha//beta"), naming.JoinAddressName(estr, "collection"), true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuff/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/a/b/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/x/y/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/alpha/beta/falls"), "falls mainly on the plain", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/a/b/falls"), "falls mainly on the plain", true, aliceID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/a/b/falls"), "falls mainly on the plain", false, bobID)

	// Test generic unmount.
	doUnmount(t, naming.JoinAddressName(estr, "//mounttable/a/b"), "", true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/a/b/falls"), "falls mainly on the plain", false, rootID)

	// Test specific unmount.
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a/b"), naming.JoinAddressName(estr, "collection"), true, rootID)
	doUnmount(t, naming.JoinAddressName(estr, "//mounttable/a/b"), naming.JoinAddressName(estr, "collection"), true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/a/b/falls"), "falls mainly on the plain", false, rootID)

	// Try timing out a mount.
	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/stuffWithTTL"), naming.JoinAddressName(estr, "collection"), true, rootID)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuffWithTTL/the/rain"), "the rain", true, rootID)
	ft.advance(time.Duration(ttlSecs+4) * time.Second)
	checkContents(t, naming.JoinAddressName(estr, "mounttable/stuffWithTTL/the/rain"), "the rain", false, rootID)

	// test unauthorized mount
	doMount(t, naming.JoinAddressName(estr, "//mounttable//a/b"), naming.JoinAddressName(estr, "collection"), false, bobID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable//a/b"), naming.JoinAddressName(estr, "collection"), false, aliceID)

	doUnmount(t, naming.JoinAddressName(estr, "//mounttable/x/y"), naming.JoinAddressName(estr, "collection"), false, bobID)
}

func doGlob(t *testing.T, name, pattern string, id ipc.ClientOpt) []string {
	mtpt, err := mounttable.BindMountTable(name, quuxClient(id))
	if err != nil {
		boom(t, "Failed to BindMountTable: %s", err)
	}
	stream, err := mtpt.Glob(pattern)
	if err != nil {
		boom(t, "Failed call to %s.Glob(%s): %s", name, pattern, err)
	}
	var reply []string
	for {
		e, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			boom(t, "Glob %s: %s", name, err)
		}
		reply = append(reply, e.Name)
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
	server, estr := newServer("", t)
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, naming.JoinAddressName(estr, "//mounttable/one/bright/day"), fakeServer, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/in/the/middle"), fakeServer, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/of/the/night"), fakeServer, true, rootID)

	// Try various globs.
	tests := []struct {
		in       string
		expected []string
	}{
		{"*", []string{"one", "in", "of"}},
		{"*/...", []string{"one", "in", "of", "one/bright", "in/the", "of/the", "one/bright/day", "in/the/middle", "of/the/night"}},
		{"*/the", []string{"in/the", "of/the"}},
		{"*/the/...", []string{"in/the", "of/the", "in/the/middle", "of/the/night"}},
		{"o*", []string{"one", "of"}},
		{"", []string{""}},
	}
	for _, test := range tests {
		out := doGlob(t, naming.JoinAddressName(estr, "//mounttable"), test.in, rootID)
		checkMatch(t, test.expected, out)
	}
}

func TestGlobACLs(t *testing.T) {
	server, estr := newServer("testdata/test.acl", t)
	defer server.Stop()

	// set up a mount space
	fakeServer := naming.JoinAddressName(estr, "quux")
	doMount(t, naming.JoinAddressName(estr, "//mounttable/one/bright/day"), fakeServer, true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a/b/c"), fakeServer, true, rootID)

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
		out := doGlob(t, naming.JoinAddressName(estr, "//mounttable"), test.in, test.id)
		checkMatch(t, test.expected, out)
	}
}

func TestServerFormat(t *testing.T) {
	server, estr := newServer("", t)
	defer server.Stop()

	doMount(t, naming.JoinAddressName(estr, "//mounttable/endpoint"), naming.JoinAddressName(estr, "life/on/the/mississippi"), true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/hostport"), "/atrampabroad:8000", true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/hostport-endpoint-platypus"), "/@atrampabroad:8000@@", true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/invalid/not/rooted"), "atrampabroad:8000", false, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/invalid/no/port"), "/atrampabroad", false, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/invalid/endpoint"), "/@following the equator:8000@@@", false, rootID)
}

func TestExpiry(t *testing.T) {
	server, estr := newServer("", t)
	defer server.Stop()

	ft := NewFakeTimeClock()
	setServerListClock(ft)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a1/b1"), naming.JoinAddressName(estr, "collection"), true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a1/b2"), naming.JoinAddressName(estr, "collection"), true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a2/b1"), naming.JoinAddressName(estr, "collection"), true, rootID)
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a2/b2/c"), naming.JoinAddressName(estr, "collection"), true, rootID)

	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable"), "*/b1/...", rootID))
	ft.advance(time.Duration(ttlSecs/2) * time.Second)
	checkMatch(t, []string{"a1/b1", "a2/b1"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable"), "*/b1/...", rootID))
	checkMatch(t, []string{"c"}, doGlob(t, naming.JoinAddressName(estr, "//mounttable/a2/b2"), "*", rootID))
	// Refresh only a1/b1.  All the other mounts will expire upon the next
	// ft advance.
	doMount(t, naming.JoinAddressName(estr, "//mounttable/a1/b1"), naming.JoinAddressName(estr, "collection"), true, rootID)
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
