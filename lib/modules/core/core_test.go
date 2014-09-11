package core_test

import (
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"veyron2/rt"

	"veyron/lib/expect"
	"veyron/lib/modules"
	"veyron/lib/modules/core"
	_ "veyron/lib/testutil"
)

func TestCommands(t *testing.T) {
	shell := core.NewShell()
	defer shell.Cleanup(os.Stderr)
	for _, c := range []string{core.RootMTCommand, core.MTCommand} {
		if len(shell.Help(c)) == 0 {
			t.Fatalf("missing command %q", c)
		}
	}
}

func init() {
	rt.Init()
}

func TestRoot(t *testing.T) {
	shell := core.NewShell()
	defer shell.Cleanup(os.Stderr)

	root, err := shell.Start(core.RootMTCommand)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, root.Stdout(), time.Second)
	s.ExpectVar("MT_NAME")
	s.ExpectVar("PID")
	root.Stdin().Close()
	s.Expect("done")
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
	shell := core.NewShell()
	if testing.Verbose() {
		defer shell.Cleanup(os.Stderr)
	} else {
		defer shell.Cleanup(nil)
	}

	// Start root mount table
	root, err := shell.Start(core.RootMTCommand)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rootSession := expect.NewSession(t, root.Stdout(), time.Second)
	rootName := rootSession.ExpectVar("MT_NAME")
	shell.SetVar("NAMESPACE_ROOT", rootName)

	if t.Failed() {
		return
	}
	mountPoints := []string{"a", "b", "c", "d", "e"}

	// Start 3 mount tables
	for _, mp := range mountPoints {
		h, err := shell.Start(core.MTCommand, mp)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		s := expect.NewSession(t, h.Stdout(), time.Second)
		// Wait until each mount table has at least called Serve to
		// mount itself.
		s.ExpectVar("MT_NAME")

	}

	ls, err := shell.Start(core.LSCommand, rootName+"/...")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	lsSession := expect.NewSession(t, ls.Stdout(), time.Second)

	lsSession.SetVerbosity(testing.Verbose())
	lsSession.Expect(rootName)

	// Look for names that correspond to the mountpoints above (i.e, a, b or c)
	pattern := ""
	for _, n := range mountPoints {
		pattern = pattern + "(" + rootName + "/(" + n + ")$)|"
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
	lseSession := expect.NewSession(t, lse.Stdout(), time.Second)
	lseSession.SetVerbosity(testing.Verbose())

	pattern = ""
	for _, n := range mountPoints {
		// Since the LSExternalCommand runs in a subprocess with NAMESPACE_ROOT
		// set to the name of the root mount table it sees to the relative name
		// format of the mounted mount tables.
		pattern = pattern + "(^" + n + "$)|"
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

func TestHelperProcess(t *testing.T) {
	if !modules.IsTestHelperProcess() {
		return
	}
	if err := modules.Dispatch(); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
