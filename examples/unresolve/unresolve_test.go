package unresolve

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	"veyron/lib/testutil/security"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

// The test sets up a topology of mounttables and servers (the latter configured
// in various ways, with/without custom UnresolveStep methods, mounting
// themselves with one or several names under a mounttable (directly or via
// a name that resolves to a mounttable). The "ASCII" art below illustrates the
// setup, with a description of each node in the topology following.
//
//       (A)
//        |
//        |"b"
//        |
//       (B)
//         \_______________________________________________
//          |    |    |                  |     |     |    |
//          |"c" |"d" | "I/want/to/know" |"e1" |"e2" |"f" |"g"
//          |    |    | "tell/me"        |     |     |    |
//         (C)  (D)  (D)                (E)   (E)    (F) (G)
//
// A mounttable service (A) with OA "aEP" acting as a root mounttable.
//
// A mounttable service (B) with OA "bEP", mounting its ep "bEP" as "b" under
// mounttable A.
//
// A fortune service (C) with OA "cEP", mounting its ep "cEP" as "c"
// under mounttable B.  [ This is the vanilla case. ]
//
// A fortune service (D) with OA "dEP", mounting its ep "dEP"
// automatically as "d" under mounttable B, and also mounting the OA
// "dEP" manually as "I/want/to/know" and "tell/me under mounttable B.
// It implements its own custom UnresolveStep.
// [ This demonstrates using a custom UnresolveStep implementation with
// an IDL-based service. ]
//
// A fortune service (E) with OA "eEP", mounting its ep "eEP" as "e1"
// and as "e2" under mounttable B.  [ This shows a service published under more
// than one name. ]
//
// A fortune service (F) with OA "fEP", with mounttable root "aOA/b/"
// mounting its ep "fEP" as "f" under "aOA/b" (essentially, under mounttable
// B).  [ This shows a service whose local root is a name that resolves to a
// mounttable via another mounttable (so local root is not just an OA ].
// TODO(cnicolaou): move this case (multihop namespace root to the namespace
// tests)
//
// A fortune service (G) with OA "gEP" that is not based on an IDL.  G
// mounts itself as "g" under mounttable B, and defines its own UnresolveStep.
// [ This demonstrates defining UnresolveStep for a service that is not
// IDL-based. ]
//

func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func init() {
	blackbox.CommandTable["childMT"] = childMT
	blackbox.CommandTable["childFortune"] = childFortune
	blackbox.CommandTable["childFortuneCustomUnresolve"] = childFortuneCustomUnresolve
	blackbox.CommandTable["childFortuneNoIDL"] = childFortuneNoIDL
}

// TODO(caprita): Consider making shutdown part of blackbox.Child.
func shutdown(child *blackbox.Child) {
	child.CloseStdin()
	child.ExpectEOFAndWait()
	child.Cleanup()
}

func TestUnresolve(t *testing.T) {
	// Create the set of servers and ensure that the right mounttables act
	// as namespace roots for each. We run all servers with an identity blessed
	// by the root mounttable's identity under the name "test", i.e., if <rootMT>
	// is the identity of the root mounttable then the servers run with the identity
	// <rootMT>/test.
	// TODO(ataly): Eventually we want to use the same identities the servers
	// would have if they were running in production.
	defer initRT()()

	// Create mounttable A.
	mtServer, aOA := createMTServer("")
	if len(aOA) == 0 {
		t.Fatalf("aOA is empty")
	}
	defer mtServer.Stop()
	rt.R().Namespace().SetRoots(aOA)

	// A's object addess, aOA, is /<address>
	vlog.Infof("aOA=%v", aOA)

	idA := rt.R().Identity()
	vlog.Infof("idA=%v", idA)

	// Create mounttable B.
	// Mounttable B uses A as a root, that is, Publish calls made for
	// services running in B will appear in A, as <suffix used in publish>
	b := blackbox.HelperCommand(t, "childMT", "b")
	defer shutdown(b)

	idFile := security.SaveIdentityToFile(security.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	b.Cmd.Env = append(b.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("NAMESPACE_ROOT=%v", aOA))
	b.Cmd.Start()
	b.Expect("ready")

	// We want to obtain the name for the MountTable mounted in A as b,
	// in particular, we want the OA of B's Mounttable service.
	// We do so by asking Mounttable A to resolve //b using its
	// ResolveStep method. The name (//b) has to be terminal since we are
	// invoking a methond on the MountTable rather asking the MountTable to
	// resolve the name!
	aName := naming.Join(aOA, "//b")

	bOA := resolveStep(t, aName)
	vlog.Infof("bOA=%v", bOA)
	bAddr, _ := naming.SplitAddressName(bOA)

	// Create server C.
	c := blackbox.HelperCommand(t, "childFortune", "c")
	defer shutdown(c)
	idFile = security.SaveIdentityToFile(security.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	c.Cmd.Env = append(c.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("NAMESPACE_ROOT=%v", bOA))
	c.Cmd.Start()
	c.Expect("ready")
	cEP := resolveStep(t, naming.Join(bOA, "//c"))
	vlog.Infof("cEP=%v", cEP)

	// Create server D and the fortune service with a custom unresolver
	d := blackbox.HelperCommand(t, "childFortuneCustomUnresolve", "d")
	defer shutdown(d)
	idFile = security.SaveIdentityToFile(security.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	d.Cmd.Env = append(d.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("NAMESPACE_ROOT=%v", bOA))
	d.Cmd.Start()
	d.Expect("ready")
	dEP := resolveStep(t, naming.Join(bOA, "//d"))
	vlog.Infof("dEP=%v", dEP)

	// Create server E.
	e := blackbox.HelperCommand(t, "childFortune", "e1", "e2")
	defer shutdown(e)
	idFile = security.SaveIdentityToFile(security.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	e.Cmd.Env = append(e.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("NAMESPACE_ROOT=%v", bOA))
	e.Cmd.Start()
	e.Expect("ready")
	eEP := resolveStep(t, naming.Join(bOA, "//e1"))
	vlog.Infof("eEP=%v", eEP)

	f := blackbox.HelperCommand(t, "childFortune", "f")
	defer shutdown(f)
	idFile = security.SaveIdentityToFile(security.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	f.Cmd.Env = append(f.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("NAMESPACE_ROOT=%v", naming.Join(aOA, "b")))
	f.Cmd.Start()
	f.Expect("ready")
	fEP := resolveStep(t, naming.JoinAddressName(bAddr, "//f"))
	vlog.Infof("fEP=%v", fEP)

	// Create server G.
	g := blackbox.HelperCommand(t, "childFortuneNoIDL", "g")
	defer shutdown(g)
	idFile = security.SaveIdentityToFile(security.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	g.Cmd.Env = append(g.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("NAMESPACE_ROOT=%v", bOA))
	g.Cmd.Start()
	g.Expect("ready")
	gEP := resolveStep(t, naming.Join(bOA, "//g"))
	vlog.Infof("gEP=%v", gEP)

	vlog.Infof("ls /... %v", glob(t, "..."))

	// Check that things resolve correctly.

	// Create a client runtime with oOA as its root.
	idFile = security.SaveIdentityToFile(security.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	os.Setenv("VEYRON_IDENTITY", idFile)

	r, _ := rt.New(veyron2.NamespaceRoots([]string{aOA}))
	ctx := r.NewContext()

	resolveCases := []struct {
		name, resolved string
	}{
		{"b/c", cEP},
		{"b/d", dEP},
		{"b/I/want/to/know", dEP},
		{"b/tell/me/the/future", naming.Join(dEP, "the/future")},
		{"b/e1", eEP},
		{"b/e2", eEP},
		{"b/g", gEP},
	}
	for _, c := range resolveCases {
		if want, got := c.resolved, resolve(t, r.Namespace(), c.name); want != got {
			t.Errorf("resolve %q expected %q, got %q instead", c.name, want, got)
		}
	}

	// Verify that we can talk to the servers we created, and that unresolve
	// one step at a time works.
	unresolveStepCases := []struct {
		name, unresStep1, unresStep2 string
	}{
		{
			"b/c",
			naming.Join(bOA, "c"),
			naming.Join(aOA, "b/c"),
		},
		{
			"b/tell/me",
			naming.Join(bOA, "I/want/to/know/the/future"),
			naming.Join(aOA, "b/I/want/to/know/the/future"),
		},
		{
			"b/f",
			naming.Join(bOA, "f"),
			naming.Join(aOA, "b/f"),
		},
		{
			"b/g",
			naming.Join(bOA, "g/fortune"),
			naming.Join(aOA, "b/g/fortune"),
		},
	}
	for _, c := range unresolveStepCases {
		// Verify that we can talk to the server.
		client := createFortuneClient(r, c.name)
		if fortuneMessage, err := client.Get(r.NewContext()); err != nil {
			t.Errorf("fortune.Get: %s failed with %v", c.name, err)
		} else if fortuneMessage != fixedFortuneMessage {
			t.Errorf("fortune expected %q, got %q instead", fixedFortuneMessage, fortuneMessage)
		}

		// Unresolve, one step.
		if want, got := c.unresStep1, unresolveStep(t, ctx, client); want != got {
			t.Errorf("fortune.UnresolveStep expected %q, got %q instead", want, got)
		}

		// Go up the tree, unresolve another step.
		if want, got := c.unresStep2, unresolveStep(t, ctx, createMTClient(naming.MakeTerminal(c.unresStep1))); want != got {
			t.Errorf("mt.UnresolveStep expected %q, got %q instead", want, got)
		}
	}

	// We handle (E) separately since its UnresolveStep returns two names
	// instead of one.

	// Verify that we can talk to server E.
	eClient := createFortuneClient(r, "b/e1")
	if fortuneMessage, err := eClient.Get(ctx); err != nil {
		t.Errorf("fortune.Get failed with %v", err)
	} else if fortuneMessage != fixedFortuneMessage {
		t.Errorf("fortune expected %q, got %q instead", fixedFortuneMessage, fortuneMessage)
	}

	// Unresolve E, one step.
	eUnres, err := eClient.UnresolveStep(ctx)
	if err != nil {
		t.Errorf("UnresolveStep failed with %v", err)
	}
	if want, got := []string{naming.Join(bOA, "e1"), naming.Join(bOA, "e2")}, eUnres; !reflect.DeepEqual(want, got) {
		t.Errorf("e.UnresolveStep expected %q, got %q instead", want, got)
	}

	// Try unresolve step on a random name in B.
	if want, got := naming.Join(aOA, "b/some/random/name"),
		unresolveStep(t, ctx, createMTClient(naming.Join(bOA, "//some/random/name"))); want != got {
		t.Errorf("b.UnresolveStep expected %q, got %q instead", want, got)
	}

	// Try unresolve step on a random name in A.
	if unres, err := createMTClient(naming.Join(aOA, "//another/random/name")).UnresolveStep(ctx); err != nil {
		t.Errorf("UnresolveStep failed with %v", err)
	} else if len(unres) > 0 {
		t.Errorf("b.UnresolveStep expected no results, got %q instead", unres)
	}

	// Verify that full unresolve works.
	unresolveCases := []struct {
		name, want string
	}{
		{"b/c", naming.Join(aOA, "b/c")},
		{naming.Join(bOA, "c"), naming.Join(aOA, "b/c")},
		{naming.Join(cEP, ""), naming.Join(aOA, "b/c")},

		{"b/tell/me/the/future", naming.Join(aOA, "b/I/want/to/know/the/future")},
		{"b/I/want/to/know/the/future", naming.Join(aOA, "b/I/want/to/know/the/future")},
		{naming.Join(bOA, "d/tell/me/the/future"), naming.Join(aOA, "b/I/want/to/know/the/future")},

		{naming.Join(bOA, "I/want/to/know/the/future"), naming.Join(aOA, "b/I/want/to/know/the/future")},
		{naming.Join(dEP, "tell/me/the/future"), naming.Join(aOA, "b/I/want/to/know/the/future")},

		{"b/e1", naming.Join(aOA, "b/e1")},
		{"b/e2", naming.Join(aOA, "b/e1")},

		{"b/f", naming.Join(aOA, "b/f")},
		{naming.Join(fEP, ""), naming.Join(aOA, "b/f")},

		{"b/g", naming.Join(aOA, "b/g/fortune")},
		{naming.Join(bOA, "g"), naming.Join(aOA, "b/g/fortune")},
		{naming.Join(gEP, ""), naming.Join(aOA, "b/g/fortune")},
	}
	for _, c := range unresolveCases {
		if want, got := c.want, unresolve(t, r.Namespace(), c.name); want != got {
			t.Errorf("unresolve %q expected %q, got %q instead", c.name, want, got)
		}
	}
}
