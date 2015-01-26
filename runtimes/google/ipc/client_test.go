package ipc_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/flags/consts"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/modules/core"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	inaming "v.io/core/veyron/runtimes/google/naming"
)

func init() {
	modules.RegisterChild("ping", "<name>", childPing)
}

func newCtx() (*context.T, veyron2.Shutdown) {
	ctx, shutdown := veyron2.Init()
	ctx, err := veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-blessing"))
	if err != nil {
		panic(err)
	}

	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))
	return ctx, shutdown
}

func testArgs(args ...string) []string {
	var targs = []string{"--", "--veyron.tcp.address=127.0.0.1:0"}
	return append(targs, args...)
}

func runMountTable(t *testing.T, ctx *context.T) (*modules.Shell, func()) {
	sh, err := modules.NewShell(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	root, err := sh.Start(core.RootMTCommand, nil, testArgs()...)
	if err != nil {
		t.Fatalf("unexpected error for root mt: %s", err)
	}
	sh.Forget(root)

	rootSession := expect.NewSession(t, root.Stdout(), time.Minute)
	rootSession.ExpectVar("PID")
	rootName := rootSession.ExpectVar("MT_NAME")

	deferFn := func() {
		if testing.Verbose() {
			vlog.Infof("------ shell cleanup ------")
			sh.Cleanup(os.Stderr, os.Stderr)
			vlog.Infof("------ root shutdown ------")
			root.Shutdown(os.Stderr, os.Stderr)
		} else {
			sh.Cleanup(nil, nil)
			root.Shutdown(nil, nil)
		}
	}

	if t.Failed() {
		deferFn()
		t.Fatalf("%s", rootSession.Error())
	}
	sh.SetVar(consts.NamespaceRootPrefix, rootName)
	if err = veyron2.GetNamespace(ctx).SetRoots(rootName); err != nil {
		t.Fatalf("unexpected error setting namespace roots: %s", err)
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

func numServers(t *testing.T, ctx *context.T, name string) int {
	me, err := veyron2.GetNamespace(ctx).Resolve(ctx, name)
	if err != nil {
		return 0
	}
	return len(me.Servers)
}

// TODO(cnicolaou): figure out how to test and see what the internals
// of tryCall are doing - e.g. using stats counters.
func TestMultipleEndpoints(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()

	sh, fn := runMountTable(t, ctx)
	defer fn()
	srv, err := sh.Start(core.EchoServerCommand, nil, testArgs("echoServer", "echoServer")...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, srv.Stdout(), time.Minute)
	s.ExpectVar("PID")
	s.ExpectVar("NAME")

	// Verify that there are 1 entries for echoServer in the mount table.
	if got, want := numServers(t, ctx, "echoServer"), 1; got != want {
		t.Fatalf("got: %d, want: %d", got, want)
	}

	runClient(t, sh)

	// Create a fake set of 100 entries in the mount table
	for i := 0; i < 100; i++ {
		// 203.0.113.0 is TEST-NET-3 from RFC5737
		ep := naming.FormatEndpoint("tcp", fmt.Sprintf("203.0.113.%d:443", i))
		n := naming.JoinAddressName(ep, "")
		if err := veyron2.GetNamespace(ctx).Mount(ctx, "echoServer", n, time.Hour); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// Verify that there are 101 entries for echoServer in the mount table.
	if got, want := numServers(t, ctx, "echoServer"), 101; got != want {
		t.Fatalf("got: %q, want: %q", got, want)
	}

	// TODO(cnicolaou): ok, so it works, but I'm not sure how
	// long it should take or if the parallel connection code
	// really works. Use counters to inspect it for example.
	if err := runClient(t, sh); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	srv.CloseStdin()
	srv.Shutdown(nil, nil)

	// Verify that there are 100 entries for echoServer in the mount table.
	if got, want := numServers(t, ctx, "echoServer"), 100; got != want {
		t.Fatalf("got: %d, want: %d", got, want)
	}
}

func TestTimeoutCall(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	ctx, _ = context.WithTimeout(ctx, 100*time.Millisecond)
	name := naming.JoinAddressName(naming.FormatEndpoint("tcp", "203.0.113.10:443"), "")
	client := veyron2.GetClient(ctx)
	_, err := client.StartCall(ctx, name, "echo", []interface{}{"args don't matter"})
	if !verror.Is(err, verror.Timeout.ID) {
		t.Fatalf("wrong error: %s", err)
	}
}

func childPing(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	name := args[1]
	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Ping", nil)
	if err != nil {
		fmt.Errorf("unexpected error: %s", err)
	}
	got := ""
	verr := call.Finish(&got, &err)
	if verr != nil {
		fmt.Errorf("unexpected error: %s", verr)
	}
	if err != nil {
		fmt.Errorf("unexpected error: %s", err)
	}
	fmt.Fprintf(stdout, "RESULT=%s\n", got)
	return nil
}

func initServer(t *testing.T, ctx *context.T) (string, func()) {
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	done := make(chan struct{})
	deferFn := func() { close(done); server.Stop() }

	eps, err := server.Listen(veyron2.GetListenSpec(ctx))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	server.Serve("", &simple{done}, nil)
	return eps[0].Name(), deferFn
}

func testForVerror(t *testing.T, err error, verr ...verror.IDAction) {
	_, file, line, _ := runtime.Caller(1)
	loc := fmt.Sprintf("%s:%d", filepath.Base(file), line)
	found := false
	for _, v := range verr {
		if verror.Is(err, v.ID) {
			found = true
			break
		}
	}
	if !found {
		if _, ok := err.(verror.E); !ok {
			t.Fatalf("%s: err %v not a verror", loc, err)
		}
		stack := ""
		if err != nil {
			stack = err.(verror.E).Stack().String()
		}
		t.Fatalf("%s: expecting one of: %v, got: %v: stack: %s", loc, verr, err, stack)
	}
}

func TestTimeoutResponse(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()
	ctx, _ = context.WithTimeout(ctx, 100*time.Millisecond)
	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	if err != nil {
		testForVerror(t, err, verror.Timeout, verror.BadProtocol)
		return
	}
	verr := call.Finish(&err)
	// TODO(cnicolaou): this should be Timeout only.
	testForVerror(t, verr, verror.Timeout, verror.BadProtocol)
}

func TestArgsAndResponses(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Sleep", []interface{}{"too many args"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	verr := call.Finish(&err)
	testForVerror(t, verr, verror.BadProtocol)

	call, err = veyron2.GetClient(ctx).StartCall(ctx, name, "Ping", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	pong := ""
	dummy := ""
	verr = call.Finish(&pong, &dummy, &err)
	testForVerror(t, verr, verror.BadProtocol)
}

func TestAccessDenied(t *testing.T) {
	rootCtx, shutdown := veyron2.Init()
	defer shutdown()

	ctx1, err := veyron2.SetPrincipal(rootCtx, tsecurity.NewPrincipal("test-blessing"))
	if err != nil {
		t.Fatal(err)
	}

	name, fn := initServer(t, ctx1)
	defer fn()

	ctx2, err := veyron2.SetPrincipal(rootCtx, tsecurity.NewPrincipal("test-blessing"))
	if err != nil {
		t.Fatal(err)
	}
	client2 := veyron2.GetClient(ctx2)
	call, err := client2.StartCall(ctx2, name, "Sleep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	verr := call.Finish(&err)
	testForVerror(t, verr, verror.NoAccess)
}

func TestCancelledBeforeFinish(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	ctx, cancel := context.WithCancel(ctx)
	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// Cancel before we call finish.
	cancel()
	verr := call.Finish(&err)
	// TOO(cnicolaou): this should be Cancelled only.
	testForVerror(t, verr, verror.Cancelled, verror.BadProtocol)
}

func TestCancelledDuringFinish(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	ctx, cancel := context.WithCancel(ctx)
	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// Cancel whilst the RPC is running.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	verr := call.Finish(&err)
	// TOO(cnicolaou): this should be Cancelled only.
	testForVerror(t, verr, verror.Cancelled, verror.BadProtocol)
}

func TestRendezvous(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	sh, fn := runMountTable(t, ctx)
	defer fn()

	name := "echoServer"

	// We start the client before we start the server, StartCall will reresolve
	// the name until it finds an entry or timesout after an exponential
	// backoff of some minutes.
	startServer := func() {
		time.Sleep(10 * time.Millisecond)
		srv, _ := sh.Start(core.EchoServerCommand, nil, testArgs("message", name)...)
		s := expect.NewSession(t, srv.Stdout(), time.Minute)
		s.ExpectVar("PID")
		s.ExpectVar("NAME")
	}
	go startServer()

	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Echo", []interface{}{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	response := ""
	verr := call.Finish(&response, &err)
	if verr != nil {
		testForVerror(t, verr, verror.Cancelled)
		return
	}
	if got, want := response, "message: hello\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCallback(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	sh, fn := runMountTable(t, ctx)
	defer fn()

	name, fn := initServer(t, ctx)
	defer fn()

	srv, err := sh.Start("ping", nil, name)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, srv.Stdout(), time.Minute)
	if got, want := s.ExpectVar("RESULT"), "pong"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStreamTimeout(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	want := 10
	ctx, _ = context.WithTimeout(ctx, 300*time.Millisecond)
	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Source", []interface{}{want})
	if err != nil {
		if !verror.Is(err, verror.Timeout.ID) && !verror.Is(err, verror.BadProtocol.ID) {
			t.Fatalf("verror should be a timeout or badprotocol, not %s: stack %s",
				err, err.(verror.E).Stack())
		}
		return
	}

	for {
		got := 0
		err := call.Recv(&got)
		if err == nil {
			if got != want {
				t.Fatalf("got %d, want %d", got, want)
			}
			want++
			continue
		}
		// TOO(cnicolaou): this should be Timeout only.
		testForVerror(t, err, verror.Timeout, verror.BadProtocol)
		break
	}
	verr := call.Finish(&err)
	testForVerror(t, verr, verror.Timeout, verror.BadProtocol)
}

func TestStreamAbort(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	call, err := veyron2.GetClient(ctx).StartCall(ctx, name, "Sink", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := 10
	for i := 0; i <= want; i++ {
		if err := call.Send(i); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}
	call.CloseSend()
	verr := call.Send(100)
	testForVerror(t, verr, verror.Aborted)

	result := 0
	verr = call.Finish(&result, &err)
	if verr != nil {
		t.Fatalf("unexpected error: %s", verr)
	}
	if !verror.Is(err, verror.Unknown.ID) || err.Error() != `v.io/core/veyron2/verror.Unknown:   EOF` {
		t.Errorf("wrong error: %#v", err)
	}
	/* TODO(cnicolaou): use this when verror2/vom transition is done.
	if err != nil && !verror.Is(err, verror.EOF.ID) {
		t.Fatalf("unexpected error: %#v", err)
	}
	*/
	if got := result; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestNoServersAvailable(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	_, fn := runMountTable(t, ctx)
	defer fn()
	name := "noservers"
	ctx, _ = context.WithTimeout(ctx, 300*time.Millisecond)
	call, verr := veyron2.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	if verr != nil {
		testForVerror(t, verr, verror.Timeout, verror.BadProtocol, verror.NoExist)
		return
	}
	err := call.Finish(&verr)
	testForVerror(t, err, verror.Timeout, verror.BadProtocol, verror.NoExist)
}

func TestNoMountTable(t *testing.T) {
	ctx, shutdown := newCtx()
	defer shutdown()
	veyron2.GetNamespace(ctx).SetRoots()
	name := "a_mount_table_entry"

	// If there is no mount table, then we'll get a NoServers error message.
	ctx, _ = context.WithTimeout(ctx, 300*time.Millisecond)
	_, verr := veyron2.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	testForVerror(t, verr, verror.NoServers)
}

// TestReconnect verifies that the client transparently re-establishes the
// connection to the server if the server dies and comes back (on the same
// endpoint).
func TestReconnect(t *testing.T) {
	rootCtx, shutdown := veyron2.Init()
	defer shutdown()

	principal := tsecurity.NewPrincipal("client")
	ctx, err := veyron2.SetPrincipal(rootCtx, principal)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := modules.NewShell(ctx, principal)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	server, err := sh.Start(core.EchoServerCommand, nil, "--", "--veyron.tcp.address=127.0.0.1:0", "mymessage", "")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	session := expect.NewSession(t, server.Stdout(), time.Minute)
	session.ReadLine()
	serverName := session.ExpectVar("NAME")
	serverEP, _ := naming.SplitAddressName(serverName)
	ep, _ := inaming.NewEndpoint(serverEP)

	makeCall := func(ctx *context.T) (string, error) {
		ctx, _ = context.WithDeadline(ctx, time.Now().Add(10*time.Second))
		call, err := veyron2.GetClient(ctx).StartCall(ctx, serverName, "Echo", []interface{}{"bratman"})
		if err != nil {
			return "", fmt.Errorf("START: %s", err)
		}
		var result string
		var rerr error
		if err = call.Finish(&result, &rerr); err != nil {
			return "", err
		}
		return result, nil
	}

	expected := "mymessage: bratman\n"
	if result, err := makeCall(ctx); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
	}
	// Kill the server, verify client can't talk to it anymore.
	sh.SetWaitTimeout(time.Minute)
	if err := server.Shutdown(os.Stderr, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if _, err := makeCall(ctx); err == nil || (!strings.HasPrefix(err.Error(), "START") && !strings.Contains(err.Error(), "EOF")) {
		t.Fatalf(`Got (%v) want ("START: <err>" or "EOF") as server is down`, err)
	}

	// Resurrect the server with the same address, verify client
	// re-establishes the connection. This is racy if another
	// process grabs the port.
	server, err = sh.Start(core.EchoServerCommand, nil, "--", "--veyron.tcp.address="+ep.Address, "mymessage again", "")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	session = expect.NewSession(t, server.Stdout(), time.Minute)
	defer server.Shutdown(os.Stderr, os.Stderr)
	expected = "mymessage again: bratman\n"
	if result, err := makeCall(ctx); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
	}

}

// TODO(cnicolaou:) tests for:
// -- Test for bad discharges error and correct invalidation, client.go:870..880
