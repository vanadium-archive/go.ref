package ipc

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"v.io/core/veyron/runtimes/google/ipc/stream"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/uniqueid"
	"v.io/core/veyron2/vdl"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/lib/stats"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	imanager "v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	"v.io/core/veyron/runtimes/google/lib/publisher"
	inaming "v.io/core/veyron/runtimes/google/naming"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
)

var (
	errMethod     = verror.Make(verror.Aborted, nil)
	clock         = new(fakeClock)
	listenAddrs   = ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}
	listenWSAddrs = ipc.ListenAddrs{{"ws", "127.0.0.1:0"}, {"tcp", "127.0.0.1:0"}}
	listenSpec    = ipc.ListenSpec{Addrs: listenAddrs}
	listenWSSpec  = ipc.ListenSpec{Addrs: listenWSAddrs}
)

type fakeClock struct {
	sync.Mutex
	time int
}

func (c *fakeClock) Now() int {
	c.Lock()
	defer c.Unlock()
	return c.time
}

func (c *fakeClock) Advance(steps uint) {
	c.Lock()
	c.time += int(steps)
	c.Unlock()
}

// We need a special way to create contexts for tests.  We
// can't create a real runtime in the runtime implementation
// so we use a fake one that panics if used.  The runtime
// implementation should not ever use the Runtime from a context.
func testContext() *context.T {
	ctx, _ := context.WithTimeout(testContextWithoutDeadline(), 20*time.Second)
	return ctx
}

func testContextWithoutDeadline() *context.T {
	ctx, _ := context.RootContext()
	ctx, err := ivtrace.Init(ctx, flags.VtraceFlags{})
	if err != nil {
		panic(err)
	}
	ctx, _ = vtrace.SetNewTrace(ctx)
	return ctx
}

func testInternalNewServer(ctx *context.T, streamMgr stream.Manager, ns naming.Namespace, opts ...ipc.ServerOpt) (ipc.Server, error) {
	client, err := InternalNewClient(streamMgr, ns)
	if err != nil {
		return nil, err
	}
	return InternalNewServer(ctx, streamMgr, ns, client, opts...)
}

type userType string

type testServer struct{}

func (*testServer) Closure(ctx ipc.ServerContext) {
}

func (*testServer) Error(ctx ipc.ServerContext) error {
	return errMethod
}

func (*testServer) Echo(ctx ipc.ServerContext, arg string) string {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", ctx.Method(), ctx.Suffix(), arg)
}

func (*testServer) EchoUser(ctx ipc.ServerContext, arg string, u userType) (string, userType) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", ctx.Method(), ctx.Suffix(), arg), u
}

func (*testServer) EchoBlessings(ctx ipc.ServerContext) (server, client string) {
	return fmt.Sprintf("%v", ctx.LocalBlessings().ForContext(ctx)), fmt.Sprintf("%v", ctx.RemoteBlessings().ForContext(ctx))
}

func (*testServer) EchoGrantedBlessings(ctx ipc.ServerContext, arg string) (result, blessing string) {
	return arg, fmt.Sprintf("%v", ctx.Blessings())
}

func (*testServer) EchoAndError(ctx ipc.ServerContext, arg string) (string, error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", ctx.Method(), ctx.Suffix(), arg)
	if arg == "error" {
		return result, errMethod
	}
	return result, nil
}

func (*testServer) Stream(call ipc.ServerCall, arg string) (string, error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", call.Method(), call.Suffix(), arg)
	var u userType
	var err error
	for err = call.Recv(&u); err == nil; err = call.Recv(&u) {
		result += " " + string(u)
		if err := call.Send(u); err != nil {
			return "", err
		}
	}
	if err == io.EOF {
		err = nil
	}
	return result, err
}

func (*testServer) Unauthorized(ipc.ServerCall) (string, error) {
	return "UnauthorizedResult", fmt.Errorf("Unauthorized should never be called")
}

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(c security.Context) error {
	if c.Method() != "Unauthorized" {
		return nil
	}
	return fmt.Errorf("testServerAuthorizer denied access")
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	// If suffix is "nilAuth" we use default authorization, if it is "aclAuth" we
	// use an ACL based authorizer, and otherwise we use the custom testServerAuthorizer.
	var authorizer security.Authorizer
	switch suffix {
	case "discharger":
		return &dischargeServer{}, testServerAuthorizer{}, nil
	case "nilAuth":
		authorizer = nil
	case "aclAuth":
		authorizer = &access.ACL{
			In: []security.BlessingPattern{"client", "server"},
		}
	default:
		authorizer = testServerAuthorizer{}
	}
	return t.server, authorizer, nil
}

type dischargeServer struct{}

func (*dischargeServer) Discharge(ctx ipc.ServerCall, cav security.Caveat, _ security.DischargeImpetus) (vdl.AnyRep, error) {
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return nil, fmt.Errorf("discharger: %v does not represent a third-party caveat", cav)
	}
	if err := tp.Dischargeable(ctx); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", cav, err)
	}
	// Add a fakeTimeCaveat to be able to control discharge expiration via 'clock'.
	expiry, err := security.NewCaveat(fakeTimeCaveat, clock.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to create an expiration on the discharge: %v", err)
	}
	return ctx.LocalPrincipal().MintDischarge(cav, expiry)
}

func startServer(t *testing.T, principal security.Principal, sm stream.Manager, ns naming.Namespace, name string, disp ipc.Dispatcher, opts ...ipc.ServerOpt) (naming.Endpoint, ipc.Server) {
	return startServerWS(t, principal, sm, ns, name, disp, noWebsocket, opts...)
}

func endpointsToStrings(eps []naming.Endpoint) []string {
	r := make([]string, len(eps))
	for i, e := range eps {
		r[i] = e.String()
	}
	sort.Strings(r)
	return r
}

func startServerWS(t *testing.T, principal security.Principal, sm stream.Manager, ns naming.Namespace, name string, disp ipc.Dispatcher, shouldUseWebsocket websocketMode, opts ...ipc.ServerOpt) (naming.Endpoint, ipc.Server) {
	vlog.VI(1).Info("InternalNewServer")
	opts = append(opts, vc.LocalPrincipal{principal})
	ctx := testContext()
	server, err := testInternalNewServer(ctx, sm, ns, opts...)
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	vlog.VI(1).Info("server.Listen")
	spec := listenSpec
	if shouldUseWebsocket {
		spec = listenWSSpec
	}
	eps, err := server.Listen(spec)
	if err != nil {
		t.Errorf("server.Listen failed: %v", err)
	}
	vlog.VI(1).Info("server.Serve")
	if err := server.ServeDispatcher(name, disp); err != nil {
		t.Errorf("server.ServeDispatcher failed: %v", err)
	}
	if err := server.AddName(name); err != nil {
		t.Errorf("server.AddName for discharger failed: %v", err)
	}

	status := server.Status()
	if got, want := endpointsToStrings(status.Endpoints), endpointsToStrings(eps); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	names := status.Mounts.Names()
	if len(names) != 1 || names[0] != name {
		t.Fatalf("unexpected names: %v", names)
	}
	return eps[0], server
}

func loc(d int) string {
	_, file, line, _ := runtime.Caller(d + 1)
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

func verifyMount(t *testing.T, ns naming.Namespace, name string) []string {
	me, err := ns.Resolve(testContext(), name)
	if err != nil {
		t.Errorf("%s: %s not found in mounttable", loc(1), name)
		return nil
	}
	return me.Names()
}

func verifyMountMissing(t *testing.T, ns naming.Namespace, name string) {
	if me, err := ns.Resolve(testContext(), name); err == nil {
		names := me.Names()
		t.Errorf("%s: %s not supposed to be found in mounttable; got %d servers instead: %v", loc(1), name, len(names), names)
	}
}

func stopServer(t *testing.T, server ipc.Server, ns naming.Namespace, name string) {
	vlog.VI(1).Info("server.Stop")
	new_name := "should_appear_in_mt/server"
	verifyMount(t, ns, name)

	// publish a second name
	if err := server.AddName(new_name); err != nil {
		t.Errorf("server.Serve failed: %v", err)
	}
	verifyMount(t, ns, new_name)

	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}

	verifyMountMissing(t, ns, name)
	verifyMountMissing(t, ns, new_name)

	// Check that we can no longer serve after Stop.
	err := server.AddName("name doesn't matter")
	if err == nil || !verror.Is(err, verror.BadState.ID) {
		t.Errorf("either no error, or a wrong error was returned: %v", err)
	}
	vlog.VI(1).Info("server.Stop DONE")
}

// fakeWSName creates a name containing a endpoint address that forces
// the use of websockets. It does so by resolving the original name
// and choosing the 'ws' endpoint from the set of endpoints returned.
// It must return a name since it'll be passed to StartCall.
func fakeWSName(ns naming.Namespace, name string) (string, error) {
	// Find the ws endpoint and use that.
	me, err := ns.Resolve(testContext(), name)
	if err != nil {
		return "", err
	}
	names := me.Names()
	for _, s := range names {
		if strings.Index(s, "@ws@") != -1 {
			return s, nil
		}
	}
	return "", fmt.Errorf("No ws endpoint found %v", names)
}

type bundle struct {
	client ipc.Client
	server ipc.Server
	ep     naming.Endpoint
	ns     naming.Namespace
	sm     stream.Manager
	name   string
}

func (b bundle) cleanup(t *testing.T) {
	if b.server != nil {
		stopServer(t, b.server, b.ns, b.name)
	}
	if b.client != nil {
		b.client.Close()
	}
}

func createBundle(t *testing.T, client, server security.Principal, ts interface{}) (b bundle) {
	return createBundleWS(t, client, server, ts, noWebsocket)
}

func createBundleWS(t *testing.T, client, server security.Principal, ts interface{}, shouldUseWebsocket websocketMode) (b bundle) {
	b.sm = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	b.ns = tnaming.NewSimpleNamespace()
	b.name = "mountpoint/server"
	if server != nil {
		b.ep, b.server = startServerWS(t, server, b.sm, b.ns, b.name, testServerDisp{ts}, shouldUseWebsocket)
	}
	if client != nil {
		var err error
		if b.client, err = InternalNewClient(b.sm, b.ns, vc.LocalPrincipal{client}); err != nil {
			t.Fatalf("InternalNewClient failed: %v", err)
		}
	}
	return
}

func matchesErrorPattern(err error, id verror.IDAction, pattern string) bool {
	if len(pattern) > 0 && err != nil && strings.Index(err.Error(), pattern) < 0 {
		return false
	}
	if err == nil && id.ID == "" {
		return true
	}
	return verror.Is(err, id.ID)
}

func TestMultipleCallsToServeAndName(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	ctx := testContext()
	server, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{tsecurity.NewPrincipal()})
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	_, err = server.Listen(listenSpec)
	if err != nil {
		t.Errorf("server.Listen failed: %v", err)
	}

	disp := &testServerDisp{&testServer{}}
	if err := server.ServeDispatcher("mountpoint/server", disp); err != nil {
		t.Errorf("server.ServeDispatcher failed: %v", err)
	}

	n1 := "mountpoint/server"
	n2 := "should_appear_in_mt/server"
	n3 := "should_appear_in_mt/server"
	n4 := "should_not_appear_in_mt/server"

	verifyMount(t, ns, n1)

	if server.ServeDispatcher(n2, disp) == nil {
		t.Errorf("server.ServeDispatcher should have failed")
	}

	if err := server.Serve(n2, &testServer{}, nil); err == nil {
		t.Errorf("server.Serve should have failed")
	}

	if err := server.AddName(n3); err != nil {
		t.Errorf("server.AddName failed: %v", err)
	}

	if err := server.AddName(n3); err != nil {
		t.Errorf("server.AddName failed: %v", err)
	}
	verifyMount(t, ns, n2)
	verifyMount(t, ns, n3)

	server.RemoveName(n1)
	verifyMountMissing(t, ns, n1)

	server.RemoveName("some randome name")

	if err := server.ServeDispatcher(n4, &testServerDisp{&testServer{}}); err == nil {
		t.Errorf("server.ServeDispatcher should have failed")
	}
	verifyMountMissing(t, ns, n4)

	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}

	verifyMountMissing(t, ns, n1)
	verifyMountMissing(t, ns, n2)
	verifyMountMissing(t, ns, n3)
}

func TestRPCServerAuthorization(t *testing.T) {
	const (
		vcErr      = "VC handshake failed"
		nameErr    = "do not match pattern"
		allowedErr = "set of allowed servers"
	)
	var (
		pprovider, pclient, pserver = tsecurity.NewPrincipal("root"), tsecurity.NewPrincipal(), tsecurity.NewPrincipal()
		pdischarger                 = pprovider
		now                         = time.Now()
		noErrID                     verror.IDAction

		// Third-party caveats on blessings presented by server.
		cavTPValid   = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/dischargeserver", mkCaveat(security.ExpiryCaveat(now.Add(24*time.Hour))))
		cavTPExpired = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/dischargeserver", mkCaveat(security.ExpiryCaveat(now.Add(-1*time.Second))))

		// Server blessings.
		bServer          = bless(pprovider, pserver, "server")
		bServerExpired   = bless(pprovider, pserver, "server", mkCaveat(security.ExpiryCaveat(time.Now().Add(-1*time.Second))))
		bServerTPValid   = bless(pprovider, pserver, "serverWithTPCaveats", cavTPValid)
		bServerTPExpired = bless(pprovider, pserver, "serverWithTPCaveats", cavTPExpired)
		bTwoBlessings, _ = security.UnionOfBlessings(bServer, bServerTPValid)

		mgr   = imanager.InternalNew(naming.FixedRoutingID(0x1111111))
		ns    = tnaming.NewSimpleNamespace()
		tests = []struct {
			server  security.Blessings           // blessings presented by the server to the client.
			name    string                       // name provided by the client to StartCall
			allowed options.AllowedServersPolicy // option provided to StartCall.
			errID   verror.IDAction
			err     string
		}{
			// Client accepts talking to the server only if the
			// server's blessings match the provided pattern
			{bServer, "mountpoint/server", nil, noErrID, ""},
			{bServer, "[root/server]mountpoint/server", nil, noErrID, ""},
			{bServer, "[root/otherserver]mountpoint/server", nil, verror.NotTrusted, nameErr},
			{bServer, "[otherroot/server]mountpoint/server", nil, verror.NotTrusted, nameErr},

			// and, if the server's blessing has third-party
			// caveats then the server provides appropriate
			// discharges.
			{bServerTPValid, "mountpoint/server", nil, noErrID, ""},
			{bServerTPValid, "[root/serverWithTPCaveats]mountpoint/server", nil, noErrID, ""},
			{bServerTPValid, "[root/otherserver]mountpoint/server", nil, verror.NotTrusted, nameErr},
			{bServerTPValid, "[otherroot/server]mountpoint/server", nil, verror.NotTrusted, nameErr},

			// Client does not talk to a server that presents
			// expired blessings (because the blessing store is
			// configured to only talk to peers with blessings matching
			// the pattern "root").
			{bServerExpired, "mountpoint/server", nil, verror.NotTrusted, vcErr},

			// Client does not talk to a server that fails to
			// provide discharges for third-party caveats on the
			// blessings presented by it.
			{bServerTPExpired, "mountpoint/server", nil, verror.NotTrusted, vcErr},

			// Testing the AllowedServersPolicy option.
			{bServer, "mountpoint/server", options.AllowedServersPolicy{"otherroot"}, verror.NotTrusted, allowedErr},
			{bServer, "[root/server]mountpoint/server", options.AllowedServersPolicy{"otherroot"}, verror.NotTrusted, allowedErr},
			{bServer, "[otherroot/server]mountpoint/server", options.AllowedServersPolicy{"root/server"}, verror.NotTrusted, nameErr},
			{bServer, "[root/server]mountpoint/server", options.AllowedServersPolicy{"root"}, noErrID, ""},
			// Server presents two blessings: One that satisfies
			// the pattern provided to StartCall and one that
			// satisfies the AllowedServersPolicy, so the server is
			// authorized.
			{bTwoBlessings, "[root/serverWithTPCaveats]mountpoint/server", options.AllowedServersPolicy{"root/server"}, noErrID, ""},
		}
	)

	_, server := startServer(t, pserver, mgr, ns, "mountpoint/server", testServerDisp{&testServer{}})
	defer stopServer(t, server, ns, "mountpoint/server")

	// Start the discharge server.
	_, dischargeServer := startServer(t, pdischarger, mgr, ns, "mountpoint/dischargeserver", testutil.LeafDispatcher(&dischargeServer{}, &acceptAllAuthorizer{}))
	defer stopServer(t, dischargeServer, ns, "mountpoint/dischargeserver")

	// Make the client and server principals trust root certificates from
	// pprovider
	pclient.AddToRoots(pprovider.BlessingStore().Default())
	pserver.AddToRoots(pprovider.BlessingStore().Default())
	// Set a blessing that the client is willing to share with servers with
	// blessings from pprovider.
	pclient.BlessingStore().Set(bless(pprovider, pclient, "client"), "root")

	for i, test := range tests {
		name := fmt.Sprintf("(Name:%q, Server:%q, Allowed:%v)", test.name, test.server, test.allowed)
		if err := pserver.BlessingStore().SetDefault(test.server); err != nil {
			t.Fatalf("SetDefault failed on server's BlessingStore: %v", err)
		}
		if _, err := pserver.BlessingStore().Set(test.server, "root"); err != nil {
			t.Fatalf("Set failed on server's BlessingStore: %v", err)
		}
		// Recreate client in each test (so as to not re-use VCs to the server).
		client, err := InternalNewClient(mgr, ns, vc.LocalPrincipal{pclient})
		if err != nil {
			t.Errorf("%s: failed to create client: %v", name, err)
			continue
		}
		ctx, cancel := context.WithTimeout(testContextWithoutDeadline(), 10*time.Second)
		var opts []ipc.CallOpt
		if test.allowed != nil {
			opts = append(opts, test.allowed)
		}
		call, err := client.StartCall(ctx, test.name, "Method", nil, opts...)
		if !matchesErrorPattern(err, test.errID, test.err) {
			t.Errorf(`%d: %s: client.StartCall: got error "%v", want to match "%v"`, i, name, err, test.err)
		} else if call != nil {
			blessings, proof := call.RemoteBlessings()
			if proof == nil {
				t.Errorf("%s: Returned nil for remote blessings", name)
			}
			// Currently all tests are configured so that the only
			// blessings presented by the server that are
			// recognized by the client match the pattern
			// "root"
			if len(blessings) < 1 || !security.BlessingPattern("root").MatchedBy(blessings...) {
				t.Errorf("%s: Client sees server as %v, expected a single blessing matching root", name, blessings)
			}
		}
		cancel()
		client.Close()
	}
}

type websocketMode bool
type closeSendMode bool

const (
	useWebsocket websocketMode = true
	noWebsocket  websocketMode = false

	closeSend   closeSendMode = true
	noCloseSend closeSendMode = false
)

func TestRPC(t *testing.T) {
	testRPC(t, closeSend, noWebsocket)
}

func TestRPCWithWebsocket(t *testing.T) {
	testRPC(t, closeSend, useWebsocket)
}

// TestCloseSendOnFinish tests that Finish informs the server that no more
// inputs will be sent by the client if CloseSend has not already done so.
func TestRPCCloseSendOnFinish(t *testing.T) {
	testRPC(t, noCloseSend, noWebsocket)
}

func TestRPCCloseSendOnFinishWithWebsocket(t *testing.T) {
	testRPC(t, noCloseSend, useWebsocket)
}

func testRPC(t *testing.T, shouldCloseSend closeSendMode, shouldUseWebsocket websocketMode) {
	type v []interface{}
	type testcase struct {
		name       string
		method     string
		args       v
		streamArgs v
		startErr   error
		results    v
		finishErr  error
	}
	var (
		tests = []testcase{
			{"mountpoint/server/suffix", "Closure", nil, nil, nil, nil, nil},
			{"mountpoint/server/suffix", "Error", nil, nil, nil, v{errMethod}, nil},

			{"mountpoint/server/suffix", "Echo", v{"foo"}, nil, nil, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, nil},
			{"mountpoint/server/suffix/abc", "Echo", v{"bar"}, nil, nil, v{`method:"Echo",suffix:"suffix/abc",arg:"bar"`}, nil},

			{"mountpoint/server/suffix", "EchoUser", v{"foo", userType("bar")}, nil, nil, v{`method:"EchoUser",suffix:"suffix",arg:"foo"`, userType("bar")}, nil},
			{"mountpoint/server/suffix/abc", "EchoUser", v{"baz", userType("bla")}, nil, nil, v{`method:"EchoUser",suffix:"suffix/abc",arg:"baz"`, userType("bla")}, nil},
			{"mountpoint/server/suffix", "Stream", v{"foo"}, v{userType("bar"), userType("baz")}, nil, v{`method:"Stream",suffix:"suffix",arg:"foo" bar baz`, nil}, nil},
			{"mountpoint/server/suffix/abc", "Stream", v{"123"}, v{userType("456"), userType("789")}, nil, v{`method:"Stream",suffix:"suffix/abc",arg:"123" 456 789`, nil}, nil},
			{"mountpoint/server/suffix", "EchoBlessings", nil, nil, nil, v{"[server]", "[client]"}, nil},
			{"mountpoint/server/suffix", "EchoAndError", v{"bugs bunny"}, nil, nil, v{`method:"EchoAndError",suffix:"suffix",arg:"bugs bunny"`, nil}, nil},
			{"mountpoint/server/suffix", "EchoAndError", v{"error"}, nil, nil, v{`method:"EchoAndError",suffix:"suffix",arg:"error"`, errMethod}, nil},
		}
		name = func(t testcase) string {
			return fmt.Sprintf("%s.%s(%v)", t.name, t.method, t.args)
		}

		pserver = tsecurity.NewPrincipal("server")
		pclient = tsecurity.NewPrincipal("client")

		b = createBundleWS(t, pclient, pserver, &testServer{}, shouldUseWebsocket)
	)
	defer b.cleanup(t)
	// The server needs to recognize the client's root certificate.
	pserver.AddToRoots(pclient.BlessingStore().Default())
	for _, test := range tests {
		vlog.VI(1).Infof("%s client.StartCall", name(test))
		vname := test.name
		if shouldUseWebsocket {
			var err error
			vname, err = fakeWSName(b.ns, vname)
			if err != nil && err != test.startErr {
				t.Errorf(`%s ns.Resolve got error "%v", want "%v"`, name(test), err, test.startErr)
				continue
			}
		}
		call, err := b.client.StartCall(testContext(), vname, test.method, test.args)
		if err != test.startErr {
			t.Errorf(`%s client.StartCall got error "%v", want "%v"`, name(test), err, test.startErr)
			continue
		}
		for _, sarg := range test.streamArgs {
			vlog.VI(1).Infof("%s client.Send(%v)", name(test), sarg)
			if err := call.Send(sarg); err != nil {
				t.Errorf(`%s call.Send(%v) got unexpected error "%v"`, name(test), sarg, err)
			}
			var u userType
			if err := call.Recv(&u); err != nil {
				t.Errorf(`%s call.Recv(%v) got unexpected error "%v"`, name(test), sarg, err)
			}
			if !reflect.DeepEqual(u, sarg) {
				t.Errorf("%s call.Recv got value %v, want %v", name(test), u, sarg)
			}
		}
		if shouldCloseSend {
			vlog.VI(1).Infof("%s call.CloseSend", name(test))
			// When the method does not involve streaming
			// arguments, the server gets all the arguments in
			// StartCall and then sends a response without
			// (unnecessarily) waiting for a CloseSend message from
			// the client.  If the server responds before the
			// CloseSend call is made at the client, the CloseSend
			// call will fail.  Thus, only check for errors on
			// CloseSend if there are streaming arguments to begin
			// with (i.e., only if the server is expected to wait
			// for the CloseSend notification).
			if err := call.CloseSend(); err != nil && len(test.streamArgs) > 0 {
				t.Errorf(`%s call.CloseSend got unexpected error "%v"`, name(test), err)
			}
		}
		vlog.VI(1).Infof("%s client.Finish", name(test))
		results := makeResultPtrs(test.results)
		err = call.Finish(results...)
		if err != test.finishErr {
			t.Errorf(`%s call.Finish got error "%v", want "%v"`, name(test), err, test.finishErr)
		}
		checkResultPtrs(t, name(test), results, test.results)
	}
}

func TestMultipleFinish(t *testing.T) {
	type v []interface{}
	b := createBundle(t, tsecurity.NewPrincipal("client"), tsecurity.NewPrincipal("server"), &testServer{})
	defer b.cleanup(t)
	call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "Echo", v{"foo"})
	if err != nil {
		t.Fatalf(`client.StartCall got error "%v"`, err)
	}
	var results string
	err = call.Finish(&results)
	if err != nil {
		t.Fatalf(`call.Finish got error "%v"`, err)
	}
	// Calling Finish a second time should result in a useful error.
	if err = call.Finish(&results); !matchesErrorPattern(err, verror.BadState, "Finish has already been called") {
		t.Fatalf(`got "%v", want "%v"`, err, verror.BadState)
	}
}

// granter implements ipc.Granter, returning a fixed (security.Blessings, error) pair.
type granter struct {
	ipc.CallOpt
	b   security.Blessings
	err error
}

func (g granter) Grant(id security.Blessings) (security.Blessings, error) { return g.b, g.err }

func TestGranter(t *testing.T) {
	var (
		pclient = tsecurity.NewPrincipal("client")
		pserver = tsecurity.NewPrincipal("server")
		b       = createBundle(t, pclient, pserver, &testServer{})
	)
	defer b.cleanup(t)

	tests := []struct {
		granter                       ipc.Granter
		startErrID, finishErrID       verror.IDAction
		blessing, starterr, finisherr string
	}{
		{blessing: "<nil>"},
		{granter: granter{b: bless(pclient, pserver, "blessed")}, blessing: "client/blessed"},
		{granter: granter{err: errors.New("hell no")}, startErrID: verror.NotTrusted, starterr: "hell no"},
		{granter: granter{b: pclient.BlessingStore().Default()}, finishErrID: verror.NoAccess, finisherr: "blessing granted not bound to this server"},
	}
	for i, test := range tests {
		call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "EchoGrantedBlessings", []interface{}{"argument"}, test.granter)
		if !matchesErrorPattern(err, test.startErrID, test.starterr) {
			t.Errorf("%d: %+v: StartCall returned error %v", i, test, err)
		}
		if err != nil {
			continue
		}
		var result, blessing string
		if err = call.Finish(&result, &blessing); !matchesErrorPattern(err, test.finishErrID, test.finisherr) {
			t.Errorf("%+v: Finish returned error %v", test, err)
		}
		if err != nil {
			continue
		}
		if result != "argument" || blessing != test.blessing {
			t.Errorf("%+v: Got (%q, %q)", test, result, blessing)
		}
	}
}

func mkThirdPartyCaveat(discharger security.PublicKey, location string, c security.Caveat) security.Caveat {
	tpc, err := security.NewPublicKeyCaveat(discharger, location, security.ThirdPartyRequirements{}, c)
	if err != nil {
		panic(err)
	}
	return tpc
}

// dischargeTestServer implements the discharge service. Always fails to
// issue a discharge, but records the impetus and traceid of the RPC call.
type dischargeTestServer struct {
	p       security.Principal
	impetus []security.DischargeImpetus
	traceid []uniqueid.Id
}

func (s *dischargeTestServer) Discharge(ctx ipc.ServerContext, cav vdl.AnyRep, impetus security.DischargeImpetus) (vdl.AnyRep, error) {
	s.impetus = append(s.impetus, impetus)
	s.traceid = append(s.traceid, vtrace.GetSpan(ctx.Context()).Trace())
	return nil, fmt.Errorf("discharges not issued")
}

func (s *dischargeTestServer) Release() ([]security.DischargeImpetus, []uniqueid.Id) {
	impetus, traceid := s.impetus, s.traceid
	s.impetus, s.traceid = nil, nil
	return impetus, traceid
}

func TestDischargeImpetusAndContextPropagation(t *testing.T) {
	var (
		pserver     = tsecurity.NewPrincipal("server")
		pdischarger = tsecurity.NewPrincipal("discharger")
		pclient     = tsecurity.NewPrincipal("client")
		sm          = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
		ns          = tnaming.NewSimpleNamespace()

		mkClient = func(req security.ThirdPartyRequirements) vc.LocalPrincipal {
			// Setup the client so that it shares a blessing with a third-party caveat with the server.
			cav, err := security.NewPublicKeyCaveat(pdischarger.PublicKey(), "mountpoint/discharger", req, security.UnconstrainedUse())
			if err != nil {
				t.Fatalf("Failed to create ThirdPartyCaveat(%+v): %v", req, err)
			}
			b, err := pclient.BlessSelf("client_for_server", cav)
			if err != nil {
				t.Fatalf("BlessSelf failed: %v", err)
			}
			pclient.BlessingStore().Set(b, "server")
			return vc.LocalPrincipal{pclient}
		}
	)
	// Initialize the client principal.
	// It trusts both the application server and the discharger.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	pclient.AddToRoots(pdischarger.BlessingStore().Default())
	// Share a blessing without any third-party caveats with the discharger.
	// It could share the same blessing as generated by setupClientBlessing, but
	// that will lead to possibly debugging confusion (since it will try to fetch
	// a discharge to talk to the discharge service).
	if b, err := pclient.BlessSelf("client_for_discharger"); err != nil {
		t.Fatalf("BlessSelf failed: %v", err)
	} else {
		pclient.BlessingStore().Set(b, "discharger")
	}

	// Setup the discharge server.
	var tester dischargeTestServer
	ctx := testContext()
	dischargeServer, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{pdischarger})
	if err != nil {
		t.Fatal(err)
	}
	defer dischargeServer.Stop()
	if _, err := dischargeServer.Listen(listenSpec); err != nil {
		t.Fatal(err)
	}
	if err := dischargeServer.Serve("mountpoint/discharger", &tester, &testServerAuthorizer{}); err != nil {
		t.Fatal(err)
	}

	// Setup the application server.
	appServer, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{pserver})
	if err != nil {
		t.Fatal(err)
	}
	defer appServer.Stop()
	eps, err := appServer.Listen(listenSpec)
	if err != nil {
		t.Fatal(err)
	}
	// TODO(bjornick,cnicolaou,ashankar): This is a hack to workaround the
	// fact that a single Listen on the "tcp" protocol followed by a call
	// to Serve(<name>, ...) transparently creates two endpoints (one for
	// tcp, one for websockets) and maps both to <name> via a mount.
	// Because all endpoints to a name are tried in a parallel, this
	// transparency makes this test hard to follow (many discharge fetch
	// attempts are made - one for VIF authentication, one for VC
	// authentication and one for the actual RPC - and having them be made
	// to two different endpoints in parallel leads to a lot of
	// non-determinism). The last plan of record known by the author of
	// this comment was to stop this sly creation of two endpoints and
	// require that they be done explicitly. When that happens, this hack
	// can go away, but till then, this workaround allows the test to be
	// more predictable by ensuring there is only one VIF/VC/Flow to the
	// server.
	object := naming.JoinAddressName(eps[0].String(), "object") // instead of "mountpoint/object"
	if err := appServer.Serve("mountpoint/object", &testServer{}, &testServerAuthorizer{}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		Requirements security.ThirdPartyRequirements
		Impetus      security.DischargeImpetus
	}{
		{ // No requirements, no impetus
			Requirements: security.ThirdPartyRequirements{},
			Impetus:      security.DischargeImpetus{},
		},
		{ // Require everything
			Requirements: security.ThirdPartyRequirements{ReportServer: true, ReportMethod: true, ReportArguments: true},
			Impetus:      security.DischargeImpetus{Server: []security.BlessingPattern{"server"}, Method: "Method", Arguments: []vdl.AnyRep{vdl.AnyRep("argument")}},
		},
		{ // Require only the method name
			Requirements: security.ThirdPartyRequirements{ReportMethod: true},
			Impetus:      security.DischargeImpetus{Method: "Method"},
		},
	}

	for testidx, test := range tests {
		pclient := mkClient(test.Requirements)
		client, err := InternalNewClient(sm, ns, pclient)
		if err != nil {
			t.Fatalf("InternalNewClient(%+v) failed: %v", test.Requirements, err)
		}
		defer client.Close()
		ctx := testContext()
		tid := vtrace.GetSpan(ctx).Trace()
		// StartCall should fetch the discharge, do not worry about finishing the RPC - do not care about that for this test.
		if _, err := client.StartCall(ctx, object, "Method", []interface{}{"argument"}); err != nil {
			t.Errorf("StartCall(%+v) failed: %v", test.Requirements, err)
			continue
		}
		impetus, traceid := tester.Release()
		// There should have been 2 or 3 attempts to fetch a discharge
		// (since the discharge service doesn't actually issue a valid
		// discharge, there is no re-usable discharge between these attempts):
		// (1) When creating a VIF with the server hosting the remote object.
		//     (This will happen only for the first test, where the stream.Manager
		//     authenticates at the VIF level for the very first time).
		// (2) When creating a VC with the server hosting the remote object.
		// (3) When making the RPC to the remote object.
		num := 3
		if testidx > 0 {
			num = 2
		}
		if want := num; len(impetus) != want || len(traceid) != want {
			t.Errorf("Test %+v: Got (%d, %d) (#impetus, #traceid), wanted %d each", test.Requirements, len(impetus), len(traceid), want)
			continue
		}
		// VC creation does not have any "impetus", it is established without
		// knowledge of the context of the RPC. So ignore that.
		//
		// TODO(ashankar): Should the impetus of the RPC that initiated the
		// VIF/VC creation be propagated?
		if got, want := impetus[len(impetus)-1], test.Impetus; !reflect.DeepEqual(got, want) {
			t.Errorf("Test %+v: Got impetus %v, want %v", test.Requirements, got, want)
		}
		// But the context used for all of this should be the same
		// (thereby allowing debug traces to link VIF/VC creation with
		// the RPC that initiated them).
		for idx, got := range traceid {
			if !reflect.DeepEqual(got, tid) {
				t.Errorf("Test %+v: %d - Got trace id %q, want %q", test.Requirements, idx, hex.EncodeToString(got[:]), hex.EncodeToString(tid[:]))
			}
		}
	}
}

func TestRPCClientAuthorization(t *testing.T) {
	type v []interface{}
	var (
		// Principals
		pclient, pserver = tsecurity.NewPrincipal("client"), tsecurity.NewPrincipal("server")
		pdischarger      = pserver

		now = time.Now()

		serverName          = "mountpoint/server"
		dischargeServerName = "mountpoint/dischargeserver"

		// Caveats on blessings to the client: First-party caveats
		cavOnlyEcho = mkCaveat(security.MethodCaveat("Echo"))
		cavExpired  = mkCaveat(security.ExpiryCaveat(now.Add(-1 * time.Second)))
		// Caveats on blessings to the client: Third-party caveats
		cavTPValid   = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/server/discharger", mkCaveat(security.ExpiryCaveat(now.Add(24*time.Hour))))
		cavTPExpired = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/server/discharger", mkCaveat(security.ExpiryCaveat(now.Add(-1*time.Second))))

		// Client blessings that will be tested.
		bServerClientOnlyEcho  = bless(pserver, pclient, "onlyecho", cavOnlyEcho)
		bServerClientExpired   = bless(pserver, pclient, "expired", cavExpired)
		bServerClientTPValid   = bless(pserver, pclient, "dischargeable_third_party_caveat", cavTPValid)
		bServerClientTPExpired = bless(pserver, pclient, "expired_third_party_caveat", cavTPExpired)
		bClient                = pclient.BlessingStore().Default()
		bRandom, _             = pclient.BlessSelf("random")

		mgr   = imanager.InternalNew(naming.FixedRoutingID(0x1111111))
		ns    = tnaming.NewSimpleNamespace()
		tests = []struct {
			blessings  security.Blessings // Blessings used by the client
			name       string             // object name on which the method is invoked
			method     string
			args       v
			results    v
			authorized bool // Whether or not the RPC should be authorized by the server.
		}{
			// There are three different authorization policies (security.Authorizer implementations)
			// used by the server, depending on the suffix (see testServerDisp.Lookup):
			// - nilAuth suffix: the default authorization policy (only delegates of or delegators of the server can call RPCs)
			// - aclAuth suffix: the ACL only allows blessings matching the patterns "server" or "client"
			// - other suffixes: testServerAuthorizer allows any principal to call any method except "Unauthorized"

			// Expired blessings should fail nilAuth and aclAuth (which care about names), but should succeed on
			// other suffixes (which allow all blessings), unless calling the Unauthorized method.
			{bServerClientExpired, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, false},
			{bServerClientExpired, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, false},
			{bServerClientExpired, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, true},
			{bServerClientExpired, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, false},

			// Same for blessings that should fail to obtain a discharge for the third party caveat.
			{bServerClientTPExpired, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, false},
			{bServerClientTPExpired, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, false},
			{bServerClientTPExpired, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, true},
			{bServerClientTPExpired, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, false},

			// The "server/client" blessing (with MethodCaveat("Echo")) should satisfy all authorization policies
			// when "Echo" is called.
			{bServerClientOnlyEcho, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, true},
			{bServerClientOnlyEcho, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, true},
			{bServerClientOnlyEcho, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, true},

			// The "server/client" blessing (with MethodCaveat("Echo")) should satisfy no authorization policy
			// when any other method is invoked, except for the testServerAuthorizer policy (which will
			// not recognize the blessing "server/onlyecho", but it would authorize anyone anyway).
			{bServerClientOnlyEcho, "mountpoint/server/nilAuth", "Closure", nil, nil, false},
			{bServerClientOnlyEcho, "mountpoint/server/aclAuth", "Closure", nil, nil, false},
			{bServerClientOnlyEcho, "mountpoint/server/suffix", "Closure", nil, nil, true},

			// The "client" blessing doesn't satisfy the default authorization policy, but does satisfy
			// the ACL and the testServerAuthorizer policy.
			{bClient, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, false},
			{bClient, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, true},
			{bClient, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, true},
			{bClient, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, false},

			// The "random" blessing does not satisfy either the default policy or the ACL, but does
			// satisfy testServerAuthorizer.
			{bRandom, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, false},
			{bRandom, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, false},
			{bRandom, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, true},
			{bRandom, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, false},

			// The "server/dischargeable_third_party_caveat" blessing satisfies all policies.
			// (the discharges should be fetched).
			{bServerClientTPValid, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, true},
			{bServerClientTPValid, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, true},
			{bServerClientTPValid, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, true},
			{bServerClientTPValid, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, false},
		}
	)
	// Start the main server.
	_, server := startServer(t, pserver, mgr, ns, serverName, testServerDisp{&testServer{}})
	defer stopServer(t, server, ns, serverName)

	// Start the discharge server.
	_, dischargeServer := startServer(t, pdischarger, mgr, ns, dischargeServerName, testutil.LeafDispatcher(&dischargeServer{}, &acceptAllAuthorizer{}))
	defer stopServer(t, dischargeServer, ns, dischargeServerName)

	// The server should recognize the client principal as an authority on "client" and "random" blessings.
	pserver.AddToRoots(bClient)
	pserver.AddToRoots(bRandom)
	// And the client needs to recognize the server's and discharger's blessings to decide which of its
	// own blessings to share.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	// tsecurity.NewPrincipal sets up a principal that shares blessings with all servers, undo that.
	pclient.BlessingStore().Set(nil, security.AllPrincipals)

	for _, test := range tests {
		name := fmt.Sprintf("%q.%s(%v) by %v", test.name, test.method, test.args, test.blessings)
		client, err := InternalNewClient(mgr, ns, vc.LocalPrincipal{pclient})
		if err != nil {
			t.Fatalf("InternalNewClient failed: %v", err)
		}
		defer client.Close()

		pclient.BlessingStore().Set(test.blessings, "server")
		call, err := client.StartCall(testContext(), test.name, test.method, test.args)
		if err != nil {
			t.Errorf(`%s client.StartCall got unexpected error: "%v"`, name, err)
			continue
		}

		results := makeResultPtrs(test.results)
		err = call.Finish(results...)
		if err != nil && test.authorized {
			t.Errorf(`%s call.Finish got error: "%v", wanted the RPC to succeed`, name, err)
		} else if err == nil && !test.authorized {
			t.Errorf("%s call.Finish succeeded, expected authorization failure", name)
		} else if !test.authorized && !verror.Is(err, verror.NoAccess.ID) {
			t.Errorf("%s. call.Finish returned error %v(%v), wanted %v", name, verror.ErrorID(verror.Convert(verror.NoAccess, nil, err)), err, verror.NoAccess)
		}
	}
}

func TestDischargePurgeFromCache(t *testing.T) {
	var (
		pserver     = tsecurity.NewPrincipal("server")
		pdischarger = pserver // In general, the discharger can be a separate principal. In this test, it happens to be the server.
		pclient     = tsecurity.NewPrincipal("client")
		// Client is blessed with a third-party caveat. The discharger service issues discharges with a fakeTimeCaveat.
		// This blessing is presented to "server".
		bclient = bless(pserver, pclient, "client", mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/server/discharger", security.UnconstrainedUse()))
	)
	// Setup the client to recognize the server's blessing and present bclient to it.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	pclient.BlessingStore().Set(bclient, "server")

	b := createBundle(t, nil, pserver, &testServer{})
	defer b.cleanup(t)

	var err error
	if b.client, err = InternalNewClient(b.sm, b.ns, vc.LocalPrincipal{pclient}); err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	call := func() error {
		call, err := b.client.StartCall(testContext(), "mountpoint/server/aclAuth", "Echo", []interface{}{"batman"})
		if err != nil {
			return err //fmt.Errorf("client.StartCall failed: %v", err)
		}
		var got string
		if err := call.Finish(&got); err != nil {
			return err //fmt.Errorf("client.Finish failed: %v", err)
		}
		if want := `method:"Echo",suffix:"aclAuth",arg:"batman"`; got != want {
			return verror.Convert(verror.BadArg, nil, fmt.Errorf("Got [%v] want [%v]", got, want))
		}
		return nil
	}

	// First call should succeed
	if err := call(); err != nil {
		t.Fatal(err)
	}
	// Advance virtual clock, which will invalidate the discharge
	clock.Advance(1)
	if err, want := call(), "not authorized"; !matchesErrorPattern(err, verror.NoAccess, want) {
		t.Errorf("Got error [%v] wanted to match pattern %q", err, want)
	}
	// But retrying will succeed since the discharge should be purged from cache and refreshed
	if err := call(); err != nil {
		t.Fatal(err)
	}
}

type cancelTestServer struct {
	started   chan struct{}
	cancelled chan struct{}
	t         *testing.T
}

func newCancelTestServer(t *testing.T) *cancelTestServer {
	return &cancelTestServer{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
		t:         t,
	}
}

func (s *cancelTestServer) CancelStreamReader(call ipc.ServerCall) error {
	close(s.started)
	var b []byte
	if err := call.Recv(&b); err != io.EOF {
		s.t.Errorf("Got error %v, want io.EOF", err)
	}
	<-call.Context().Done()
	close(s.cancelled)
	return nil
}

// CancelStreamIgnorer doesn't read from it's input stream so all it's
// buffers fill.  The intention is to show that call.Done() is closed
// even when the stream is stalled.
func (s *cancelTestServer) CancelStreamIgnorer(call ipc.ServerCall) error {
	close(s.started)
	<-call.Context().Done()
	close(s.cancelled)
	return nil
}

func waitForCancel(t *testing.T, ts *cancelTestServer, cancel context.CancelFunc) {
	<-ts.started
	cancel()
	<-ts.cancelled
}

// TestCancel tests cancellation while the server is reading from a stream.
func TestCancel(t *testing.T) {
	ts := newCancelTestServer(t)
	b := createBundle(t, tsecurity.NewPrincipal("client"), tsecurity.NewPrincipal("server"), ts)
	defer b.cleanup(t)

	ctx, cancel := context.WithCancel(testContext())
	_, err := b.client.StartCall(ctx, "mountpoint/server/suffix", "CancelStreamReader", []interface{}{})
	if err != nil {
		t.Fatalf("Start call failed: %v", err)
	}
	waitForCancel(t, ts, cancel)
}

// TestCancelWithFullBuffers tests that even if the writer has filled the buffers and
// the server is not reading that the cancel message gets through.
func TestCancelWithFullBuffers(t *testing.T) {
	ts := newCancelTestServer(t)
	b := createBundle(t, tsecurity.NewPrincipal("client"), tsecurity.NewPrincipal("server"), ts)
	defer b.cleanup(t)

	ctx, cancel := context.WithCancel(testContext())
	call, err := b.client.StartCall(ctx, "mountpoint/server/suffix", "CancelStreamIgnorer", []interface{}{})
	if err != nil {
		t.Fatalf("Start call failed: %v", err)
	}
	// Fill up all the write buffers to ensure that cancelling works even when the stream
	// is blocked.
	call.Send(make([]byte, vc.MaxSharedBytes))
	call.Send(make([]byte, vc.DefaultBytesBufferedPerFlow))

	waitForCancel(t, ts, cancel)
}

type streamRecvInGoroutineServer struct{ c chan error }

func (s *streamRecvInGoroutineServer) RecvInGoroutine(call ipc.ServerCall) error {
	// Spawn a goroutine to read streaming data from the client.
	go func() {
		var i interface{}
		for {
			err := call.Recv(&i)
			if err != nil {
				s.c <- err
				return
			}
		}
	}()
	// Imagine the server did some processing here and now that it is done,
	// it does not care to see what else the client has to say.
	return nil
}

func TestStreamReadTerminatedByServer(t *testing.T) {
	s := &streamRecvInGoroutineServer{c: make(chan error, 1)}
	b := createBundle(t, tsecurity.NewPrincipal("client"), tsecurity.NewPrincipal("server"), s)
	defer b.cleanup(t)

	call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "RecvInGoroutine", []interface{}{})
	if err != nil {
		t.Fatalf("StartCall failed: %v", err)
	}

	c := make(chan error, 1)
	go func() {
		for i := 0; true; i++ {
			if err := call.Send(i); err != nil {
				c <- err
				return
			}
		}
	}()

	// The goroutine at the server executing "Recv" should have terminated
	// with EOF.
	if err := <-s.c; err != io.EOF {
		t.Errorf("Got %v at server, want io.EOF", err)
	}
	// The client Send should have failed since the RPC has been
	// terminated.
	if err := <-c; err == nil {
		t.Errorf("Client Send should fail as the server should have closed the flow")
	}
}

// TestConnectWithIncompatibleServers tests that clients ignore incompatible endpoints.
func TestConnectWithIncompatibleServers(t *testing.T) {
	b := createBundle(t, tsecurity.NewPrincipal("client"), tsecurity.NewPrincipal("server"), &testServer{})
	defer b.cleanup(t)

	// Publish some incompatible endpoints.
	publisher := publisher.New(testContext(), b.ns, publishPeriod)
	defer publisher.WaitForStop()
	defer publisher.Stop()
	publisher.AddName("incompatible")
	publisher.AddServer("/@2@tcp@localhost:10000@@1000000@2000000@@", false)
	publisher.AddServer("/@2@tcp@localhost:10001@@2000000@3000000@@", false)

	ctx, _ := context.WithTimeout(testContext(), 100*time.Millisecond)

	_, err := b.client.StartCall(ctx, "incompatible/suffix", "Echo", []interface{}{"foo"})
	if !verror.Is(err, verror.NoServers.ID) {
		t.Errorf("Expected error %s, found: %v", verror.NoServers, err)
	}

	// Now add a server with a compatible endpoint and try again.
	publisher.AddServer("/"+b.ep.String(), false)
	publisher.AddName("incompatible")

	call, err := b.client.StartCall(testContext(), "incompatible/suffix", "Echo", []interface{}{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if err = call.Finish(&result); err != nil {
		t.Errorf("Unexpected error finishing call %v", err)
	}
	expected := `method:"Echo",suffix:"suffix",arg:"foo"`
	if result != expected {
		t.Errorf("Wrong result returned.  Got %s, wanted %s", result, expected)
	}
}

func TestPreferredAddress(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	pa := func(string, []ipc.Address) ([]ipc.Address, error) {
		a := &net.IPAddr{}
		a.IP = net.ParseIP("1.1.1.1")
		return []ipc.Address{&netstate.AddrIfc{Addr: a}}, nil
	}
	ctx := testContext()
	server, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{tsecurity.NewPrincipal("server")})
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()

	spec := ipc.ListenSpec{
		Addrs:          ipc.ListenAddrs{{"tcp", ":0"}},
		AddressChooser: pa,
	}
	eps, err := server.Listen(spec)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	iep := eps[0].(*inaming.Endpoint)
	host, _, err := net.SplitHostPort(iep.Address)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if got, want := host, "1.1.1.1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Won't override the specified address.
	eps, err = server.Listen(listenSpec)
	iep = eps[0].(*inaming.Endpoint)
	host, _, err = net.SplitHostPort(iep.Address)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if got, want := host, "127.0.0.1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPreferredAddressErrors(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	paerr := func(_ string, a []ipc.Address) ([]ipc.Address, error) {
		return nil, fmt.Errorf("oops")
	}
	ctx := testContext()
	server, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{tsecurity.NewPrincipal("server")})
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	spec := ipc.ListenSpec{
		Addrs:          ipc.ListenAddrs{{"tcp", ":0"}},
		AddressChooser: paerr,
	}
	eps, err := server.Listen(spec)
	iep := eps[0].(*inaming.Endpoint)
	host, _, err := net.SplitHostPort(iep.Address)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		t.Fatalf("failed to parse IP address: %q", host)
	}
	if !ip.IsUnspecified() {
		t.Errorf("IP: %q is not unspecified", ip)
	}
}

func TestSecurityNone(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x66666666))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	ctx := testContext()
	server, err := testInternalNewServer(ctx, sm, ns, options.VCSecurityNone)
	if err != nil {
		t.Fatalf("InternalNewServer failed: %v", err)
	}
	if _, err = server.Listen(listenSpec); err != nil {
		t.Fatalf("server.Listen failed: %v", err)
	}
	disp := &testServerDisp{&testServer{}}
	if err := server.ServeDispatcher("mp/server", disp); err != nil {
		t.Fatalf("server.Serve failed: %v", err)
	}
	client, err := InternalNewClient(sm, ns)
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	// When using VCSecurityNone, all authorization checks should be skipped, so
	// unauthorized methods should be callable.
	call, err := client.StartCall(ctx, "mp/server", "Unauthorized", nil, options.VCSecurityNone)
	if err != nil {
		t.Fatalf("client.StartCall failed: %v", err)
	}
	var got string
	var ierr error
	if err := call.Finish(&got, &ierr); err != nil {
		t.Errorf("call.Finish failed: %v", err)
	}
	if want := "UnauthorizedResult"; got != want {
		t.Errorf("got (%v), want (%v)", got, want)
	}
}

func TestCallWithNilContext(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x66666666))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	client, err := InternalNewClient(sm, ns)
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	call, err := client.StartCall(nil, "foo", "bar", []interface{}{}, options.VCSecurityNone)
	if call != nil {
		t.Errorf("Expected nil interface got: %#v", call)
	}
	if !verror.Is(err, verror.BadArg.ID) {
		t.Errorf("Expected an BadArg error, got: %s", err.Error())
	}
}

func TestServerBlessingsOpt(t *testing.T) {
	var (
		pserver   = tsecurity.NewPrincipal("server")
		pclient   = tsecurity.NewPrincipal("client")
		batman, _ = pserver.BlessSelf("batman")
	)
	// Make the client recognize all server blessings
	if err := pclient.AddToRoots(batman); err != nil {
		t.Fatal(err)
	}
	if err := pclient.AddToRoots(pserver.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}
	// Start a server that uses the ServerBlessings option to configure itself
	// to act as batman (as opposed to using the default blessing).
	ns := tnaming.NewSimpleNamespace()
	ctx := testContext()
	runServer := func(name string, opts ...ipc.ServerOpt) stream.Manager {
		opts = append(opts, vc.LocalPrincipal{pserver})
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		sm := imanager.InternalNew(rid)
		server, err := testInternalNewServer(ctx, sm, ns, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.Listen(listenSpec); err != nil {
			t.Fatal(err)
		}
		if err := server.Serve(name, &testServer{}, nil); err != nil {
			t.Fatal(err)
		}
		return sm
	}

	defer runServer("mountpoint/batman", options.ServerBlessings{batman}).Shutdown()
	defer runServer("mountpoint/default").Shutdown()

	// And finally, make and RPC and see that the client sees "batman"
	runClient := func(server string) ([]string, error) {
		smc := imanager.InternalNew(naming.FixedRoutingID(0xc))
		defer smc.Shutdown()
		client, err := InternalNewClient(
			smc,
			ns,
			vc.LocalPrincipal{pclient})
		if err != nil {
			return nil, err
		}
		defer client.Close()
		call, err := client.StartCall(testContext(), server, "Closure", nil)
		if err != nil {
			return nil, err
		}
		blessings, _ := call.RemoteBlessings()
		return blessings, nil
	}

	// When talking to mountpoint/batman, should see "batman"
	// When talking to mountpoint/default, should see "server"
	if got, err := runClient("mountpoint/batman"); err != nil || len(got) != 1 || got[0] != "batman" {
		t.Errorf("Got (%v, %v) wanted 'batman'", got, err)
	}
	if got, err := runClient("mountpoint/default"); err != nil || len(got) != 1 || got[0] != "server" {
		t.Errorf("Got (%v, %v) wanted 'server'", got, err)
	}
}

type mockDischarger struct {
	mu     sync.Mutex
	called bool
}

func (m *mockDischarger) Discharge(ctx ipc.ServerContext, caveat security.Caveat, _ security.DischargeImpetus) (vdl.AnyRep, error) {
	m.mu.Lock()
	m.called = true
	m.mu.Unlock()
	return ctx.LocalPrincipal().MintDischarge(caveat, security.UnconstrainedUse())
}

func TestNoDischargesOpt(t *testing.T) {
	var (
		pdischarger = tsecurity.NewPrincipal("discharger")
		pserver     = tsecurity.NewPrincipal("server")
		pclient     = tsecurity.NewPrincipal("client")
	)
	// Make the client recognize all server blessings
	if err := pclient.AddToRoots(pserver.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}
	if err := pclient.AddToRoots(pdischarger.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}

	// Bless the client with a ThirdPartyCaveat.
	tpcav := mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/discharger", mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))
	blessings, err := pserver.Bless(pclient.PublicKey(), pserver.BlessingStore().Default(), "tpcav", tpcav)
	if err != nil {
		t.Fatalf("failed to create Blessings: %v", err)
	}
	if _, err = pclient.BlessingStore().Set(blessings, "server"); err != nil {
		t.Fatalf("failed to set blessings: %v", err)
	}

	ns := tnaming.NewSimpleNamespace()
	ctx := testContext()
	runServer := func(name string, obj interface{}, principal security.Principal) stream.Manager {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		sm := imanager.InternalNew(rid)
		server, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{principal})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.Listen(listenSpec); err != nil {
			t.Fatal(err)
		}
		if err := server.Serve(name, obj, acceptAllAuthorizer{}); err != nil {
			t.Fatal(err)
		}
		return sm
	}

	// Setup the disharger and test server.
	discharger := &mockDischarger{}
	defer runServer("mountpoint/discharger", discharger, pdischarger).Shutdown()
	defer runServer("mountpoint/testServer", &testServer{}, pserver).Shutdown()

	runClient := func(noDischarges bool) {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		smc := imanager.InternalNew(rid)
		defer smc.Shutdown()
		client, err := InternalNewClient(smc, ns, vc.LocalPrincipal{pclient})
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}
		defer client.Close()
		var opts []ipc.CallOpt
		if noDischarges {
			opts = append(opts, vc.NoDischarges{})
		}
		if _, err = client.StartCall(testContext(), "mountpoint/testServer", "Closure", nil, opts...); err != nil {
			t.Fatalf("failed to StartCall: %v", err)
		}
	}

	// Test that when the NoDischarges option is set, mockDischarger does not get called.
	if runClient(true); discharger.called {
		t.Errorf("did not expect discharger to be called")
	}
	discharger.called = false
	// Test that when the Nodischarges option is not set, mockDischarger does get called.
	if runClient(false); !discharger.called {
		t.Errorf("expected discharger to be called")
	}
}

func TestNoImplicitDischargeFetching(t *testing.T) {
	// This test ensures that discharge clients only fetch discharges for the specified tp caveats and not its own.
	var (
		pdischarger1     = tsecurity.NewPrincipal("discharger1")
		pdischarger2     = tsecurity.NewPrincipal("discharger2")
		pdischargeClient = tsecurity.NewPrincipal("dischargeClient")
	)

	// Bless the client with a ThirdPartyCaveat from discharger1.
	tpcav1 := mkThirdPartyCaveat(pdischarger1.PublicKey(), "mountpoint/discharger1", mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))
	blessings, err := pdischarger1.Bless(pdischargeClient.PublicKey(), pdischarger1.BlessingStore().Default(), "tpcav1", tpcav1)
	if err != nil {
		t.Fatalf("failed to create Blessings: %v", err)
	}
	if err = pdischargeClient.BlessingStore().SetDefault(blessings); err != nil {
		t.Fatalf("failed to set blessings: %v", err)
	}

	ns := tnaming.NewSimpleNamespace()
	ctx := testContext()
	runServer := func(name string, obj interface{}, principal security.Principal) stream.Manager {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		sm := imanager.InternalNew(rid)
		server, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{principal})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.Listen(listenSpec); err != nil {
			t.Fatal(err)
		}
		if err := server.Serve(name, obj, acceptAllAuthorizer{}); err != nil {
			t.Fatal(err)
		}
		return sm
	}

	// Setup the disharger and test server.
	discharger1 := &mockDischarger{}
	discharger2 := &mockDischarger{}
	defer runServer("mountpoint/discharger1", discharger1, pdischarger1).Shutdown()
	defer runServer("mountpoint/discharger2", discharger2, pdischarger2).Shutdown()

	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	sm := imanager.InternalNew(rid)

	c, err := InternalNewClient(sm, ns, vc.LocalPrincipal{pdischargeClient})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	dc := c.(*client).dc
	tpcav2, err := security.NewPublicKeyCaveat(pdischarger2.PublicKey(), "mountpoint/discharger2", security.ThirdPartyRequirements{}, mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))
	if err != nil {
		t.Error(err)
	}
	dc.PrepareDischarges(testContext(), []security.Caveat{tpcav2}, security.DischargeImpetus{})

	// Ensure that discharger1 was not called and discharger2 was called.
	if discharger1.called {
		t.Errorf("discharge for caveat on discharge client should not have been fetched.")
	}
	if !discharger2.called {
		t.Errorf("discharge for caveat passed to PrepareDischarges should have been fetched.")
	}
}

// TestBlessingsCache tests that the VCCache is used to sucessfully used to cache duplicate
// calls blessings.
func TestBlessingsCache(t *testing.T) {
	var (
		pserver = tsecurity.NewPrincipal("server")
		pclient = tsecurity.NewPrincipal("client")
	)
	// Make the client recognize all server blessings
	if err := pclient.AddToRoots(pserver.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}

	ns := tnaming.NewSimpleNamespace()
	ctx := testContext()
	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	runServer := func(principal security.Principal, rid naming.RoutingID) (ipc.Server, stream.Manager, naming.Endpoint) {
		sm := imanager.InternalNew(rid)
		server, err := testInternalNewServer(ctx, sm, ns, vc.LocalPrincipal{principal})
		if err != nil {
			t.Fatal(err)
		}
		ep, err := server.Listen(listenSpec)
		if err != nil {
			t.Fatal(err)
		}
		return server, sm, ep[0]
	}

	server, serverSM, serverEP := runServer(pserver, rid)
	go server.Serve("mountpoint/testServer", &testServer{}, acceptAllAuthorizer{})
	defer serverSM.Shutdown()

	newClient := func() ipc.Client {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		smc := imanager.InternalNew(rid)
		defer smc.Shutdown()
		client, err := InternalNewClient(smc, ns, vc.LocalPrincipal{pclient})
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}
		return client
	}

	runClient := func(client ipc.Client) {
		var call ipc.Call
		if call, err = client.StartCall(testContext(), "/"+serverEP.String(), "Closure", nil); err != nil {
			t.Fatalf("failed to StartCall: %v", err)
		}
		if err := call.Finish(); err != nil {
			t.Fatal(err)
		}
	}

	cachePrefix := naming.Join("ipc", "server", "routing-id", rid.String(), "security", "blessings", "cache")
	cacheHits, err := stats.GetStatsObject(naming.Join(cachePrefix, "hits"))
	if err != nil {
		t.Fatal(err)
	}
	cacheAttempts, err := stats.GetStatsObject(naming.Join(cachePrefix, "attempts"))
	if err != nil {
		t.Fatal(err)
	}

	// Check that the blessings cache is not used on the first call.
	clientA := newClient()
	runClient(clientA)
	if gotAttempts, gotHits := cacheAttempts.Value().(int64), cacheHits.Value().(int64); gotAttempts != 1 || gotHits != 0 {
		t.Errorf("got cacheAttempts(%v), cacheHits(%v), expected cacheAttempts(1), cacheHits(0)", gotAttempts, gotHits)
	}
	// Check that the cache is hit on the second call with the same blessings.
	runClient(clientA)
	if gotAttempts, gotHits := cacheAttempts.Value().(int64), cacheHits.Value().(int64); gotAttempts != 2 || gotHits != 1 {
		t.Errorf("got cacheAttempts(%v), cacheHits(%v), expected cacheAttempts(2), cacheHits(1)", gotAttempts, gotHits)
	}
	clientA.Close()
	// Check that the cache is not used with a different client.
	clientB := newClient()
	runClient(clientB)
	if gotAttempts, gotHits := cacheAttempts.Value().(int64), cacheHits.Value().(int64); gotAttempts != 3 || gotHits != 1 {
		t.Errorf("got cacheAttempts(%v), cacheHits(%v), expected cacheAttempts(3), cacheHits(1)", gotAttempts, gotHits)
	}
	// clientB changes its blessings, the cache should not be used.
	blessings, err := pserver.Bless(pclient.PublicKey(), pserver.BlessingStore().Default(), "cav", mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))
	if err != nil {
		t.Fatalf("failed to create Blessings: %v", err)
	}
	if _, err = pclient.BlessingStore().Set(blessings, "server"); err != nil {
		t.Fatalf("failed to set blessings: %v", err)
	}
	runClient(clientB)
	if gotAttempts, gotHits := cacheAttempts.Value().(int64), cacheHits.Value().(int64); gotAttempts != 4 || gotHits != 1 {
		t.Errorf("got cacheAttempts(%v), cacheHits(%v), expected cacheAttempts(4), cacheHits(1)", gotAttempts, gotHits)
	}
	clientB.Close()
}

var fakeTimeCaveat = security.CaveatDescriptor{
	Id:        uniqueid.Id{0x18, 0xba, 0x6f, 0x84, 0xd5, 0xec, 0xdb, 0x9b, 0xf2, 0x32, 0x19, 0x5b, 0x53, 0x92, 0x80, 0x0},
	ParamType: vdl.TypeOf(int64(0)),
}

func TestMain(m *testing.M) {
	testutil.Init()
	security.RegisterCaveatValidator(fakeTimeCaveat, func(_ security.Context, t int64) error {
		if now := clock.Now(); now > int(t) {
			return fmt.Errorf("fakeTimeCaveat expired: now=%d > then=%d", now, t)
		}
		return nil
	})
	os.Exit(m.Run())
}
