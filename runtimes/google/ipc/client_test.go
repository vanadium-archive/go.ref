package ipc_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/modules/core"
	"veyron.io/veyron/veyron/profiles"
)

var r veyron2.Runtime

func init() {
	modules.RegisterChild("ping", "<name>", childPing)
	var err error
	if r, err = rt.New(); err != nil {
		panic(err)
	}

	r.Namespace().CacheCtl(naming.DisableCache(true))
}

func testArgs(args ...string) []string {
	var targs = []string{"--", "--veyron.tcp.address=127.0.0.1:0"}
	return append(targs, args...)
}

func runMountTable(t *testing.T, r veyron2.Runtime) (*modules.Shell, func()) {
	sh, err := modules.NewShell(r.Principal())
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
	if err = r.Namespace().SetRoots(rootName); err != nil {
		t.Fatalf("unexpected error setting namespace roots: %s", err)
	}

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

func numServers(t *testing.T, name string) int {
	servers, err := r.Namespace().Resolve(r.NewContext(), name)
	if err != nil {
		return 0
	}
	return len(servers)
}

// TODO(cnicolaou): figure out how to test and see what the internals
// of tryCall are doing - e.g. using stats counters.
func TestMultipleEndpoints(t *testing.T) {
	sh, fn := runMountTable(t, r)
	defer fn()
	srv, err := sh.Start(core.EchoServerCommand, nil, testArgs("echoServer", "echoServer")...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, srv.Stdout(), time.Minute)
	s.ExpectVar("NAME")

	runClient(t, sh)

	// Create a fake set of 100 entries in the mount table
	ctx := r.NewContext()
	for i := 0; i < 100; i++ {
		// 203.0.113.0 is TEST-NET-3 from RFC5737
		ep := naming.FormatEndpoint("tcp", fmt.Sprintf("203.0.113.%d:443", i))
		n := naming.JoinAddressName(ep, "")
		if err := r.Namespace().Mount(ctx, "echoServer", n, time.Hour); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// Verify that there are 102 entries for echoServer in the mount table.
	if got, want := numServers(t, "echoServer"), 102; got != want {
		t.Fatalf("got: %d, want: %d", got, want)
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
	if got, want := numServers(t, "echoServer"), 100; got != want {
		t.Fatalf("got: %d, want: %d", got, want)
	}
}

func TestTimeoutCall(t *testing.T) {
	client := r.Client()
	ctx, _ := r.NewContext().WithTimeout(100 * time.Millisecond)
	name := naming.JoinAddressName(naming.FormatEndpoint("tcp", "203.0.113.10:443"), "")
	_, err := client.StartCall(ctx, name, "echo", []interface{}{"args don't matter"})
	if !verror.Is(err, verror.Timeout.ID) {
		t.Fatalf("wrong error: %s", err)
	}
}

type sleeper struct {
	done <-chan struct{}
}

func (s *sleeper) Sleep(call ipc.ServerContext) error {
	select {
	case <-s.done:
	case <-time.After(time.Hour):
	}
	return nil
}

func (s *sleeper) Ping(call ipc.ServerContext) (string, error) {
	return "pong", nil
}

func (s *sleeper) Source(call ipc.ServerCall, start int) error {
	i := start
	backoff := 25 * time.Millisecond
	for {
		select {
		case <-s.done:
			return nil
		case <-time.After(backoff):
			call.Send(i)
			i++
		}
		backoff *= 2
	}
}

func (s *sleeper) Sink(call ipc.ServerCall) (int, error) {
	i := 0
	for {
		if err := call.Recv(&i); err != nil {
			return i, verror.Convert(verror.Internal, call, err)
		}
	}
}

func childPing(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	name := args[1]
	call, err := r.Client().StartCall(r.NewContext(), name, "Ping", nil)
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

func initServer(t *testing.T, r veyron2.Runtime) (string, ipc.Server, func()) {
	server, err := r.NewServer()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	done := make(chan struct{})
	deferFn := func() { close(done); server.Stop() }

	ep, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	server.Serve("", &sleeper{done}, nil)
	name := naming.JoinAddressName(ep.String(), "")
	return name, server, deferFn
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
	name, _, fn := initServer(t, r)
	defer fn()
	ctx, _ := r.NewContext().WithTimeout(100 * time.Millisecond)
	call, err := r.Client().StartCall(ctx, name, "Sleep", nil)
	if err != nil {
		testForVerror(t, err, verror.Timeout)
		return
	}
	verr := call.Finish(&err)
	// TODO(cnicolaou): this should be Timeout only.
	testForVerror(t, verr, verror.Timeout, verror.BadProtocol)
}

func TestArgsAndResponses(t *testing.T) {
	name, _, fn := initServer(t, r)
	defer fn()

	call, err := r.Client().StartCall(r.NewContext(), name, "Sleep", []interface{}{"too many args"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	verr := call.Finish(&err)
	testForVerror(t, verr, verror.BadProtocol)

	call, err = r.Client().StartCall(r.NewContext(), name, "Ping", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	pong := ""
	dummy := ""
	verr = call.Finish(&pong, &dummy, &err)
	testForVerror(t, verr, verror.BadProtocol)
}

func TestAccessDenied(t *testing.T) {
	r1, _ := rt.New()
	r2, _ := rt.New()

	// The server and client use different runtimes and hence different
	// principals and without any shared blessings the server will deny
	// access to the client
	name, _, fn := initServer(t, r1)
	defer fn()

	client := r2.Client()
	call, err := client.StartCall(r2.NewContext(), name, "Sleep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	verr := call.Finish(&err)
	testForVerror(t, verr, verror.NoAccess)
}

func TestCancelledBeforeFinish(t *testing.T) {
	name, _, fn := initServer(t, r)
	defer fn()

	ctx, cancel := r.NewContext().WithCancel()
	call, err := r.Client().StartCall(ctx, name, "Sleep", nil)
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
	name, _, fn := initServer(t, r)
	defer fn()

	ctx, cancel := r.NewContext().WithCancel()
	call, err := r.Client().StartCall(ctx, name, "Sleep", nil)
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
	sh, fn := runMountTable(t, r)
	defer fn()

	name := "echoServer"

	// We start the client before we start the server, StartCall will reresolve
	// the name until it finds an entry or timesout after an exponential
	// backoff of some minutes.
	startServer := func() {
		time.Sleep(10 * time.Millisecond)
		srv, _ := sh.Start(core.EchoServerCommand, nil, testArgs("message", name)...)
		s := expect.NewSession(t, srv.Stdout(), time.Minute)
		s.ExpectVar("NAME")
	}
	go startServer()

	call, err := r.Client().StartCall(r.NewContext(), name, "Echo", []interface{}{"hello"})
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
	sh, fn := runMountTable(t, r)
	defer fn()

	name, _, fn := initServer(t, r)
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
	name, _, fn := initServer(t, r)
	defer fn()

	want := 10
	ctx, _ := r.NewContext().WithTimeout(300 * time.Millisecond)
	call, err := r.Client().StartCall(ctx, name, "Source", []interface{}{want})
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
	name, _, fn := initServer(t, r)
	defer fn()

	ctx := r.NewContext()
	call, err := r.Client().StartCall(ctx, name, "Sink", nil)
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
	if !verror.Is(err, "veyron.io/veyron/veyron2/verror.Internal") || err.Error() != `ipc.test:"".Sink: Internal error: EOF` {
		t.Errorf("wrong error: %#v", err)
	}
	if got := result; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestNoServersAvailable(t *testing.T) {
	_, fn := runMountTable(t, r)
	defer fn()
	name := "noservers"
	ctx, _ := r.NewContext().WithTimeout(300 * time.Millisecond)
	call, verr := r.Client().StartCall(ctx, name, "Sleep", nil)
	if verr != nil {
		testForVerror(t, verr, verror.Timeout, verror.BadProtocol, verror.NoExist)
		return
	}
	// The local namespace client may return the 'current' name when it encounters
	// a timeout or other networking error, which means we may end up invoking an
	// RPC on that entry - which in our case means we end up invoking:
	// <mount table endpoint>/noservers.Sleep(). This RPC will fail immediately
	// since we've already reached our timeout, but we can't see that error
	// until we call Finish.
	err := call.Finish(&verr)
	testForVerror(t, err, verror.Timeout, verror.BadProtocol)
}

func TestNoMountTable(t *testing.T) {
	r.Namespace().SetRoots()
	name := "a_mount_table_entry"

	// If there is no mount table, then we'll get a NoServers error message.
	ctx, _ := r.NewContext().WithTimeout(300 * time.Millisecond)
	_, verr := r.Client().StartCall(ctx, name, "Sleep", nil)
	testForVerror(t, verr, verror.NoServers)
}

// TODO(cnicolaou:) tests for:
// -- Test for bad discharges error and correct invalidation, client.go:870..880
