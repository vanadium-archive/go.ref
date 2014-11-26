package ipc_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/modules/core"
)

func init() {
	rt.Init()
}

func testArgs(args ...string) []string {
	var targs = []string{"--", "--veyron.tcp.address=127.0.0.1:0"}
	return append(targs, args...)
}

func runMountTable(t *testing.T) (*modules.Shell, func()) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	root, err := sh.Start(core.RootMTCommand, nil, testArgs()...)
	if err != nil {
		t.Fatalf("unexpected error for root mt: %s", err)
	}
	sh.Forget(root)

	rootSession := expect.NewSession(t, root.Stdout(), time.Minute)
	rootName := rootSession.ExpectVar("MT_NAME")
	if t.Failed() {
		t.Fatalf("%s", rootSession.Error())
	}
	sh.SetVar(consts.NamespaceRootPrefix, rootName)
	sh.Start(core.SetNamespaceRootsCommand, nil, rootName)

	deferFn := func() {
		if testing.Verbose() {
			vlog.Infof("------ root shutdown ------")
			root.Shutdown(os.Stderr, os.Stderr)
		} else {
			root.Shutdown(nil, nil)
		}
	}
	return sh, deferFn
}

func runClient(t *testing.T, sh *modules.Shell) error {
	clt, err := sh.Start(core.EchoClientCommand, nil, "echoServer", "a message")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, clt.Stdout(), 30*time.Second)
	s.Expect("echoServer: a message")
	if s.Failed() {
		return s.Error()
	}
	return nil
}

func numServers(t *testing.T, sh *modules.Shell, name string) string {
	r, err := sh.Start(core.ResolveCommand, nil, "echoServer")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, r.Stdout(), time.Minute)
	rn := s.ExpectVar("RN")
	return rn
}

// TODO(cnicolaou): figure out how to test and see what the internals
// of tryCall are doing - e.g. using stats counters.
func TestMultipleEndpoints(t *testing.T) {
	sh, fn := runMountTable(t)
	defer fn()
	srv, _ := sh.Start(core.EchoServerCommand, nil, testArgs("echoServer", "echoServer")...)
	s := expect.NewSession(t, srv.Stdout(), time.Minute)
	s.ExpectVar("NAME")

	runClient(t, sh)

	// Create a fake set of 100 entries in the mount table
	for i := 0; i < 100; i++ {
		// 203.0.113.0 is TEST-NET-3 from RFC5737
		ep := naming.FormatEndpoint("tcp", fmt.Sprintf("203.0.113.%d:443", i))
		n := naming.JoinAddressName(ep, "")
		h, err := sh.Start(core.MountCommand, nil, "echoServer", n, "1h")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if err := h.Shutdown(nil, os.Stderr); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// Verify that there are 102 entries for echoServer in the mount table.
	if got, want := numServers(t, sh, "echoServer"), "102"; got != want {
		vlog.Fatalf("got: %q, want: %q", got, want)
	}

	// TODO(cnicolaou): ok, so it works, but I'm not sure how
	// long it should take or if the parallel connection code
	// really works. Use counters to inspect it for example.
	if err := runClient(t, sh); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	srv.CloseStdin()
	srv.Shutdown(nil, nil)

	// TODO(cnicolaou,p): figure out why the real entry isn't removed
	// from the mount table.
	// Verify that there are 100 entries for echoServer in the mount table.
	if got, want := numServers(t, sh, "echoServer"), "102"; got != want {
		vlog.Fatalf("got: %q, want: %q", got, want)
	}
}
