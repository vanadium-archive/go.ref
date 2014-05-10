package unresolve

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"

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
//         \_____________________________________________
//          |    |    |                  |     |     |   \
//          |"c" |"d" | "I/want/to/know" |"e1" |"e2" |"f" |"g"
//          |    |    |                  |     |     |    |
//         (C)  (D)  (D/tell/me")       (E)   (E)   (F)  (G)
//
// A mounttable service (A) with OA "aEP/mt" acting as a root mounttable.
//
// A mounttable service (B) with OA "bEP/mt", mounting its ep "bEP" as "b" under
// mounttable A.
//
// A fortune service (C) with OA "eEP/fortune", mouting its ep "eEP" as "c"
// under mounttable B.  [ This is the vanilla case. ]
//
// A fortune service (D) with OA "dEP/tell/me/the/future", mounting its ep "dEP"
// automatically as "d" under mounttable B, and also mounting the OA
// "dEP/tell/me" manually as "I/want/to/know" under mounttable B.  It implements
// its own custom UnresolveStep.  [ This demonstrates using a custom
// UnresolveStep implementation with an IDL-based service. ]
//
// A fortune service (E) with OA "eEP/fortune", mounting its ep "eEP" as "e1"
// and as "e2" under mounttable B.  [ This shows a service published under more
// than one name. ]
//
// A fortune service (F) with OA "fEP/fortune", with mounttable root "aOA/b/mt"
// mounting its ep "fEP" as "f" under "aOA/b/mt" (essentially, under mounttable
// B).  [ This shows a service whose local root is a name that resolves to a
// mounttable via another mounttable (so local root is not just an OA ].
//
// A fortune service (G) with OA "gEP/fortune" that is not based on an IDL.  G
// mounts itself as "g" under mounttable B, and defines its own UnresolveStep.
// [ This demonstrates defining UnresolveStep for a service that is not
// IDL-based. ]

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
	server := newServer()
	defer server.Stop()

	// Create mounttable A.
	aOA := createMT(server)
	if len(aOA) == 0 {
		t.Fatalf("aOA is empty")
	}
	vlog.Infof("aOA=%v", aOA)
	idA := rt.R().Identity()
	vlog.Infof("idA=%v", idA)
	// Create mounttable B.
	b := blackbox.HelperCommand(t, "childMT", "b")
	defer shutdown(b)

	idFile := testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	b.Cmd.Env = append(b.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("MOUNTTABLE_ROOT=%v", aOA))
	b.Cmd.Start()
	b.Expect("ready")
	bOA := naming.Join(resolveStep(t, naming.MakeFixed(naming.Join(aOA, "b"))), "mt")
	vlog.Infof("bOA=%v", bOA)

	// Create server C.
	c := blackbox.HelperCommand(t, "childFortune", "c")
	defer shutdown(c)
	idFile = testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	c.Cmd.Env = append(c.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("MOUNTTABLE_ROOT=%v", bOA))
	c.Cmd.Start()
	c.Expect("ready")
	cEP := resolveStep(t, naming.MakeFixed(naming.Join(bOA, "c")))
	vlog.Infof("cEP=%v", cEP)

	// Create server D.
	d := blackbox.HelperCommand(t, "childFortuneCustomUnresolve", "d")
	defer shutdown(d)
	idFile = testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	d.Cmd.Env = append(d.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("MOUNTTABLE_ROOT=%v", bOA))
	d.Cmd.Start()
	d.Expect("ready")
	dEP := resolveStep(t, naming.MakeFixed(naming.Join(bOA, "d")))
	vlog.Infof("dEP=%v", dEP)

	// Create server E.
	e := blackbox.HelperCommand(t, "childFortune", "e1", "e2")
	defer shutdown(e)
	idFile = testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	e.Cmd.Env = append(e.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("MOUNTTABLE_ROOT=%v", bOA))
	e.Cmd.Start()
	e.Expect("ready")
	eEP := resolveStep(t, naming.MakeFixed(naming.Join(bOA, "e1")))
	vlog.Infof("eEP=%v", eEP)

	// Create server F.
	f := blackbox.HelperCommand(t, "childFortune", "f")
	defer shutdown(f)
	idFile = testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	f.Cmd.Env = append(f.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("MOUNTTABLE_ROOT=%v", naming.Join(aOA, "b/mt")))
	f.Cmd.Start()
	f.Expect("ready")
	fEP := resolveStep(t, naming.MakeFixed(naming.Join(bOA, "f")))
	vlog.Infof("fEP=%v", fEP)

	// Create server G.
	g := blackbox.HelperCommand(t, "childFortuneNoIDL", "g")
	defer shutdown(g)
	idFile = testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	g.Cmd.Env = append(g.Cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%v", idFile), fmt.Sprintf("MOUNTTABLE_ROOT=%v", bOA))
	g.Cmd.Start()
	g.Expect("ready")
	gEP := resolveStep(t, naming.MakeFixed(naming.Join(bOA, "g")))
	vlog.Infof("gEP=%v", gEP)

	// Check that things resolve correctly.

	// Create a client runtime with oOA as its root.
	idFile = testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(idA, "test"))
	defer os.Remove(idFile)
	os.Setenv("VEYRON_IDENTITY", idFile)
	r, _ := rt.New(veyron2.MountTableRoots([]string{aOA}))
	resolveCases := []struct {
		name, resolved string
	}{
		{"b/mt/c", cEP},
		{"b/mt/d", dEP},
		{"b/mt/I/want/to/know", naming.JoinAddressNameFixed(dEP, "tell/me")},
		{"b/mt/e1", eEP},
		{"b/mt/e2", eEP},
		{"b/mt/f", fEP},
		{"b/mt/g", gEP},
	}
	for _, c := range resolveCases {
		if want, got := c.resolved, resolve(t, r.MountTable(), c.name); want != got {
			t.Errorf("resolve %q expected %q, got %q instead", c.name, want, got)
		}
	}

	// Verify that we can talk to the servers we created, and that unresolve
	// one step at a time works.

	unresolveStepCases := []struct {
		name, unresStep1, unresStep2 string
	}{
		{
			"b/mt/c/fortune",
			naming.Join(bOA, "c/fortune"),
			naming.Join(aOA, "b/mt/c/fortune"),
		},
		{
			"b/mt/d/tell/me/the/future",
			naming.Join(bOA, "I/want/to/know/the/future"),
			naming.Join(aOA, "b/mt/I/want/to/know/the/future"),
		},
		{
			"b/mt/f/fortune",
			naming.Join(bOA, "f/fortune"),
			naming.Join(aOA, "b/mt/f/fortune"),
		},
		{
			"b/mt/g/fortune",
			naming.Join(bOA, "g/fortune"),
			naming.Join(aOA, "b/mt/g/fortune"),
		},
	}
	for _, c := range unresolveStepCases {
		// Verify that we can talk to the server.
		client := createFortuneClient(r, c.name)
		if fortuneMessage, err := client.Get(); err != nil {
			t.Errorf("fortune.Get failed with %v", err)
		} else if fortuneMessage != fixedFortuneMessage {
			t.Errorf("fortune expected %q, got %q instead", fixedFortuneMessage, fortuneMessage)
		}

		// Unresolve, one step.
		if want, got := c.unresStep1, unresolveStep(t, client); want != got {
			t.Errorf("fortune.UnresolveStep expected %q, got %q instead", want, got)
		}

		// Go up the tree, unresolve another step.
		if want, got := c.unresStep2, unresolveStep(t, createMTClient(naming.MakeFixed(c.unresStep1))); want != got {
			t.Errorf("mt.UnresolveStep expected %q, got %q instead", want, got)
		}
	}
	// We handle (E) separately since its UnresolveStep returns two names
	// instead of one.

	// Verify that we can talk to server E.
	eClient := createFortuneClient(r, "b/mt/e1/fortune")
	if fortuneMessage, err := eClient.Get(); err != nil {
		t.Errorf("fortune.Get failed with %v", err)
	} else if fortuneMessage != fixedFortuneMessage {
		t.Errorf("fortune expected %q, got %q instead", fixedFortuneMessage, fortuneMessage)
	}

	// Unresolve E, one step.
	eUnres, err := eClient.UnresolveStep()
	if err != nil {
		t.Errorf("UnresolveStep failed with %v", err)
	}
	if want, got := []string{naming.Join(bOA, "e1/fortune"), naming.Join(bOA, "e2/fortune")}, eUnres; !reflect.DeepEqual(want, got) {
		t.Errorf("e.UnresolveStep expected %q, got %q instead", want, got)
	}

	// Try unresolve step on a random name in B.
	if want, got := naming.Join(aOA, "b/mt/some/random/name"),
		unresolveStep(t, createMTClient(naming.MakeFixed(naming.Join(bOA, "some/random/name")))); want != got {
		t.Errorf("b.UnresolveStep expected %q, got %q instead", want, got)
	}

	// Try unresolve step on a random name in A.
	if unres, err := createMTClient(naming.MakeFixed(naming.Join(aOA, "another/random/name"))).UnresolveStep(); err != nil {
		t.Errorf("UnresolveStep failed with %v", err)
	} else if len(unres) > 0 {
		t.Errorf("b.UnresolveStep expected no results, got %q instead", unres)
	}

	// Verify that full unresolve works.

	unresolveCases := []struct {
		name, want string
	}{
		{"b/mt/c/fortune", naming.Join(aOA, "b/mt/c/fortune")},
		{naming.Join(bOA, "c/fortune"), naming.Join(aOA, "b/mt/c/fortune")},
		{naming.Join(cEP, "fortune"), naming.Join(aOA, "b/mt/c/fortune")},

		{"b/mt/d/tell/me/the/future", naming.Join(aOA, "b/mt/I/want/to/know/the/future")},
		{naming.Join(bOA, "d/tell/me/the/future"), naming.Join(aOA, "b/mt/I/want/to/know/the/future")},
		{"b/mt/I/want/to/know/the/future", naming.Join(aOA, "b/mt/I/want/to/know/the/future")},
		{naming.Join(bOA, "I/want/to/know/the/future"), naming.Join(aOA, "b/mt/I/want/to/know/the/future")},
		{naming.Join(dEP, "tell/me/the/future"), naming.Join(aOA, "b/mt/I/want/to/know/the/future")},

		{"b/mt/e1/fortune", naming.Join(aOA, "b/mt/e1/fortune")},
		{"b/mt/e2/fortune", naming.Join(aOA, "b/mt/e1/fortune")},

		{"b/mt/f/fortune", naming.Join(aOA, "b/mt/f/fortune")},
		{naming.Join(fEP, "fortune"), naming.Join(aOA, "b/mt/f/fortune")},
		{"b/mt/g/fortune", naming.Join(aOA, "b/mt/g/fortune")},
		{naming.Join(bOA, "g/fortune"), naming.Join(aOA, "b/mt/g/fortune")},
		{naming.Join(gEP, "fortune"), naming.Join(aOA, "b/mt/g/fortune")},
	}
	for _, c := range unresolveCases {
		if want, got := c.want, unresolve(t, r.MountTable(), c.name); want != got {
			t.Errorf("unresolve %q expected %q, got %q instead", c.name, want, got)
		}
	}
}
