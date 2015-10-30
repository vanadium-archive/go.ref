// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	fmessage "v.io/v23/flow/message"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"

	"v.io/x/ref"
	"v.io/x/ref/internal/logger"
	lsecurity "v.io/x/ref/lib/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/internal/flow/protocols/debug"
	inaming "v.io/x/ref/runtime/internal/naming"
	irpc "v.io/x/ref/runtime/internal/rpc"
	"v.io/x/ref/runtime/internal/rpc/stream/message"
	"v.io/x/ref/runtime/internal/testing/mocks/mocknet"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

//go:generate jiri test generate .

var rootMT = modules.Register(func(env *modules.Env, args ...string) error {
	seclevel := options.SecurityConfidential
	if len(args) == 1 && args[0] == "nosec" {
		seclevel = options.SecurityNone
	}
	return runRootMT(seclevel, env, args...)
}, "rootMT")

func runRootMT(seclevel options.SecurityLevel, env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	if seclevel == options.SecurityNone && ref.RPCTransitionState() >= ref.XServers {
		ls := v23.GetListenSpec(ctx)
		for i := range ls.Addrs {
			ls.Addrs[i].Protocol, ls.Addrs[i].Address = debug.WrapAddress(
				ls.Addrs[i].Protocol, ls.Addrs[i].Address)
		}
		ctx = v23.WithListenSpec(ctx, ls)
	}
	mt, err := mounttablelib.NewMountTableDispatcher(ctx, "", "", "mounttable")
	if err != nil {
		return fmt.Errorf("mounttablelib.NewMountTableDispatcher failed: %s", err)
	}
	ctx, server, err := v23.WithNewDispatchingServer(ctx, "", mt, options.ServesMountTable(true), seclevel)
	if err != nil {
		return fmt.Errorf("root failed: %v", err)
	}
	fmt.Fprintf(env.Stdout, "PID=%d\n", os.Getpid())
	for _, ep := range server.Status().Endpoints {
		fmt.Fprintf(env.Stdout, "MT_NAME=%s\n", ep.Name())
	}
	modules.WaitForEOF(env.Stdin)
	return nil
}

type treeDispatcher struct{ id string }

func (d treeDispatcher) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	return &echoServerObject{d.id, suffix}, nil, nil
}

type echoServerObject struct {
	id, suffix string
}

func (es *echoServerObject) Echo(_ *context.T, _ rpc.ServerCall, m string) (string, error) {
	if len(es.suffix) > 0 {
		return fmt.Sprintf("%s.%s: %s\n", es.id, es.suffix, m), nil
	}
	return fmt.Sprintf("%s: %s\n", es.id, m), nil
}

func (es *echoServerObject) Sleep(_ *context.T, _ rpc.ServerCall, d string) error {
	duration, err := time.ParseDuration(d)
	if err != nil {
		return err
	}
	time.Sleep(duration)
	return nil
}

var echoServer = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	id, mp := args[0], args[1]
	disp := &treeDispatcher{id: id}
	ctx, server, err := v23.WithNewDispatchingServer(ctx, mp, disp)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "PID=%d\n", os.Getpid())
	for _, ep := range server.Status().Endpoints {
		fmt.Fprintf(env.Stdout, "NAME=%s\n", ep.Name())
	}
	modules.WaitForEOF(env.Stdin)
	return nil
}, "echoServer")

var echoClient = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	name := args[0]
	args = args[1:]
	client := v23.GetClient(ctx)
	for _, a := range args {
		var r string
		if err := client.Call(ctx, name, "Echo", []interface{}{a}, []interface{}{&r}); err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, r)
	}
	return nil
}, "echoClient")

func runMountTable(t *testing.T, ctx *context.T, args ...string) (*modules.Shell, func()) {
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	root, err := sh.Start(nil, rootMT, args...)
	if err != nil {
		t.Fatalf("unexpected error for root mt: %s", err)
	}
	deferFn := func() {
		sh.Cleanup(os.Stderr, os.Stderr)
	}

	root.ExpectVar("PID")
	rootName := root.ExpectVar("MT_NAME")
	if len(rootName) == 0 {
		root.Shutdown(nil, os.Stderr)
	}

	sh.SetVar(ref.EnvNamespacePrefix, rootName)
	if err = v23.GetNamespace(ctx).SetRoots(rootName); err != nil {
		t.Fatalf("unexpected error setting namespace roots: %s", err)
	}

	return sh, deferFn
}

func runClient(t *testing.T, sh *modules.Shell) error {
	clt, err := sh.Start(nil, echoClient, "echoServer", "a message")
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

func numServers(t *testing.T, ctx *context.T, name string, expected int) int {
	for {
		me, err := v23.GetNamespace(ctx).Resolve(ctx, name)
		if err == nil && len(me.Servers) == expected {
			return expected
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TODO(cnicolaou): figure out how to test and see what the internals
// of tryCall are doing - e.g. using stats counters.
func TestMultipleEndpoints(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, fn := runMountTable(t, ctx)
	defer fn()
	srv, err := sh.Start(nil, echoServer, "echoServer", "echoServer")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, srv.Stdout(), time.Minute)
	s.ExpectVar("PID")
	s.ExpectVar("NAME")

	// Verify that there are 1 entries for echoServer in the mount table.
	if got, want := numServers(t, ctx, "echoServer", 1), 1; got != want {
		t.Fatalf("got: %d, want: %d", got, want)
	}

	runClient(t, sh)

	// Create a fake set of 100 entries in the mount table
	for i := 0; i < 100; i++ {
		// 203.0.113.0 is TEST-NET-3 from RFC5737
		ep := naming.FormatEndpoint("tcp", fmt.Sprintf("203.0.113.%d:443", i))
		n := naming.JoinAddressName(ep, "")
		if err := v23.GetNamespace(ctx).Mount(ctx, "echoServer", n, time.Hour); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// Verify that there are 101 entries for echoServer in the mount table.
	if got, want := numServers(t, ctx, "echoServer", 101), 101; got != want {
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
	if got, want := numServers(t, ctx, "echoServer", 100), 100; got != want {
		t.Fatalf("got: %d, want: %d", got, want)
	}
}

func TestTimeout(t *testing.T) {
	t.Skip("https://github.com/vanadium/issues/issues/650")

	ctx, shutdown := test.V23Init()
	defer shutdown()
	client := v23.GetClient(ctx)
	ctx, _ = context.WithTimeout(ctx, 100*time.Millisecond)
	name := naming.JoinAddressName(naming.FormatEndpoint("tcp", "203.0.113.10:443"), "")
	_, err := client.StartCall(ctx, name, "echo", []interface{}{"args don't matter"})
	t.Log(err)
	if verror.ErrorID(err) != verror.ErrTimeout.ID {
		t.Fatalf("wrong error: %s", err)
	}
}

func logErrors(t *testing.T, msg string, logerr, logstack, debugString bool, err error) {
	_, file, line, _ := runtime.Caller(2)
	loc := fmt.Sprintf("%s:%d", filepath.Base(file), line)
	if logerr {
		t.Logf("%s: %s: %v", loc, msg, err)
	}
	if logstack {
		t.Logf("%s: %s: %v", loc, msg, verror.Stack(err).String())
	}
	if debugString {
		t.Logf("%s: %s: %v", loc, msg, verror.DebugString(err))
	}
}

func TestStartCallErrors(t *testing.T) {
	// TODO(suharshs,mattr): This test will be enables once the new client is written and solves the flakiness.
	t.Skip("TODO(suharshs,mattr): This test is very flaky and is temporarily disabled.")
	ctx, shutdown := test.V23Init()
	defer shutdown()
	client := v23.GetClient(ctx)

	ns := v23.GetNamespace(ctx)

	logErr := func(msg string, err error) {
		logErrors(t, msg, true, false, false, err)
	}

	emptyCtx := &context.T{}
	emptyCtx = context.WithLogger(emptyCtx, logger.Global())
	_, err := client.StartCall(emptyCtx, "noname", "nomethod", nil)
	if verror.ErrorID(err) != verror.ErrBadArg.ID {
		t.Fatalf("wrong error: %s", err)
	}
	logErr("no context", err)

	// TODO(mattr): This test doesn't pass because xclient doesn't look for this
	// error.
	// p1 := security.PublicKeyAuthorizer(testutil.NewPrincipal().PublicKey())
	// p2 := security.PublicKeyAuthorizer(testutil.NewPrincipal().PublicKey())
	// _, err = client.StartCall(ctx, "noname", "nomethod", nil, options.ServerAuthorizer{p1}, options.ServerAuthorizer{p2})
	// if verror.ErrorID(err) != verror.ErrBadArg.ID {
	// 	t.Fatalf("wrong error: %s", err)
	// }
	// logErr("too many public keys", err)

	// This will fail with NoServers, but because there is no mount table
	// to communicate with. The error message should include a
	// 'connection refused' string.
	ns.SetRoots("/127.0.0.1:8101")
	_, err = client.StartCall(ctx, "noname", "nomethod", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrNoServers.ID {
		t.Fatalf("wrong error: %s", err)
	}
	if want := "connection refused"; !strings.Contains(verror.DebugString(err), want) {
		t.Fatalf("wrong error: %s - doesn't contain %q", err, want)
	}
	logErr("no mount table", err)

	// This will fail with NoServers, but because there really is no
	// name registered with the mount table.
	_, shutdown = runMountTable(t, ctx)
	defer shutdown()
	_, err = client.StartCall(ctx, "noname", "nomethod", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrNoServers.ID {
		t.Fatalf("wrong error: %s", err)
	}
	if unwanted := "connection refused"; strings.Contains(err.Error(), unwanted) {
		t.Fatalf("wrong error: %s - does contain %q", err, unwanted)
	}
	logErr("no name registered", err)

	// The following tests will fail with NoServers, but because there are
	// no protocols that the client and servers (mount table, and "name") share.
	nctx, nclient, err := v23.WithNewClient(ctx, irpc.PreferredProtocols([]string{"wsh"}))

	addr := naming.FormatEndpoint("nope", "127.0.0.1:1081")
	if err := ns.Mount(ctx, "name", addr, time.Minute); err != nil {
		t.Fatal(err)
	}

	// This will fail in its attempt to call ResolveStep to the mount table
	// because we are using both the new context and the new client.
	_, err = nclient.StartCall(nctx, "name", "nomethod", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrNoServers.ID {
		t.Fatalf("wrong error: %s", err)
	}
	if want := "ResolveStep"; !strings.Contains(err.Error(), want) {
		t.Fatalf("wrong error: %s - doesn't contain %q", err, want)
	}
	logErr("mismatched protocols", err)

	// This will fail in its attempt to invoke the actual RPC because
	// we are using the old context (which supplies the context for the calls
	// to ResolveStep) and the new client.
	_, err = nclient.StartCall(ctx, "name", "nomethod", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrNoServers.ID {
		t.Fatalf("wrong error: %s", err)
	}
	if want := "nope"; !strings.Contains(err.Error(), want) {
		t.Fatalf("wrong error: %s - doesn't contain %q", err, want)
	}
	if unwanted := "ResolveStep"; strings.Contains(err.Error(), unwanted) {
		t.Fatalf("wrong error: %s - does contain %q", err, unwanted)

	}
	logErr("mismatched protocols", err)

	// The following two tests will fail due to a timeout.
	ns.SetRoots("/203.0.113.10:8101")
	nctx, _ = context.WithTimeout(ctx, 100*time.Millisecond)
	// This call will timeout talking to the mount table.
	call, err := client.StartCall(nctx, "name", "noname", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrTimeout.ID {
		t.Fatalf("wrong error: %s", err)
	}
	if call != nil {
		t.Fatalf("expected call to be nil")
	}
	logErr("timeout to mount table", err)

	// TODO(mattr): This test is temporarily disabled.  It's very flaky
	// and we're about to move to a new version of the client.  We will
	// fix it in the new client.
	// This, second test, will fail due a timeout contacting the server itself.
	// addr = naming.FormatEndpoint("tcp", "203.0.113.10:8101")

	// nctx, _ = context.WithTimeout(ctx, 100*time.Millisecond)
	// new_name := naming.JoinAddressName(addr, "")
	// call, err = client.StartCall(nctx, new_name, "noname", nil, options.NoRetry{})
	// if verror.ErrorID(err) != verror.ErrTimeout.ID {
	// 	t.Fatalf("wrong error: %s", err)
	// }
	// if call != nil {
	// 	t.Fatalf("expected call to be nil")
	// }
	// logErr("timeout to server", err)
}

func dropDataDialer(ctx *context.T, network, address string, timeout time.Duration) (net.Conn, error) {
	matcher := func(read bool, msg message.T) bool {
		// Drop and close the connection when reading the first data message.
		if _, ok := msg.(*message.Data); ok && read {
			return true
		}
		return false
	}
	opts := mocknet.Opts{
		Mode:              mocknet.V23CloseAtMessage,
		V23MessageMatcher: matcher,
	}
	return mocknet.DialerWithOpts(opts, network, address, timeout)
}

func simpleResolver(ctx *context.T, network, address string) (string, string, error) {
	return network, address, nil
}

func TestStartCallBadProtocol(t *testing.T) {
	if ref.RPCTransitionState() >= ref.XServers {
		t.Skip("This version of the test only runs under the old rpc system.")
	}
	ctx, shutdown := test.V23Init()
	defer shutdown()

	client := v23.GetClient(ctx)

	logErr := func(msg string, err error) {
		logErrors(t, msg, true, false, false, err)
	}

	ns := v23.GetNamespace(ctx)
	simpleListen := func(ctx *context.T, protocol, address string) (net.Listener, error) {
		return net.Listen(protocol, address)
	}
	rpc.RegisterProtocol("dropData", dropDataDialer, simpleResolver, simpleListen)
	// The following test will fail due to a broken connection.
	// We need to run mount table and servers with no security to use
	// the V23CloseAtMessage net.Conn mock.
	_, shutdown = runMountTable(t, ctx, "nosec")
	defer shutdown()
	roots := ns.Roots()
	brkRoot, err := mocknet.RewriteEndpointProtocol(roots[0], "dropData")
	if err != nil {
		t.Fatal(err)
	}
	ns.SetRoots(brkRoot.Name())
	nctx, _ := context.WithTimeout(ctx, 5*time.Second)
	call, err := client.StartCall(nctx, "name", "noname", nil, options.NoRetry{}, options.SecurityNone)
	if verror.ErrorID(err) != verror.ErrNoServers.ID {
		t.Errorf("wrong error: %s", verror.DebugString(err))
	}
	if call != nil {
		t.Errorf("expected call to be nil")
	}
	logErr("broken connection", err)

	// The following test will fail with because the client will set up
	// a secure connection to a server that isn't expecting one.
	nctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	name, fn := initServer(t, ctx, options.SecurityNone)
	defer fn()
	call, err = client.StartCall(nctx, name, "noname", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrBadProtocol.ID {
		t.Errorf("wrong error: %s", err)
	}
	if call != nil {
		t.Errorf("expected call to be nil")
	}
	logErr("insecure server", err)

	// This is the inverse, secure server, insecure client
	name, fn = initServer(t, ctx)
	defer fn()
	call, err = client.StartCall(nctx, name, "noname", nil, options.NoRetry{}, options.SecurityNone)
	if verror.ErrorID(err) != verror.ErrBadProtocol.ID {
		t.Errorf("wrong error: %s", err)
	}
	if call != nil {
		t.Errorf("expected call to be nil")
	}
	logErr("insecure client", err)
}

type closeConn struct {
	ctx *context.T
	flow.Conn
	closed chan struct{}
}

func (c *closeConn) ReadMsg() ([]byte, error) {
	buf, err := c.Conn.ReadMsg()
	if err == nil {
		if m, err := fmessage.Read(c.ctx, buf); err == nil {
			if _, ok := m.(*fmessage.Data); ok {
				close(c.closed)
				c.Conn.Close()
				return nil, io.EOF
			}
		}
	}
	return buf, err
}

func TestStartCallBadProtocol_NewRPC(t *testing.T) {
	if ref.RPCTransitionState() < ref.XServers {
		t.Skip("This version of the test only runs under the new RPC system")
	}
	ctx, shutdown := test.V23Init()
	defer shutdown()

	client := v23.GetClient(ctx)

	logErr := func(msg string, err error) {
		logErrors(t, msg, true, false, false, err)
	}

	ns := v23.GetNamespace(ctx)
	// The following test will fail due to a broken connection.
	// We need to run mount table and servers with no security to use
	// the V23CloseAtMessage net.Conn mock.
	_, shutdown = runMountTable(t, ctx, "nosec")
	defer shutdown()
	ns.SetRoots(debug.WrapName(ns.Roots()[0]))
	ch := make(chan struct{})
	nctx := debug.WithFilter(ctx, func(c flow.Conn) flow.Conn {
		return &closeConn{ctx, c, ch}
	})
	call, err := client.StartCall(nctx, "name", "noname", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrNoServers.ID {
		t.Errorf("wrong error: %s", verror.DebugString(err))
	}
	if call != nil {
		t.Errorf("expected call to be nil")
	}
	logErr("broken connection", err)

	// Make sure we failed because we really did close the connection
	// with our filter
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Errorf("timeout waiting for chan")
	}
}

func TestStartCallSecurity(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	client := v23.GetClient(ctx)

	logErr := func(msg string, err error) {
		logErrors(t, msg, true, false, false, err)
	}

	name, fn := initServer(t, ctx)
	defer fn()

	// Create a context with a new principal that doesn't match the server,
	// so that the client will not trust the server.
	ctx1, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("test-blessing"))
	if err != nil {
		t.Fatal(err)
	}
	call, err := client.StartCall(ctx1, name, "noname", nil, options.NoRetry{})
	if verror.ErrorID(err) != verror.ErrNotTrusted.ID {
		t.Fatalf("wrong error: %s", err)
	}
	if call != nil {
		t.Fatalf("expected call to be nil")
	}
	logErr("client does not trust server", err)
}

var childPing = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	name := args[0]
	got := ""
	if err := v23.GetClient(ctx).Call(ctx, name, "Ping", nil, []interface{}{&got}); err != nil {
		return fmt.Errorf("unexpected error: %s", err)
	}
	fmt.Fprintf(env.Stdout, "RESULT=%s\n", got)
	return nil
}, "childPing")

func initServer(t *testing.T, ctx *context.T, opts ...rpc.ServerOpt) (string, func()) {
	done := make(chan struct{})
	ctx, server, err := v23.WithNewServer(ctx, "", &simple{done}, nil, opts...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	return server.Status().Endpoints[0].Name(), func() { close(done) }
}

func TestTimeoutResponse(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	ctx, cancel := context.WithTimeout(ctx, time.Millisecond)
	err := v23.GetClient(ctx).Call(ctx, name, "Sleep", nil, nil)
	if got, want := verror.ErrorID(err), verror.ErrTimeout.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	cancel()
}

func TestArgsAndResponses(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	call, err := v23.GetClient(ctx).StartCall(ctx, name, "Sleep", []interface{}{"too many args"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	err = call.Finish()
	if got, want := verror.ErrorID(err), verror.ErrBadProtocol.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	call, err = v23.GetClient(ctx).StartCall(ctx, name, "Ping", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	pong := ""
	dummy := ""
	err = call.Finish(&pong, &dummy)
	if got, want := verror.ErrorID(err), verror.ErrBadProtocol.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAccessDenied(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	name, fn := initServer(t, ctx)
	defer fn()

	ctx1, err := v23.WithPrincipal(ctx, testutil.NewPrincipal("test-blessing"))
	// Client must recognize the server, otherwise it won't even send the request.
	security.AddToRoots(v23.GetPrincipal(ctx1), v23.GetPrincipal(ctx).BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}
	call, err := v23.GetClient(ctx1).StartCall(ctx1, name, "Sleep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	err = call.Finish()
	if got, want := verror.ErrorID(err), verror.ErrNoAccess.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCanceledBeforeFinish(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	ctx, cancel := context.WithCancel(ctx)
	call, err := v23.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// Cancel before we call finish.
	cancel()
	err = call.Finish()
	if got, want := verror.ErrorID(err), verror.ErrCanceled.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCanceledDuringFinish(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	ctx, cancel := context.WithCancel(ctx)
	call, err := v23.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// Cancel whilst the RPC is running.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	err = call.Finish()
	if got, want := verror.ErrorID(err), verror.ErrCanceled.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRendezvous(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	sh, fn := runMountTable(t, ctx)
	defer fn()

	name := "echoServer"

	// We start the client before we start the server, StartCall will reresolve
	// the name until it finds an entry or times out after an exponential
	// backoff of some minutes.
	startServer := func() {
		time.Sleep(100 * time.Millisecond)
		srv, _ := sh.Start(nil, echoServer, "message", name)
		s := expect.NewSession(t, srv.Stdout(), time.Minute)
		s.ExpectVar("PID")
		s.ExpectVar("NAME")
	}
	go startServer()

	call, err := v23.GetClient(ctx).StartCall(ctx, name, "Echo", []interface{}{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	response := ""
	if err := call.Finish(&response); err != nil {
		if got, want := verror.ErrorID(err), verror.ErrCanceled.ID; got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if got, want := response, "message: hello\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCallback(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	sh, fn := runMountTable(t, ctx)
	defer fn()

	name, fn := initServer(t, ctx)
	defer fn()

	srv, err := sh.Start(nil, childPing, name)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, srv.Stdout(), time.Minute)
	if got, want := s.ExpectVar("RESULT"), "pong"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStreamTimeout(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	want := 10
	ctx, _ = context.WithTimeout(ctx, 300*time.Millisecond)
	call, err := v23.GetClient(ctx).StartCall(ctx, name, "Source", []interface{}{want})
	if err != nil {
		if verror.ErrorID(err) != verror.ErrTimeout.ID {
			t.Fatalf("verror should be a timeout not %s: stack %s",
				err, verror.Stack(err))
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
		if got, want := verror.ErrorID(err), verror.ErrTimeout.ID; got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
		break
	}
	err = call.Finish()
	if got, want := verror.ErrorID(err), verror.ErrTimeout.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestStreamAbort(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	name, fn := initServer(t, ctx)
	defer fn()

	call, err := v23.GetClient(ctx).StartCall(ctx, name, "Sink", nil)
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
	err = call.Send(100)
	if got, want := verror.ErrorID(err), verror.ErrAborted.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	result := 0
	err = call.Finish(&result)
	if err != nil {
		t.Errorf("unexpected error: %#v", err)
	}
	if got := result; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestNoServersAvailable(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	_, fn := runMountTable(t, ctx)
	defer fn()
	name := "noservers"
	call, err := v23.GetClient(ctx).StartCall(ctx, name, "Sleep", nil, options.NoRetry{})
	if err != nil {
		if got, want := verror.ErrorID(err), verror.ErrNoServers.ID; got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
		return
	}
	err = call.Finish()
	if got, want := verror.ErrorID(err), verror.ErrNoServers.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

}

func TestNoMountTable(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	v23.GetNamespace(ctx).SetRoots()
	name := "a_mount_table_entry"

	// If there is no mount table, then we'll get a NoServers error message.
	ctx, _ = context.WithTimeout(ctx, 300*time.Millisecond)
	_, err := v23.GetClient(ctx).StartCall(ctx, name, "Sleep", nil)
	if got, want := verror.ErrorID(err), verror.ErrNoServers.ID; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestReconnect verifies that the client transparently re-establishes the
// connection to the server if the server dies and comes back (on the same
// endpoint).
func TestReconnect(t *testing.T) {
	t.Skip()
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, v23.GetPrincipal(ctx), testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	server, err := sh.Start(nil, echoServer, "--v23.tcp.address=127.0.0.1:0", "mymessage", "")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	server.ReadLine()
	serverName := server.ExpectVar("NAME")
	serverEP, _ := naming.SplitAddressName(serverName)
	ep, _ := inaming.NewEndpoint(serverEP)

	makeCall := func(ctx *context.T, opts ...rpc.CallOpt) (string, error) {
		ctx, _ = context.WithDeadline(ctx, time.Now().Add(10*time.Second))
		call, err := v23.GetClient(ctx).StartCall(ctx, serverName, "Echo", []interface{}{"bratman"}, opts...)
		if err != nil {
			return "", fmt.Errorf("START: %s", err)
		}
		var result string
		if err := call.Finish(&result); err != nil {
			return "", err
		}
		return result, nil
	}

	expected := "mymessage: bratman\n"
	if result, err := makeCall(ctx); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
	}
	// Kill the server, verify client can't talk to it anymore.
	if err := server.Shutdown(os.Stderr, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if _, err := makeCall(ctx, options.NoRetry{}); err == nil || (!strings.HasPrefix(err.Error(), "START") && !strings.Contains(err.Error(), "EOF")) {
		t.Fatalf(`Got (%v) want ("START: <err>" or "EOF") as server is down`, err)
	}

	// Resurrect the server with the same address, verify client
	// re-establishes the connection. This is racy if another
	// process grabs the port.
	server, err = sh.Start(nil, echoServer, "--v23.tcp.address="+ep.Address, "mymessage again", "")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer server.Shutdown(os.Stderr, os.Stderr)
	expected = "mymessage again: bratman\n"
	if result, err := makeCall(ctx); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
	}
}

func TestMethodErrors(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	clt := v23.GetClient(ctx)

	name, fn := initServer(t, ctx)
	defer fn()

	var (
		i, j int
		s    string
	)

	testCases := []struct {
		testName, objectName, method string
		args, results                []interface{}
		wantID                       verror.ID
		wantMessage                  string
	}{
		{
			testName:   "unknown method",
			objectName: name,
			method:     "NoMethod",
			wantID:     verror.ErrUnknownMethod.ID,
		},
		{
			testName:   "unknown suffix",
			objectName: name + "/NoSuffix",
			method:     "Ping",
			wantID:     verror.ErrUnknownSuffix.ID,
		},
		{
			testName:    "too many args",
			objectName:  name,
			method:      "Ping",
			args:        []interface{}{1, 2},
			results:     []interface{}{&i},
			wantID:      verror.ErrBadProtocol.ID,
			wantMessage: "wrong number of input arguments",
		},
		{
			testName:    "wrong number of results",
			objectName:  name,
			method:      "Ping",
			results:     []interface{}{&i, &j},
			wantID:      verror.ErrBadProtocol.ID,
			wantMessage: "results, but want",
		},
		{
			testName:    "wrong number of results",
			objectName:  name,
			method:      "Ping",
			results:     []interface{}{&i, &j},
			wantID:      verror.ErrBadProtocol.ID,
			wantMessage: "results, but want",
		},
		{
			testName:    "mismatched arg types",
			objectName:  name,
			method:      "Echo",
			args:        []interface{}{1},
			results:     []interface{}{&s},
			wantID:      verror.ErrBadProtocol.ID,
			wantMessage: "aren't compatible",
		},
		{
			testName:    "mismatched result types",
			objectName:  name,
			method:      "Ping",
			results:     []interface{}{&i},
			wantID:      verror.ErrBadProtocol.ID,
			wantMessage: "aren't compatible",
		},
	}

	for _, test := range testCases {
		testPrefix := fmt.Sprintf("test(%s) failed", test.testName)
		call, err := clt.StartCall(ctx, test.objectName, test.method, test.args)
		if err != nil {
			t.Fatalf("%s: %v", testPrefix, err)
		}
		verr := call.Finish(test.results...)
		if verror.ErrorID(verr) != test.wantID {
			t.Errorf("%s: wrong error: %v", testPrefix, verr)
		} else if got, want := verr.Error(), test.wantMessage; !strings.Contains(got, want) {
			t.Errorf("%s: want %q to contain %q", testPrefix, got, want)
		}
		logErrors(t, test.testName, false, false, false, verr)
	}
}

func TestReservedMethodErrors(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	clt := v23.GetClient(ctx)

	name, fn := initServer(t, ctx)
	defer fn()

	logErr := func(msg string, err error) {
		logErrors(t, msg, true, false, false, err)
	}

	// This call will fail because the __xx suffix is not supported by
	// the dispatcher implementing Signature.
	call, err := clt.StartCall(ctx, name+"/__xx", "__Signature", nil)
	if err != nil {
		t.Fatal(err)
	}
	sig := []signature.Interface{}
	verr := call.Finish(&sig)
	if verror.ErrorID(verr) != verror.ErrUnknownSuffix.ID {
		t.Fatalf("wrong error: %s", verr)
	}
	logErr("unknown suffix", verr)

	// This call will fail for the same reason, but with a different error,
	// saying that MethodSignature is an unknown method.
	call, err = clt.StartCall(ctx, name+"/__xx", "__MethodSignature", []interface{}{"dummy"})
	if err != nil {
		t.Fatal(err)
	}
	verr = call.Finish(&sig)
	if verror.ErrorID(verr) != verror.ErrUnknownMethod.ID {
		t.Fatalf("wrong error: %s", verr)
	}
	logErr("unknown method", verr)
}

type publicKeyAuth struct {
	pkey security.PublicKey
}

func (a *publicKeyAuth) Authorize(ctx *context.T, call security.Call) error {
	pkey := call.RemoteBlessings().PublicKey()
	if !reflect.DeepEqual(a.pkey, pkey) {
		return fmt.Errorf("public key mismatch: %v != %v", a.pkey, pkey)
	}
	return nil
}

func TestAllowEmptyBlessings(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	p, err := lsecurity.NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	clientCtx, err := v23.WithPrincipal(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	clt := v23.GetClient(clientCtx)

	_, server, err := v23.WithNewServer(ctx, "", &simple{}, &publicKeyAuth{p.PublicKey()})
	if err != nil {
		t.Fatal(err)
	}
	name := server.Status().Endpoints[0].Name()

	call, err := clt.StartCall(clientCtx, name, "Ping", []interface{}{}, options.ServerAuthorizer{security.AllowEveryone()})
	if err != nil {
		t.Fatal(err)
	}
	var pong string
	if err := call.Finish(&pong); err != nil {
		t.Fatal(err)
	}
}
