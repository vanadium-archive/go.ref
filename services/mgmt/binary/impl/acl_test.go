package impl_test

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"syscall"
	"testing"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/repository"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/v23/vlog"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/services/mgmt/binary/impl"
	mgmttest "v.io/core/veyron/services/mgmt/lib/testutil"
)

//go:generate v23 test generate

const (
	binaryCmd = "binaryd"
)

func binaryd(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) < 2 {
		vlog.Fatalf("binaryd expected at least name and store arguments and optionally ACL flags per TaggedACLMapFromFlag")
	}
	publishName := args[0]
	storedir := args[1]

	ctx, shutdown := testutil.InitForTest()

	defer fmt.Fprintf(stdout, "%v terminating\n", publishName)
	defer vlog.VI(1).Infof("%v terminating", publishName)
	defer shutdown()

	server, endpoint := mgmttest.NewServer(ctx)
	name := naming.JoinAddressName(endpoint, "")
	vlog.VI(1).Infof("binaryd name: %v", name)

	depth := 2
	state, err := impl.NewState(storedir, "", depth)
	if err != nil {
		vlog.Fatalf("NewState(%v, %v, %v) failed: %v", storedir, "", depth, err)
	}
	dispatcher, err := impl.NewDispatcher(v23.GetPrincipal(ctx), state)
	if err != nil {
		vlog.Fatalf("Failed to create binaryd dispatcher: %v", err)
	}
	if err := server.ServeDispatcher(publishName, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}

	fmt.Fprintf(stdout, "ready:%d\n", os.Getpid())
	<-signals.ShutdownOnSignals(ctx)

	return nil
}

func TestBinaryCreateACL(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	selfPrincipal := tsecurity.NewPrincipal("self")
	selfCtx, err := v23.SetPrincipal(ctx, selfPrincipal)
	if err != nil {
		t.Fatalf("SetPrincipal failed: %v", err)
	}
	dir, childPrincipal := tsecurity.ForkCredentials(selfPrincipal, "child")
	defer os.RemoveAll(dir)
	childCtx, err := v23.SetPrincipal(ctx, childPrincipal)
	if err != nil {
		t.Fatalf("SetPrincipal failed: %v", err)
	}

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, childCtx, v23.GetPrincipal(childCtx))
	defer deferFn()
	// make selfCtx and childCtx have the same Namespace Roots as set by
	// CreateShellAndMountTable
	v23.GetNamespace(selfCtx).SetRoots(v23.GetNamespace(childCtx).Roots()...)

	// setup mock up directory to put state in
	storedir, cleanup := mgmttest.SetupRootDir(t, "bindir")
	defer cleanup()
	prepDirectory(t, storedir)

	_, nms := mgmttest.RunShellCommand(t, sh, nil, binaryCmd, "bini", storedir)
	pid := mgmttest.ReadPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	vlog.VI(2).Infof("Self uploads a shared and private binary.")
	binary := repository.BinaryClient("bini/private")
	if err := binary.Create(childCtx, 1, repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
		t.Fatalf("Create() failed %v", err)
	}
	fakeDataPrivate := testData()
	if streamErr, err := invokeUpload(t, childCtx, binary, fakeDataPrivate, 0); streamErr != nil || err != nil {
		t.Fatalf("invokeUpload() failed %v, %v", err, streamErr)
	}

	vlog.VI(2).Infof("Validate that the ACL also allows Self")
	acl, _, err := binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL failed: %v", err)
	}
	expected := access.TaggedACLMap{
		"Admin":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}},
		"Read":    access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}},
		"Write":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}},
		"Debug":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}},
	}
	if got, want := acl.Normalize(), expected.Normalize(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, expected %#v ", got, want)
	}
}

func TestBinaryRootACL(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	selfPrincipal := tsecurity.NewPrincipal("self")
	selfCtx, err := v23.SetPrincipal(ctx, selfPrincipal)
	if err != nil {
		t.Fatalf("SetPrincipal failed: %v", err)
	}
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, selfCtx, v23.GetPrincipal(selfCtx))
	defer deferFn()

	// setup mock up directory to put state in
	storedir, cleanup := mgmttest.SetupRootDir(t, "bindir")
	defer cleanup()
	prepDirectory(t, storedir)

	otherPrincipal := tsecurity.NewPrincipal("other")
	if err := otherPrincipal.AddToRoots(selfPrincipal.BlessingStore().Default()); err != nil {
		t.Fatalf("otherPrincipal.AddToRoots() failed: %v", err)
	}
	otherCtx, err := v23.SetPrincipal(selfCtx, otherPrincipal)
	if err != nil {
		t.Fatalf("SetPrincipal() failed: %v", err)
	}

	_, nms := mgmttest.RunShellCommand(t, sh, nil, binaryCmd, "bini", storedir)
	pid := mgmttest.ReadPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	vlog.VI(2).Infof("Self uploads a shared and private binary.")
	binary := repository.BinaryClient("bini/private")
	if err := binary.Create(selfCtx, 1, repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
		t.Fatalf("Create() failed %v", err)
	}
	fakeDataPrivate := testData()
	if streamErr, err := invokeUpload(t, selfCtx, binary, fakeDataPrivate, 0); streamErr != nil || err != nil {
		t.Fatalf("invokeUpload() failed %v, %v", err, streamErr)
	}

	binary = repository.BinaryClient("bini/shared")
	if err := binary.Create(selfCtx, 1, repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
		t.Fatalf("Create() failed %v", err)
	}
	fakeDataShared := testData()
	if streamErr, err := invokeUpload(t, selfCtx, binary, fakeDataShared, 0); streamErr != nil || err != nil {
		t.Fatalf("invokeUpload() failed %v, %v", err, streamErr)
	}

	vlog.VI(2).Infof("Verify that in the beginning other can't access bini/private or bini/shared")
	binary = repository.BinaryClient("bini/private")
	if _, _, err := binary.Stat(otherCtx); !verror.Is(err, verror.ErrNoAccess.ID) {
		t.Fatalf("Stat() should have failed but didn't: %v", err)
	}
	binary = repository.BinaryClient("bini/shared")
	if _, _, err := binary.Stat(otherCtx); !verror.Is(err, verror.ErrNoAccess.ID) {
		t.Fatalf("Stat() should have failed but didn't: %v", err)
	}

	vlog.VI(2).Infof("Validate the ACL file on bini/private.")
	binary = repository.BinaryClient("bini/private")
	acl, _, err := binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL failed: %v", err)
	}
	expected := access.TaggedACLMap{
		"Admin":   access.ACL{In: []security.BlessingPattern{"self"}},
		"Read":    access.ACL{In: []security.BlessingPattern{"self"}},
		"Write":   access.ACL{In: []security.BlessingPattern{"self"}},
		"Debug":   access.ACL{In: []security.BlessingPattern{"self"}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"self"}},
	}
	if got, want := acl.Normalize(), expected.Normalize(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, expected %#v ", got, want)
	}

	vlog.VI(2).Infof("Validate the ACL file on bini/private.")
	binary = repository.BinaryClient("bini/private")
	acl, etag, err := binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL failed: %v", err)
	}
	if got, want := acl.Normalize(), expected.Normalize(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, expected %#v ", got, want)
	}

	vlog.VI(2).Infof("self blesses other as self/other and locks the bini/private binary to itself.")
	selfBlessing := selfPrincipal.BlessingStore().Default()
	otherBlessing, err := selfPrincipal.Bless(otherPrincipal.PublicKey(), selfBlessing, "other", security.UnconstrainedUse())
	if err != nil {
		t.Fatalf("selfPrincipal.Bless() failed: %v", err)
	}
	if _, err := otherPrincipal.BlessingStore().Set(otherBlessing, security.AllPrincipals); err != nil {
		t.Fatalf("otherPrincipal.BlessingStore() failed: %v", err)
	}

	vlog.VI(2).Infof("Self modifies the ACL file on bini/private.")
	for _, tag := range access.AllTypicalTags() {
		acl.Clear("self", string(tag))
		acl.Add("self/$", string(tag))
	}
	if err := binary.SetACL(selfCtx, acl, etag); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}

	vlog.VI(2).Infof(" Verify that bini/private's acls are updated.")
	binary = repository.BinaryClient("bini/private")
	updated := access.TaggedACLMap{
		"Admin":   access.ACL{In: []security.BlessingPattern{"self/$"}},
		"Read":    access.ACL{In: []security.BlessingPattern{"self/$"}},
		"Write":   access.ACL{In: []security.BlessingPattern{"self/$"}},
		"Debug":   access.ACL{In: []security.BlessingPattern{"self/$"}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"self/$"}},
	}
	acl, _, err = binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL failed: %v", err)
	}
	if got, want := acl.Normalize(), updated.Normalize(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, expected %#v ", got, want)
	}

	// Other still can't access bini/shared because there's no ACL file at the
	// root level. Self has to set one explicitly to enable sharing. This way, self
	// can't accidentally expose the server without setting a root ACL.
	vlog.VI(2).Infof(" Verify that other still can't access bini/shared.")
	binary = repository.BinaryClient("bini/shared")
	if _, _, err := binary.Stat(otherCtx); !verror.Is(err, verror.ErrNoAccess.ID) {
		t.Fatalf("Stat() should have failed but didn't: %v", err)
	}

	vlog.VI(2).Infof("Self sets a root ACL.")
	binary = repository.BinaryClient("bini")
	newRootACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		newRootACL.Add("self/$", string(tag))
	}
	if err := binary.SetACL(selfCtx, newRootACL, ""); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}

	vlog.VI(2).Infof("Verify that other can access bini/shared now but not access bini/private.")
	binary = repository.BinaryClient("bini/shared")
	if _, _, err := binary.Stat(otherCtx); err != nil {
		t.Fatalf("Stat() shouldn't have failed: %v", err)
	}
	binary = repository.BinaryClient("bini/private")
	if _, _, err := binary.Stat(otherCtx); !verror.Is(err, verror.ErrNoAccess.ID) {
		t.Fatalf("Stat() should have failed but didn't: %v", err)
	}

	vlog.VI(2).Infof("Other still can't create so Self gives Other right to Create.")
	binary = repository.BinaryClient("bini")
	acl, tag, err := binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL() failed: %v", err)
	}
	acl.Add("self", string("Write"))
	err = binary.SetACL(selfCtx, acl, tag)
	if err != nil {
		t.Fatalf("SetACL() failed: %v", err)
	}

	vlog.VI(2).Infof("Other creates bini/otherbinary")
	binary = repository.BinaryClient("bini/otherbinary")
	if err := binary.Create(otherCtx, 1, repository.MediaInfo{Type: "application/octet-stream"}); err != nil {
		t.Fatalf("Create() failed %v", err)
	}
	fakeDataOther := testData()
	if streamErr, err := invokeUpload(t, otherCtx, binary, fakeDataOther, 0); streamErr != nil || err != nil {
		t.FailNow()
	}

	vlog.VI(2).Infof("Other can read acls for bini/otherbinary.")
	updated = access.TaggedACLMap{
		"Admin":   access.ACL{In: []security.BlessingPattern{"self/$", "self/other"}},
		"Read":    access.ACL{In: []security.BlessingPattern{"self/$", "self/other"}},
		"Write":   access.ACL{In: []security.BlessingPattern{"self/$", "self/other"}},
		"Debug":   access.ACL{In: []security.BlessingPattern{"self/$", "self/other"}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"self/$", "self/other"}},
	}
	acl, _, err = binary.GetACL(otherCtx)
	if err != nil {
		t.Fatalf("GetACL failed: %v", err)
	}
	if got, want := acl.Normalize(), updated.Normalize(); !reflect.DeepEqual(want, got) {
		t.Errorf("got %#v, expected %#v ", got, want)
	}

	vlog.VI(2).Infof("Other tries to exclude self by adding self to Read's notin")
	acl, tag, err = binary.GetACL(otherCtx)
	if err != nil {
		t.Fatalf("GetACL() failed: %v", err)
	}
	acl.Blacklist("self", string("Read"))
	err = binary.SetACL(otherCtx, acl, tag)
	if err != nil {
		t.Fatalf("SetACL() failed: %v", err)
	}

	vlog.VI(2).Infof("But self's rights are inherited from root so self can still access despite blacklist.")
	binary = repository.BinaryClient("bini/otherbinary")
	if _, _, err := binary.Stat(selfCtx); err != nil {
		t.Fatalf("Stat() shouldn't have failed: %v", err)
	}

	vlog.VI(2).Infof("Self petulantly blacklists other back.")
	binary = repository.BinaryClient("bini")
	acl, tag, err = binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL() failed: %v", err)
	}
	for _, tag := range access.AllTypicalTags() {
		acl.Blacklist("self/other", string(tag))
	}
	err = binary.SetACL(selfCtx, acl, tag)
	if err != nil {
		t.Fatalf("SetACL() failed: %v", err)
	}

	vlog.VI(2).Infof("And now other can do nothing at affecting the root. Other should be penitent.")
	binary = repository.BinaryClient("bini/nototherbinary")
	if err := binary.Create(otherCtx, 1, repository.MediaInfo{Type: "application/octet-stream"}); !verror.Is(err, verror.ErrNoAccess.ID) {
		t.Fatalf("Create() should have failed %v", err)
	}

	vlog.VI(2).Infof("But other can still access shared.")
	binary = repository.BinaryClient("bini/shared")
	if _, _, err := binary.Stat(otherCtx); err != nil {
		t.Fatalf("Stat() should not have failed but did: %v", err)
	}

	vlog.VI(2).Infof("Self petulantly blacklists other's binary too.")
	binary = repository.BinaryClient("bini/shared")
	acl, tag, err = binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL() failed: %v", err)
	}
	for _, tag := range access.AllTypicalTags() {
		acl.Blacklist("self/other", string(tag))
	}
	err = binary.SetACL(selfCtx, acl, tag)
	if err != nil {
		t.Fatalf("SetACL() failed: %v", err)
	}

	vlog.VI(2).Infof("And now other can't access shared either.")
	binary = repository.BinaryClient("bini/shared")
	if _, _, err := binary.Stat(otherCtx); !verror.Is(err, verror.ErrNoAccess.ID) {
		t.Fatalf("Stat() should have failed but didn't: %v", err)
	}
}

func TestBinaryRationalStartingValueForGetACL(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	v23.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	selfPrincipal := tsecurity.NewPrincipal("self")
	selfCtx, err := v23.SetPrincipal(ctx, selfPrincipal)
	if err != nil {
		t.Fatalf("SetPrincipal failed: %v", err)
	}
	sh, deferFn := mgmttest.CreateShellAndMountTable(t, selfCtx, v23.GetPrincipal(selfCtx))
	defer deferFn()

	// setup mock up directory to put state in
	storedir, cleanup := mgmttest.SetupRootDir(t, "bindir")
	defer cleanup()
	prepDirectory(t, storedir)

	otherPrincipal := tsecurity.NewPrincipal("other")
	if err := otherPrincipal.AddToRoots(selfPrincipal.BlessingStore().Default()); err != nil {
		t.Fatalf("otherPrincipal.AddToRoots() failed: %v", err)
	}

	_, nms := mgmttest.RunShellCommand(t, sh, nil, binaryCmd, "bini", storedir)
	pid := mgmttest.ReadPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	binary := repository.BinaryClient("bini")
	acl, tag, err := binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL failed: %#v", err)
	}
	expected := access.TaggedACLMap{
		"Admin":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
		"Read":    access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
		"Write":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
		"Debug":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
	}
	if got, want := acl.Normalize(), expected.Normalize(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, expected %#v ", got, want)
	}

	acl.Blacklist("self", string("Read"))
	err = binary.SetACL(selfCtx, acl, tag)
	if err != nil {
		t.Fatalf("SetACL() failed: %v", err)
	}

	acl, tag, err = binary.GetACL(selfCtx)
	if err != nil {
		t.Fatalf("GetACL failed: %#v", err)
	}
	expected = access.TaggedACLMap{
		"Admin":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
		"Read":    access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{"self"}},
		"Write":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
		"Debug":   access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"self/$", "self/child"}, NotIn: []string{}},
	}
	if got, want := acl.Normalize(), expected.Normalize(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, expected %#v ", got, want)
	}

}
