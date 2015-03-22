package namespace_test

import (
	"fmt"
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/security/access"

	_ "v.io/x/ref/profiles"
	service "v.io/x/ref/services/mounttable/lib"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

func init() {
	test.Init()
}

func initTest() (rootCtx *context.T, aliceCtx *context.T, bobCtx *context.T, shutdown v23.Shutdown) {
	ctx, shutdown := test.InitForTest()
	var err error
	if rootCtx, err = v23.SetPrincipal(ctx, testutil.NewPrincipal("root")); err != nil {
		panic("failed to set root principal")
	}
	if aliceCtx, err = v23.SetPrincipal(ctx, testutil.NewPrincipal("alice")); err != nil {
		panic("failed to set alice principal")
	}
	if bobCtx, err = v23.SetPrincipal(ctx, testutil.NewPrincipal("bob")); err != nil {
		panic("failed to set bob principal")
	}
	for _, r := range []*context.T{rootCtx, aliceCtx, bobCtx} {
		// A hack to set the namespace roots to a value that won't work.
		v23.GetNamespace(r).SetRoots()
		// And have all principals recognize each others blessings.
		p1 := v23.GetPrincipal(r)
		for _, other := range []*context.T{rootCtx, aliceCtx, bobCtx} {
			// testutil.NewPrincipal has already setup each
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

type nopServer struct{ x int }

func (s *nopServer) NOP(call rpc.ServerCall) error {
	return nil
}

var nobody = []security.BlessingPattern{""}
var everybody = []security.BlessingPattern{"..."}
var closedAccessList = access.Permissions{
	"Resolve": access.AccessList{
		In: nobody,
	},
	"Read": access.AccessList{
		In: nobody,
	},
	"Admin": access.AccessList{
		In: nobody,
	},
	"Create": access.AccessList{
		In: nobody,
	},
	"Mount": access.AccessList{
		In: nobody,
	},
}
var openAccessList = access.Permissions{
	"Resolve": access.AccessList{
		In: everybody,
	},
	"Read": access.AccessList{
		In: everybody,
	},
	"Admin": access.AccessList{
		In: everybody,
	},
	"Create": access.AccessList{
		In: everybody,
	},
	"Mount": access.AccessList{
		In: everybody,
	},
}

func TestAccessLists(t *testing.T) {
	// Create three different personalities.
	// TODO(p): Use the multiple personalities to test AccessList functionality.
	rootCtx, aliceCtx, _, shutdown := initTest()
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

	// Set/Get the mount point's AccessList.
	acl, etag, err := ns.GetPermissions(rootCtx, "a/b/c")
	if err != nil {
		t.Fatalf("GetPermissions a/b/c: %s", err)
	}
	if err := ns.SetPermissions(rootCtx, "a/b/c", openAccessList, etag); err != nil {
		t.Fatalf("SetPermissions a/b/c: %s", err)
	}
	nacl, _, err := ns.GetPermissions(rootCtx, "a/b/c")
	if err != nil {
		t.Fatalf("GetPermissions a/b/c: %s", err)
	}
	if !reflect.DeepEqual(openAccessList, nacl) {
		t.Fatalf("want %v, got %v", openAccessList, nacl)
	}

	// Now Set/Get the parallel mount point's AccessList.
	name := "a/b/c/d/e"
	etag = "" // Parallel setacl with any other value is dangerous
	if err := ns.SetPermissions(rootCtx, name, openAccessList, etag); err != nil {
		t.Fatalf("SetPermissions %s: %s", name, err)
	}
	nacl, _, err = ns.GetPermissions(rootCtx, name)
	if err != nil {
		t.Fatalf("GetPermissions %s: %s", name, err)
	}
	if !reflect.DeepEqual(openAccessList, nacl) {
		t.Fatalf("want %v, got %v", openAccessList, nacl)
	}

	// Get from each server individually to make sure both are set.
	name = naming.Join(mt1Addr, "d/e")
	nacl, _, err = ns.GetPermissions(rootCtx, name)
	if err != nil {
		t.Fatalf("GetPermissions %s: %s", name, err)
	}
	if !reflect.DeepEqual(openAccessList, nacl) {
		t.Fatalf("want %v, got %v", openAccessList, nacl)
	}
	name = naming.Join(mt2Addr, "d/e")
	nacl, _, err = ns.GetPermissions(rootCtx, name)
	if err != nil {
		t.Fatalf("GetPermissions %s: %s", name, err)
	}
	if !reflect.DeepEqual(openAccessList, nacl) {
		t.Fatalf("want %v, got %v", acl, nacl)
	}

	// Create mount points accessible only by root's key.
	name = "a/b/c/d/f"
	deadbody := "/the:8888/rain"
	if err := ns.SetPermissions(rootCtx, name, closedAccessList, etag); err != nil {
		t.Fatalf("SetPermissions %s: %s", name, err)
	}
	nacl, _, err = ns.GetPermissions(rootCtx, name)
	if err != nil {
		t.Fatalf("GetPermissions %s: %s", name, err)
	}
	if !reflect.DeepEqual(closedAccessList, nacl) {
		t.Fatalf("want %v, got %v", closedAccessList, nacl)
	}
	if err := ns.Mount(rootCtx, name, deadbody, 10000); err != nil {
		t.Fatalf("Mount %s: %s", name, err)
	}

	// Alice shouldn't be able to resolve it.
	_, err = v23.GetNamespace(aliceCtx).Resolve(aliceCtx, name)
	if err == nil {
		t.Fatalf("as alice we shouldn't be able to Resolve %s", name)
	}

	// Root should be able to resolve it.
	_, err = ns.Resolve(rootCtx, name)
	if err != nil {
		t.Fatalf("as root Resolve %s: %s", name, err)
	}

	// Create a mount point via Serve accessible only by root's key.
	name = "a/b/c/d/g"
	if err := ns.SetPermissions(rootCtx, name, closedAccessList, etag); err != nil {
		t.Fatalf("SetPermissions %s: %s", name, err)
	}
	server, err := v23.NewServer(rootCtx)
	if err != nil {
		t.Fatalf("v23.NewServer failed: %v", err)
	}
	if _, err := server.Listen(v23.GetListenSpec(rootCtx)); err != nil {
		t.Fatalf("Failed to Listen: %s", err)
	}
	if err := server.Serve(name, &nopServer{1}, nil); err != nil {
		t.Fatalf("Failed to Serve: %s", err)
	}

	// Alice shouldn't be able to resolve it.
	_, err = v23.GetNamespace(aliceCtx).Resolve(aliceCtx, name)
	if err == nil {
		t.Fatalf("as alice we shouldn't be able to Resolve %s", name)
	}

	// Root should be able to resolve it.
	_, err = ns.Resolve(rootCtx, name)
	if err != nil {
		t.Fatalf("as root Resolve %s: %s", name, err)
	}
}
