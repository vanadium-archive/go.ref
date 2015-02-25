package namespace_test

import (
	"fmt"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/security/access"

	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	service "v.io/core/veyron/services/mounttable/lib"
)

func init() {
	testutil.Init()
}

func initTest() (rootCtx *context.T, aliceCtx *context.T, bobCtx *context.T, shutdown v23.Shutdown) {
	ctx, shutdown := testutil.InitForTest()
	var err error
	if rootCtx, err = v23.SetPrincipal(ctx, tsecurity.NewPrincipal("root")); err != nil {
		panic("failed to set root principal")
	}
	if aliceCtx, err = v23.SetPrincipal(ctx, tsecurity.NewPrincipal("alice")); err != nil {
		panic("failed to set alice principal")
	}
	if bobCtx, err = v23.SetPrincipal(ctx, tsecurity.NewPrincipal("bob")); err != nil {
		panic("failed to set bob principal")
	}
	for _, r := range []*context.T{rootCtx, aliceCtx, bobCtx} {
		// A hack to set the namespace roots to a value that won't work.
		v23.GetNamespace(r).SetRoots()
		// And have all principals recognize each others blessings.
		p1 := v23.GetPrincipal(r)
		for _, other := range []*context.T{rootCtx, aliceCtx, bobCtx} {
			// tsecurity.NewPrincipal has already setup each
			// principal to use the same blessing for both server
			// and client activities.
			if err := p1.AddToRoots(v23.GetPrincipal(other).BlessingStore().Default()); err != nil {
				panic(err)
			}
		}
	}
	return rootCtx, aliceCtx, bobCtx, shutdown
}

// Create a new mounttable service.
func newMT(t *testing.T, ctx *context.T) (func(), string) {
	estr, stopFunc, err := service.StartServers(ctx, v23.GetListenSpec(ctx), "", "", "")
	if err != nil {
		t.Fatalf("r.NewServer: %s", err)
	}
	return stopFunc, estr
}

func TestACLs(t *testing.T) {
	// Create three different personalities.
	// TODO(p): Use the multiple personalities to test ACL functionality.
	rootCtx, _, _, shutdown := initTest()
	defer shutdown()

	// Create root mounttable.
	stop, rmtAddr := newMT(t, rootCtx)
	fmt.Printf("rmt at %s\n", rmtAddr)
	defer stop()
	ns := v23.GetNamespace(rootCtx)
	ns.SetRoots("/" + rmtAddr)

	// Create two parallel mount tables.
	stop1, mt1Addr := newMT(t, rootCtx)
	fmt.Printf("mt1 at %s\n", mt1Addr)
	defer stop1()
	stop2, mt2Addr := newMT(t, rootCtx)
	fmt.Printf("mt2 at %s\n", mt2Addr)
	defer stop2()

	// Mount them into the root.
	if err := ns.Mount(rootCtx, "a/b/c", mt1Addr, 0, naming.ServesMountTableOpt(true)); err != nil {
		t.Fatalf("Failed to Mount %s onto a/b/c: %s", "/"+mt1Addr, err)
	}
	if err := ns.Mount(rootCtx, "a/b/c", mt2Addr, 0, naming.ServesMountTableOpt(true)); err != nil {
		t.Fatalf("Failed to Mount %s onto a/b/c: %s", "/"+mt2Addr, err)
	}

	// Set/Get the mount point's ACL.
	acl, etag, err := ns.GetACL(rootCtx, "a/b/c")
	if err != nil {
		t.Fatalf("GetACL a/b/c: %s", err)
	}
	acl = access.TaggedACLMap{"Read": access.ACL{In: []security.BlessingPattern{security.AllPrincipals}}}
	if err := ns.SetACL(rootCtx, "a/b/c", acl, etag); err != nil {
		t.Fatalf("SetACL a/b/c: %s", err)
	}
	nacl, _, err := ns.GetACL(rootCtx, "a/b/c")
	if err != nil {
		t.Fatalf("GetACL a/b/c: %s", err)
	}
	if !reflect.DeepEqual(acl, nacl) {
		t.Fatalf("want %v, got %v", acl, nacl)
	}

	// Now Set/Get the parallel mount point's ACL.
	etag = "" // Parallel setacl with any other value is dangerous
	acl = access.TaggedACLMap{"Read": access.ACL{In: []security.BlessingPattern{security.AllPrincipals}},
		"Admin": access.ACL{In: []security.BlessingPattern{security.AllPrincipals}}}
	if err := ns.SetACL(rootCtx, "a/b/c/d/e", acl, etag); err != nil {
		t.Fatalf("SetACL a/b/c/d/e: %s", err)
	}
	nacl, _, err = ns.GetACL(rootCtx, "a/b/c/d/e")
	if err != nil {
		t.Fatalf("GetACL a/b/c/d/e: %s", err)
	}
	if !reflect.DeepEqual(acl, nacl) {
		t.Fatalf("want %v, got %v", acl, nacl)
	}

	// Get from each server individually to make sure both are set.
	nacl, _, err = ns.GetACL(rootCtx, naming.Join(mt1Addr, "d/e"))
	if err != nil {
		t.Fatalf("GetACL a/b/c/d/e: %s", err)
	}
	if !reflect.DeepEqual(acl, nacl) {
		t.Fatalf("want %v, got %v", acl, nacl)
	}
	nacl, _, err = ns.GetACL(rootCtx, naming.Join(mt2Addr, "d/e"))
	if err != nil {
		t.Fatalf("GetACL a/b/c/d/e: %s", err)
	}
	if !reflect.DeepEqual(acl, nacl) {
		t.Fatalf("want %v, got %v", acl, nacl)
	}
}
