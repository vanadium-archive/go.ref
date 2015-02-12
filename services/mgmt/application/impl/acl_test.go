package impl_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"reflect"
	"syscall"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/services/mgmt/application/impl"
	mgmttest "v.io/core/veyron/services/mgmt/lib/testutil"
	"v.io/core/veyron/services/mgmt/repository"
)

const (
	repoCmd = "repository"
)

func init() {
	modules.RegisterChild(repoCmd, "", appRepository)
	testutil.Init()
}

// TestHelperProcess is the entrypoint for the modules commands in a
// a test subprocess.
func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func appRepository(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) < 2 {
		vlog.Fatalf("repository expected at least name and store arguments and optionally ACL flags per TaggedACLMapFromFlag")
	}
	publishName := args[0]
	storedir := args[1]

	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	defer fmt.Fprintf(stdout, "%v terminating\n", publishName)
	defer vlog.VI(1).Infof("%v terminating", publishName)
	server, endpoint := mgmttest.NewServer(ctx)
	defer server.Stop()

	name := naming.JoinAddressName(endpoint, "")
	vlog.VI(1).Infof("applicationd name: %v", name)

	dispatcher, err := impl.NewDispatcher(storedir)
	if err != nil {
		vlog.Fatalf("Failed to create repository dispatcher: %v", err)
	}
	if err := server.ServeDispatcher(publishName, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}

	fmt.Fprintf(stdout, "ready:%d\n", os.Getpid())
	<-signals.ShutdownOnSignals(ctx)

	return nil
}

func TestApplicationUpdateACL(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// setup mock up directory to put state in
	storedir, cleanup := mgmttest.SetupRootDir(t, "application")
	defer cleanup()

	otherCtx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal())
	if err != nil {
		t.Fatal(err)
	}

	idp := tsecurity.NewIDProvider("root")

	// By default, globalRT and otherRT will have blessings generated based on the
	// username/machine name running this process. Since these blessings will appear
	// in ACLs, give them recognizable names.
	if err := idp.Bless(veyron2.GetPrincipal(ctx), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(veyron2.GetPrincipal(otherCtx), "other"); err != nil {
		t.Fatal(err)
	}

	_, nms := mgmttest.RunShellCommand(t, sh, nil, repoCmd, "repo", storedir)
	pid := mgmttest.ReadPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	v1stub := repository.ApplicationClient("repo/search/v1")
	repostub := repository.ApplicationClient("repo")

	// Create example envelopes.
	envelopeV1 := application.Envelope{
		Args:   []string{"--help"},
		Env:    []string{"DEBUG=1"},
		Binary: application.SignedFile{File: "/veyron/name/of/binary"},
	}

	// Envelope putting as other should fail.
	// TODO(rjkroege): Validate that it is failed with permission denied.
	if err := v1stub.Put(otherCtx, []string{"base"}, envelopeV1); err == nil {
		t.Fatalf("Put() wrongly didn't fail")
	}

	// Envelope putting as global should succeed.
	if err := v1stub.Put(ctx, []string{"base"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	acl, etag, err := repostub.GetACL(ctx)
	if !verror.Is(err, impl.ErrNotFound.ID) {
		t.Fatalf("GetACL should have failed with ErrNotFound but was: %v", err)
	}
	if etag != "default" {
		t.Fatalf("getACL expected:default, got:%v(%v)", etag, acl)
	}
	if acl != nil {
		t.Fatalf("GetACL got %v, expected %v", acl, nil)
	}

	vlog.VI(2).Infof("self attempting to give other permission to update application")
	newACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		newACL.Add("root/self", string(tag))
		newACL.Add("root/other", string(tag))
	}
	if err := repostub.SetACL(ctx, newACL, ""); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}

	acl, etag, err = repostub.GetACL(ctx)
	if err != nil {
		t.Fatalf("GetACL should not have failed: %v", err)
	}
	expected := newACL
	if got := acl; !reflect.DeepEqual(expected.Normalize(), got.Normalize()) {
		t.Errorf("got %#v, exected %#v ", got, expected)
	}

	// Envelope putting as other should now succeed.
	if err := v1stub.Put(otherCtx, []string{"base"}, envelopeV1); err != nil {
		t.Fatalf("Put() wrongly failed: %v", err)
	}

	// Other takes control.
	acl, etag, err = repostub.GetACL(otherCtx)
	if err != nil {
		t.Fatalf("GetACL 2 should not have failed: %v", err)
	}
	acl["Admin"] = access.ACL{
		In:    []security.BlessingPattern{"root/other"},
		NotIn: []string{}}
	if err = repostub.SetACL(otherCtx, acl, etag); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}

	// Self is now locked out but other isn't.
	if _, _, err = repostub.GetACL(ctx); err == nil {
		t.Fatalf("GetACL should not have succeeded")
	}
	acl, _, err = repostub.GetACL(otherCtx)
	if err != nil {
		t.Fatalf("GetACL should not have failed: %v", err)
	}
	expected = access.TaggedACLMap{
		"Admin": access.ACL{
			In:    []security.BlessingPattern{"root/other"},
			NotIn: []string{}},
		"Read": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}},
		"Write": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}},
		"Debug": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}}}

	if got := acl; !reflect.DeepEqual(expected.Normalize(), got.Normalize()) {
		t.Errorf("got %#v, exected %#v ", got, expected)
	}
}

func TestPerAppACL(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// setup mock up directory to put state in
	storedir, cleanup := mgmttest.SetupRootDir(t, "application")
	defer cleanup()

	otherCtx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal())
	if err != nil {
		t.Fatal(err)
	}

	idp := tsecurity.NewIDProvider("root")

	// By default, globalRT and otherRT will have blessings generated based on the
	// username/machine name running this process. Since these blessings will appear
	// in ACLs, give them recognizable names.
	if err := idp.Bless(veyron2.GetPrincipal(ctx), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(veyron2.GetPrincipal(otherCtx), "other"); err != nil {
		t.Fatal(err)
	}

	_, nms := mgmttest.RunShellCommand(t, sh, nil, repoCmd, "repo", storedir)
	pid := mgmttest.ReadPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create example envelope.
	envelopeV1 := application.Envelope{
		Args:   []string{"--help"},
		Env:    []string{"DEBUG=1"},
		Binary: application.SignedFile{File: "/veyron/name/of/binary"},
	}

	// Upload the envelope at two different names.
	v1stub := repository.ApplicationClient("repo/search/v1")
	if err := v1stub.Put(ctx, []string{"base"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	v2stub := repository.ApplicationClient("repo/search/v2")
	if err := v2stub.Put(ctx, []string{"base"}, envelopeV1); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// Self can access ACLs but other can't.
	for _, path := range []string{"repo/search", "repo/search/v1", "repo/search/v2"} {
		stub := repository.ApplicationClient(path)
		acl, etag, err := stub.GetACL(ctx)
		if !verror.Is(err, impl.ErrNotFound.ID) {
			t.Fatalf("GetACL should have failed with ErrNotFound but was: %v", err)
		}
		if etag != "default" {
			t.Fatalf("GetACL expected:default, got:%v(%v)", etag, acl)
		}
		if acl != nil {
			t.Fatalf("GetACL got %v, expected %v", acl, nil)
		}
		if _, _, err := stub.GetACL(otherCtx); err == nil {
			t.Fatalf("GetACL didn't fail for other when it should have.")
		}
	}

	// Self gives other full access only to repo/search/v1.
	newACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		newACL.Add("root/self", string(tag))
		newACL.Add("root/other", string(tag))
	}
	if err := v1stub.SetACL(ctx, newACL, ""); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}

	// Other can now access this location.
	acl, _, err := v1stub.GetACL(otherCtx)
	if err != nil {
		t.Fatalf("GetACL should not have failed: %v", err)
	}
	expected := access.TaggedACLMap{
		"Admin": access.ACL{
			In: []security.BlessingPattern{"root/other",
				"root/self"},
			NotIn: []string{}},
		"Read": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}},
		"Write": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}},
		"Debug": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}},
		"Resolve": access.ACL{In: []security.BlessingPattern{"root/other",
			"root/self"},
			NotIn: []string{}}}
	if got := acl; !reflect.DeepEqual(expected.Normalize(), got.Normalize()) {
		t.Errorf("got %#v, exected %#v ", got, expected)
	}

	// But other locations should be unaffected and other cannot access.
	for _, path := range []string{"repo/search", "repo/search/v2"} {
		stub := repository.ApplicationClient(path)
		if _, _, err := stub.GetACL(otherCtx); err == nil {
			t.Fatalf("GetACL didn't fail for other when it should have.")
		}
	}

	// Self gives other write perms on base.
	repostub := repository.ApplicationClient("repo/")
	newACL = make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		newACL.Add("root/self", string(tag))
	}
	newACL["Write"] = access.ACL{In: []security.BlessingPattern{"root/other", "root/self"}}
	if err := repostub.SetACL(ctx, newACL, ""); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}

	// Other can now upload an envelope at both locations.
	for _, stub := range []repository.ApplicationClientStub{v1stub, v2stub} {
		if err := stub.Put(otherCtx, []string{"base"}, envelopeV1); err != nil {
			t.Fatalf("Put() failed: %v", err)
		}
	}

	// But self didn't give other ACL modification permissions.
	for _, path := range []string{"repo/search", "repo/search/v2"} {
		stub := repository.ApplicationClient(path)
		if _, _, err := stub.GetACL(otherCtx); err == nil {
			t.Fatalf("GetACL didn't fail for other when it should have.")
		}
	}
}

func TestInitialACLSet(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	sh, deferFn := mgmttest.CreateShellAndMountTable(t, ctx, nil)
	defer deferFn()

	// Setup mock up directory to put state in.
	storedir, cleanup := mgmttest.SetupRootDir(t, "application")
	defer cleanup()

	idp := tsecurity.NewIDProvider("root")

	// Make a recognizable principal name.
	if err := idp.Bless(veyron2.GetPrincipal(ctx), "self"); err != nil {
		t.Fatal(err)
	}

	// Make an TAM for use on the command line.
	expected := access.TaggedACLMap{
		"Admin": access.ACL{
			In: []security.BlessingPattern{"root/rubberchicken",
				"root/self"},
			NotIn: []string{},
		},
	}

	b := new(bytes.Buffer)
	if err := expected.WriteTo(b); err != nil {
		t.Fatal(err)
	}

	_, nms := mgmttest.RunShellCommand(t, sh, nil, repoCmd, "--veyron.acl.literal", b.String(), "repo", storedir)
	pid := mgmttest.ReadPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// It should have the correct starting ACLs from the command line.
	stub := repository.ApplicationClient("repo")
	acl, _, err := stub.GetACL(ctx)
	if err != nil {
		t.Fatalf("GetACL should not have failed: %v", err)
	}
	if got := acl; !reflect.DeepEqual(expected.Normalize(), got.Normalize()) {
		t.Errorf("got %#v, exected %#v ", got, expected)
	}
}
