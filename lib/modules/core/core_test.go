package core_test

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/modules/core"

	_ "veyron.io/veyron/veyron/lib/testutil"
)

func TestCommands(t *testing.T) {
	shell := core.NewShell()
	defer shell.Cleanup(nil, os.Stderr)
	for _, c := range []string{core.RootMTCommand, core.MTCommand} {
		if len(shell.Help(c)) == 0 {
			t.Fatalf("missing command %q", c)
		}
	}
}

func init() {
	rt.Init()
}

func newShell() (*modules.Shell, func()) {
	sh := core.NewShell()
	return sh, func() {
		if testing.Verbose() {
			sh.Cleanup(os.Stderr, os.Stderr)
		} else {
			sh.Cleanup(nil, nil)
		}
	}
}

func TestRoot(t *testing.T) {
	shell, fn := newShell()
	defer fn()
	root, err := shell.Start(core.RootMTCommand)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, root.Stdout(), time.Second)
	s.ExpectVar("MT_NAME")
	s.ExpectVar("MT_ADDR")
	s.ExpectVar("PID")
	root.CloseStdin()
}

func startMountTables(t *testing.T, sh *modules.Shell, mountPoints ...string) (map[string]string, error) {
	// Start root mount table
	root, err := sh.Start(core.RootMTCommand)
	if err != nil {
		t.Fatalf("unexpected error for root mt: %s", err)
	}
	rootSession := expect.NewSession(t, root.Stdout(), time.Minute)
	rootName := rootSession.ExpectVar("MT_NAME")
	if t.Failed() {
		return nil, rootSession.Error()
	}
	sh.SetVar("NAMESPACE_ROOT", rootName)
	mountAddrs := make(map[string]string)
	mountAddrs["root"] = rootName

	// Start the mount tables
	for _, mp := range mountPoints {
		h, err := sh.Start(core.MTCommand, mp)
		if err != nil {
			return nil, fmt.Errorf("unexpected error for mt %q: %s", mp, err)
		}
		s := expect.NewSession(t, h.Stdout(), time.Minute)
		// Wait until each mount table has at least called Serve to
		// mount itself.
		mountAddrs[mp] = s.ExpectVar("MT_NAME")
		if s.Failed() {
			return nil, s.Error()
		}
	}
	return mountAddrs, nil

}

func getMatchingMountpoint(r [][]string) string {
	if len(r) != 1 {
		return ""
	}
	shortest := ""
	for _, p := range r[0][1:] {
		if len(p) > 0 {
			if len(shortest) == 0 {
				shortest = p
			}
			if len(shortest) > 0 && len(p) < len(shortest) {
				shortest = p
			}
		}
	}
	return shortest
}

func TestMountTableAndGlob(t *testing.T) {
	shell, fn := newShell()
	defer fn()

	mountPoints := []string{"a", "b", "c", "d", "e"}
	mountAddrs, err := startMountTables(t, shell, mountPoints...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rootName := mountAddrs["root"]
	ls, err := shell.Start(core.LSCommand, rootName+"/...")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	lsSession := expect.NewSession(t, ls.Stdout(), time.Minute)
	lsSession.SetVerbosity(testing.Verbose())

	if got, want := lsSession.ExpectVar("RN"), strconv.Itoa(len(mountPoints)+1); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	lsSession.Expect("R0=" + rootName)

	// Look for names that correspond to the mountpoints above (i.e, a, b or c)
	pattern := ""
	for _, n := range mountPoints {
		pattern = pattern + "^R[\\d]+=(" + rootName + "/(" + n + ")$)|"
	}
	pattern = pattern[:len(pattern)-1]

	found := []string{}
	for i := 0; i < len(mountPoints); i++ {
		found = append(found, getMatchingMountpoint(lsSession.ExpectRE(pattern, 1)))
	}
	sort.Strings(found)
	sort.Strings(mountPoints)
	if got, want := found, mountPoints; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Run the ls command in a subprocess, with NAMESPACE_ROOT as set above.
	lse, err := shell.Start(core.LSExternalCommand, "...")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	lseSession := expect.NewSession(t, lse.Stdout(), time.Minute)
	lseSession.SetVerbosity(testing.Verbose())

	if got, want := lseSession.ExpectVar("RN"), strconv.Itoa(len(mountPoints)); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	pattern = ""
	for _, n := range mountPoints {
		// Since the LSExternalCommand runs in a subprocess with NAMESPACE_ROOT
		// set to the name of the root mount table it sees to the relative name
		// format of the mounted mount tables.
		pattern = pattern + "^R[\\d]+=(" + n + "$)|"
	}
	pattern = pattern[:len(pattern)-1]
	found = []string{}
	for i := 0; i < len(mountPoints); i++ {
		found = append(found, getMatchingMountpoint(lseSession.ExpectRE(pattern, 1)))
	}
	sort.Strings(found)
	sort.Strings(mountPoints)
	if got, want := found, mountPoints; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEcho(t *testing.T) {
	shell, fn := newShell()
	defer fn()

	srv, err := shell.Start(core.EchoServerCommand, "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	srvSession := expect.NewSession(t, srv.Stdout(), time.Minute)
	name := srvSession.ExpectVar("NAME")
	if len(name) == 0 {
		t.Fatalf("failed to get name")
	}

	clt, err := shell.Start(core.EchoClientCommand, name, "a message")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	cltSession := expect.NewSession(t, clt.Stdout(), time.Minute)
	cltSession.Expect("test: a message")
}

func TestResolve(t *testing.T) {
	shell, fn := newShell()
	defer fn()

	mountPoints := []string{"a", "b"}
	mountAddrs, err := startMountTables(t, shell, mountPoints...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rootName := mountAddrs["root"]
	mtName := "b"
	echoName := naming.Join(mtName, "echo")
	srv, err := shell.Start(core.EchoServerCommand, "test", echoName)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	srvSession := expect.NewSession(t, srv.Stdout(), time.Minute)
	srvSession.ExpectVar("NAME")
	addr := srvSession.ExpectVar("ADDR")
	addr = naming.JoinAddressName(addr, "//")

	// Resolve an object
	resolver, err := shell.Start(core.ResolveCommand, rootName+"/"+echoName)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	resolverSession := expect.NewSession(t, resolver.Stdout(), time.Minute)
	if got, want := resolverSession.ExpectVar("RN"), "1"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := resolverSession.ExpectVar("R0"), addr; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if err = resolver.Shutdown(nil, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// Resolve to a mount table using a rooted name.
	addr = naming.JoinAddressName(mountAddrs[mtName], "//echo")
	resolver, err = shell.Start(core.ResolveMTCommand, rootName+"/"+echoName)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	resolverSession = expect.NewSession(t, resolver.Stdout(), time.Minute)
	if got, want := resolverSession.ExpectVar("RN"), "1"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := resolverSession.ExpectVar("R0"), addr; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := resolver.Shutdown(nil, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// Resolve to a mount table, but using a relative name.
	nsroots, err := shell.Start(core.SetNamespaceRootsCommand, rootName)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := nsroots.Shutdown(nil, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	resolver, err = shell.Start(core.ResolveMTCommand, echoName)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	resolverSession = expect.NewSession(t, resolver.Stdout(), time.Minute)
	if got, want := resolverSession.ExpectVar("RN"), "1"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := resolverSession.ExpectVar("R0"), addr; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := resolver.Shutdown(nil, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}
