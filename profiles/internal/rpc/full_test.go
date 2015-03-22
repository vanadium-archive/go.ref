package rpc

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/uniqueid"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
	"v.io/x/ref/profiles/internal/rpc/stream"

	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/profiles/internal/lib/publisher"
	"v.io/x/ref/profiles/internal/lib/websocket"
	inaming "v.io/x/ref/profiles/internal/naming"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/ws"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/wsh"
	imanager "v.io/x/ref/profiles/internal/rpc/stream/manager"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
	tnaming "v.io/x/ref/profiles/internal/testing/mocks/naming"
	"v.io/x/ref/test/testutil"
)

//go:generate v23 test generate

var (
	errMethod     = verror.New(verror.ErrAborted, nil)
	clock         = new(fakeClock)
	listenAddrs   = rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}
	listenWSAddrs = rpc.ListenAddrs{{"ws", "127.0.0.1:0"}, {"tcp", "127.0.0.1:0"}}
	listenSpec    = rpc.ListenSpec{Addrs: listenAddrs}
	listenWSSpec  = rpc.ListenSpec{Addrs: listenWSAddrs}
)

type fakeClock struct {
	sync.Mutex
	time int64
}

func (c *fakeClock) Now() int64 {
	c.Lock()
	defer c.Unlock()
	return c.time
}

func (c *fakeClock) Advance(steps uint) {
	c.Lock()
	c.time += int64(steps)
	c.Unlock()
}

func testInternalNewServer(ctx *context.T, streamMgr stream.Manager, ns ns.Namespace, principal security.Principal, opts ...rpc.ServerOpt) (rpc.Server, error) {
	client, err := InternalNewClient(streamMgr, ns)
	if err != nil {
		return nil, err
	}
	return InternalNewServer(ctx, streamMgr, ns, client, principal, opts...)
}

type userType string

type testServer struct{}

func (*testServer) Closure(call rpc.ServerCall) error {
	return nil
}

func (*testServer) Error(call rpc.ServerCall) error {
	return errMethod
}

func (*testServer) Echo(call rpc.ServerCall, arg string) (string, error) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", call.Method(), call.Suffix(), arg), nil
}

func (*testServer) EchoUser(call rpc.ServerCall, arg string, u userType) (string, userType, error) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", call.Method(), call.Suffix(), arg), u, nil
}

func (*testServer) EchoBlessings(call rpc.ServerCall) (server, client string, _ error) {
	local := security.LocalBlessingNames(call.Context())
	remote, _ := security.RemoteBlessingNames(call.Context())
	return fmt.Sprintf("%v", local), fmt.Sprintf("%v", remote), nil
}

func (*testServer) EchoGrantedBlessings(call rpc.ServerCall, arg string) (result, blessing string, _ error) {
	return arg, fmt.Sprintf("%v", call.GrantedBlessings()), nil
}

func (*testServer) EchoAndError(call rpc.ServerCall, arg string) (string, error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", call.Method(), call.Suffix(), arg)
	if arg == "error" {
		return result, errMethod
	}
	return result, nil
}

func (*testServer) Stream(call rpc.StreamServerCall, arg string) (string, error) {
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

func (*testServer) Unauthorized(rpc.StreamServerCall) (string, error) {
	return "UnauthorizedResult", nil
}

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(ctx *context.T) error {
	c := security.GetCall(ctx)
	// Verify that the Call object seen by the authorizer
	// has the necessary fields.
	lb := c.LocalBlessings()
	if lb.IsZero() {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalBlessings", c)
	}
	if tpcavs := lb.ThirdPartyCaveats(); len(tpcavs) > 0 && c.LocalDischarges() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalDischarges even when LocalBlessings have third-party caveats", c)

	}
	if c.LocalPrincipal() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalPrincipal", c)
	}
	if c.Method() == "" {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no Method", c)
	}
	if c.LocalEndpoint() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalEndpoint", c)
	}
	if c.RemoteEndpoint() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no RemoteEndpoint", c)
	}

	// Do not authorize the method "Unauthorized".
	if c.Method() == "Unauthorized" {
		return fmt.Errorf("testServerAuthorizer denied access")
	}
	return nil
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	// If suffix is "nilAuth" we use default authorization, if it is "aclAuth" we
	// use an AccessList based authorizer, and otherwise we use the custom testServerAuthorizer.
	var authorizer security.Authorizer
	switch suffix {
	case "discharger":
		return &dischargeServer{}, testServerAuthorizer{}, nil
	case "nilAuth":
		authorizer = nil
	case "aclAuth":
		authorizer = &access.AccessList{
			In: []security.BlessingPattern{"client", "server"},
		}
	default:
		authorizer = testServerAuthorizer{}
	}
	return t.server, authorizer, nil
}

type dischargeServer struct {
	mu     sync.Mutex
	called bool
}

func (ds *dischargeServer) Discharge(call rpc.StreamServerCall, cav security.Caveat, _ security.DischargeImpetus) (security.Discharge, error) {
	ds.mu.Lock()
	ds.called = true
	ds.mu.Unlock()
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return security.Discharge{}, fmt.Errorf("discharger: %v does not represent a third-party caveat", cav)
	}
	if err := tp.Dischargeable(call); err != nil {
		return security.Discharge{}, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", cav, err)
	}
	// Add a fakeTimeCaveat to be able to control discharge expiration via 'clock'.
	expiry, err := security.NewCaveat(fakeTimeCaveat, clock.Now())
	if err != nil {
		return security.Discharge{}, fmt.Errorf("failed to create an expiration on the discharge: %v", err)
	}
	return call.LocalPrincipal().MintDischarge(cav, expiry)
}

func startServer(t *testing.T, ctx *context.T, principal security.Principal, sm stream.Manager, ns ns.Namespace, name string, disp rpc.Dispatcher, opts ...rpc.ServerOpt) (naming.Endpoint, rpc.Server) {
	return startServerWS(t, ctx, principal, sm, ns, name, disp, noWebsocket, opts...)
}

func endpointsToStrings(eps []naming.Endpoint) []string {
	r := make([]string, len(eps))
	for i, e := range eps {
		r[i] = e.String()
	}
	sort.Strings(r)
	return r
}

func startServerWS(t *testing.T, ctx *context.T, principal security.Principal, sm stream.Manager, ns ns.Namespace, name string, disp rpc.Dispatcher, shouldUseWebsocket websocketMode, opts ...rpc.ServerOpt) (naming.Endpoint, rpc.Server) {
	vlog.VI(1).Info("InternalNewServer")
	ctx, _ = v23.SetPrincipal(ctx, principal)
	server, err := testInternalNewServer(ctx, sm, ns, principal, opts...)
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

func verifyMount(t *testing.T, ctx *context.T, ns ns.Namespace, name string) []string {
	me, err := ns.Resolve(ctx, name)
	if err != nil {
		t.Errorf("%s: %s not found in mounttable", loc(1), name)
		return nil
	}
	return me.Names()
}

func verifyMountMissing(t *testing.T, ctx *context.T, ns ns.Namespace, name string) {
	if me, err := ns.Resolve(ctx, name); err == nil {
		names := me.Names()
		t.Errorf("%s: %s not supposed to be found in mounttable; got %d servers instead: %v (%+v)", loc(1), name, len(names), names, me)
	}
}

func stopServer(t *testing.T, ctx *context.T, server rpc.Server, ns ns.Namespace, name string) {
	vlog.VI(1).Info("server.Stop")
	new_name := "should_appear_in_mt/server"
	verifyMount(t, ctx, ns, name)

	// publish a second name
	if err := server.AddName(new_name); err != nil {
		t.Errorf("server.Serve failed: %v", err)
	}
	verifyMount(t, ctx, ns, new_name)

	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}

	verifyMountMissing(t, ctx, ns, name)
	verifyMountMissing(t, ctx, ns, new_name)

	// Check that we can no longer serve after Stop.
	err := server.AddName("name doesn't matter")
	if err == nil || !verror.Is(err, verror.ErrBadState.ID) {
		t.Errorf("either no error, or a wrong error was returned: %v", err)
	}
	vlog.VI(1).Info("server.Stop DONE")
}

// fakeWSName creates a name containing a endpoint address that forces
// the use of websockets. It does so by resolving the original name
// and choosing the 'ws' endpoint from the set of endpoints returned.
// It must return a name since it'll be passed to StartCall.
func fakeWSName(ctx *context.T, ns ns.Namespace, name string) (string, error) {
	// Find the ws endpoint and use that.
	me, err := ns.Resolve(ctx, name)
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
	client rpc.Client
	server rpc.Server
	ep     naming.Endpoint
	ns     ns.Namespace
	sm     stream.Manager
	name   string
}

func (b bundle) cleanup(t *testing.T, ctx *context.T) {
	if b.server != nil {
		stopServer(t, ctx, b.server, b.ns, b.name)
	}
	if b.client != nil {
		b.client.Close()
	}
}

func createBundle(t *testing.T, ctx *context.T, server security.Principal, ts interface{}) (b bundle) {
	return createBundleWS(t, ctx, server, ts, noWebsocket)
}

func createBundleWS(t *testing.T, ctx *context.T, server security.Principal, ts interface{}, shouldUseWebsocket websocketMode) (b bundle) {
	b.sm = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	b.ns = tnaming.NewSimpleNamespace()
	b.name = "mountpoint/server"
	if server != nil {
		b.ep, b.server = startServerWS(t, ctx, server, b.sm, b.ns, b.name, testServerDisp{ts}, shouldUseWebsocket)
	}
	var err error
	if b.client, err = InternalNewClient(b.sm, b.ns); err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
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

func runServer(t *testing.T, ctx *context.T, ns ns.Namespace, principal security.Principal, name string, obj interface{}, opts ...rpc.ServerOpt) stream.Manager {
	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	sm := imanager.InternalNew(rid)
	server, err := testInternalNewServer(ctx, sm, ns, principal, opts...)
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

func TestMultipleCallsToServeAndName(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	ctx, shutdown := initForTest()
	defer shutdown()
	server, err := testInternalNewServer(ctx, sm, ns, testutil.NewPrincipal("server"))
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

	verifyMount(t, ctx, ns, n1)

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
	verifyMount(t, ctx, ns, n2)
	verifyMount(t, ctx, ns, n3)

	server.RemoveName(n1)
	verifyMountMissing(t, ctx, ns, n1)

	server.RemoveName("some randome name")

	if err := server.ServeDispatcher(n4, &testServerDisp{&testServer{}}); err == nil {
		t.Errorf("server.ServeDispatcher should have failed")
	}
	verifyMountMissing(t, ctx, ns, n4)

	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}

	verifyMountMissing(t, ctx, ns, n1)
	verifyMountMissing(t, ctx, ns, n2)
	verifyMountMissing(t, ctx, ns, n3)
}

func TestRPCServerAuthorization(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	const (
		publicKeyErr        = "not matched by server key"
		missingDischargeErr = "missing discharge"
		expiryErr           = "is after expiry"
		allowedErr          = "do not match any allowed server patterns"
	)
	type O []rpc.CallOpt // shorthand
	var (
		pprovider, pclient, pserver = testutil.NewPrincipal("root"), testutil.NewPrincipal(), testutil.NewPrincipal()
		pdischarger                 = pprovider
		now                         = time.Now()
		noErrID                     verror.IDAction

		// Third-party caveats on blessings presented by server.
		cavTPValid   = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/dischargeserver", mkCaveat(security.ExpiryCaveat(now.Add(24*time.Hour))))
		cavTPExpired = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/dischargeserver", mkCaveat(security.ExpiryCaveat(now.Add(-1*time.Second))))

		// Server blessings.
		bServer          = bless(pprovider, pserver, "server")
		bServerExpired   = bless(pprovider, pserver, "expiredserver", mkCaveat(security.ExpiryCaveat(time.Now().Add(-1*time.Second))))
		bServerTPValid   = bless(pprovider, pserver, "serverWithTPCaveats", cavTPValid)
		bServerTPExpired = bless(pprovider, pserver, "serverWithExpiredTPCaveats", cavTPExpired)
		bOther           = bless(pprovider, pserver, "other")
		bTwoBlessings, _ = security.UnionOfBlessings(bServer, bOther)

		mgr   = imanager.InternalNew(naming.FixedRoutingID(0x1111111))
		ns    = tnaming.NewSimpleNamespace()
		tests = []struct {
			server security.Blessings // blessings presented by the server to the client.
			name   string             // name provided by the client to StartCall
			opts   O                  // options provided to StartCall.
			errID  verror.IDAction
			err    string
		}{
			// Client accepts talking to the server only if the
			// server presents valid blessings (and discharges)
			// consistent with the ones published in the endpoint.
			{bServer, "mountpoint/server", nil, noErrID, ""},
			{bServerTPValid, "mountpoint/server", nil, noErrID, ""},

			// Client will not talk to a server that presents
			// expired blessings or is missing discharges.
			{bServerExpired, "mountpoint/server", nil, verror.ErrNotTrusted, expiryErr},
			{bServerTPExpired, "mountpoint/server", nil, verror.ErrNotTrusted, missingDischargeErr},

			// Testing the AllowedServersPolicy option.
			{bServer, "mountpoint/server", O{options.AllowedServersPolicy{"otherroot"}}, verror.ErrNotTrusted, allowedErr},
			{bServer, "mountpoint/server", O{options.AllowedServersPolicy{"root"}}, noErrID, ""},
			{bTwoBlessings, "mountpoint/server", O{options.AllowedServersPolicy{"root/other"}}, noErrID, ""},

			// Test the ServerPublicKey option.
			{bOther, "mountpoint/server", O{options.SkipServerEndpointAuthorization{}, options.ServerPublicKey{bOther.PublicKey()}}, noErrID, ""},
			{bOther, "mountpoint/server", O{options.SkipServerEndpointAuthorization{}, options.ServerPublicKey{testutil.NewPrincipal("irrelevant").PublicKey()}}, verror.ErrNotTrusted, publicKeyErr},

			// Test the "paranoid" names, where the pattern is provided in the name.
			{bServer, "__(root/server)/mountpoint/server", nil, noErrID, ""},
			{bServer, "__(root/other)/mountpoint/server", nil, verror.ErrNotTrusted, allowedErr},
			{bTwoBlessings, "__(root/server)/mountpoint/server", O{options.AllowedServersPolicy{"root/other"}}, noErrID, ""},
		}
	)
	// Start the discharge server.
	_, dischargeServer := startServer(t, ctx, pdischarger, mgr, ns, "mountpoint/dischargeserver", testutil.LeafDispatcher(&dischargeServer{}, &acceptAllAuthorizer{}))
	defer stopServer(t, ctx, dischargeServer, ns, "mountpoint/dischargeserver")

	// Make the client and server principals trust root certificates from
	// pprovider
	pclient.AddToRoots(pprovider.BlessingStore().Default())
	pserver.AddToRoots(pprovider.BlessingStore().Default())
	// Set a blessing that the client is willing to share with servers
	// (that are blessed by pprovider).
	pclient.BlessingStore().Set(bless(pprovider, pclient, "client"), "root")

	clientCtx, _ := v23.SetPrincipal(ctx, pclient)
	client, err := InternalNewClient(mgr, ns)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var server rpc.Server
	stop := func() {
		if server != nil {
			stopServer(t, ctx, server, ns, "mountpoint/server")
		}
	}
	defer stop()
	for i, test := range tests {
		stop() // Stop any server started in the previous test.
		name := fmt.Sprintf("(#%d: Name:%q, Server:%q, opts:%v)", i, test.name, test.server, test.opts)
		if err := pserver.BlessingStore().SetDefault(test.server); err != nil {
			t.Fatalf("SetDefault failed on server's BlessingStore: %v", err)
		}
		if _, err := pserver.BlessingStore().Set(test.server, "root"); err != nil {
			t.Fatalf("Set failed on server's BlessingStore: %v", err)
		}
		_, server = startServer(t, ctx, pserver, mgr, ns, "mountpoint/server", testServerDisp{&testServer{}})
		clientCtx, cancel := context.WithCancel(clientCtx)
		call, err := client.StartCall(clientCtx, test.name, "Method", nil, test.opts...)
		if !matchesErrorPattern(err, test.errID, test.err) {
			t.Errorf(`%s: client.StartCall: got error "%v", want to match "%v"`, name, err, test.err)
		} else if call != nil {
			blessings, proof := call.RemoteBlessings()
			if proof.IsZero() {
				t.Errorf("%s: Returned zero value for remote blessings", name)
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
	}
}

func TestServerManInTheMiddleAttack(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	// Test scenario: A server mounts itself, but then some other service
	// somehow "takes over" the network endpoint (a naughty router
	// perhaps), thus trying to steal traffic.
	var (
		pclient   = testutil.NewPrincipal("client")
		pserver   = testutil.NewPrincipal("server")
		pattacker = testutil.NewPrincipal("attacker")
	)
	// Client recognizes both the server and the attacker's blessings.
	// (Though, it doesn't need to do the latter for the purposes of this
	// test).
	pclient.AddToRoots(pserver.BlessingStore().Default())
	pclient.AddToRoots(pattacker.BlessingStore().Default())

	// Start up the attacker's server.
	attacker, err := testInternalNewServer(
		ctx,
		imanager.InternalNew(naming.FixedRoutingID(0xaaaaaaaaaaaaaaaa)),
		// (To prevent the attacker for legitimately mounting on the
		// namespace that the client will use, provide it with a
		// different namespace).
		tnaming.NewSimpleNamespace(),
		pattacker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := attacker.Listen(listenSpec); err != nil {
		t.Fatal(err)
	}
	if err := attacker.ServeDispatcher("mountpoint/server", testServerDisp{&testServer{}}); err != nil {
		t.Fatal(err)
	}
	var ep naming.Endpoint
	if status := attacker.Status(); len(status.Endpoints) < 1 {
		t.Fatalf("Attacker server does not have an endpoint: %+v", status)
	} else {
		ep = status.Endpoints[0]
	}

	// The legitimate server would have mounted the same endpoint on the
	// namespace, but with different blessings.
	ns := tnaming.NewSimpleNamespace()
	ep.(*inaming.Endpoint).Blessings = []string{"server"}
	if err := ns.Mount(ctx, "mountpoint/server", ep.Name(), time.Hour); err != nil {
		t.Fatal(err)
	}

	// The RPC call should fail because the blessings presented by the
	// (attacker's) server are not consistent with the ones registered in
	// the mounttable trusted by the client.
	client, err := InternalNewClient(
		imanager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc)),
		ns)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, _ = v23.SetPrincipal(ctx, pclient)
	if _, err := client.StartCall(ctx, "mountpoint/server", "Closure", nil); !verror.Is(err, verror.ErrNotTrusted.ID) {
		t.Errorf("Got error %v (errorid=%v), want errorid=%v", err, verror.ErrorID(err), verror.ErrNotTrusted.ID)
	}
	// But the RPC should succeed if the client explicitly
	// decided to skip server authorization.
	if _, err := client.StartCall(ctx, "mountpoint/server", "Closure", nil, options.SkipServerEndpointAuthorization{}); err != nil {
		t.Errorf("Unexpected error(%v) when skipping server authorization", err)
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
	ctx, shutdown := initForTest()
	defer shutdown()
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
			{"mountpoint/server/suffix", "Error", nil, nil, nil, nil, errMethod},

			{"mountpoint/server/suffix", "Echo", v{"foo"}, nil, nil, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, nil},
			{"mountpoint/server/suffix/abc", "Echo", v{"bar"}, nil, nil, v{`method:"Echo",suffix:"suffix/abc",arg:"bar"`}, nil},

			{"mountpoint/server/suffix", "EchoUser", v{"foo", userType("bar")}, nil, nil, v{`method:"EchoUser",suffix:"suffix",arg:"foo"`, userType("bar")}, nil},
			{"mountpoint/server/suffix/abc", "EchoUser", v{"baz", userType("bla")}, nil, nil, v{`method:"EchoUser",suffix:"suffix/abc",arg:"baz"`, userType("bla")}, nil},
			{"mountpoint/server/suffix", "Stream", v{"foo"}, v{userType("bar"), userType("baz")}, nil, v{`method:"Stream",suffix:"suffix",arg:"foo" bar baz`}, nil},
			{"mountpoint/server/suffix/abc", "Stream", v{"123"}, v{userType("456"), userType("789")}, nil, v{`method:"Stream",suffix:"suffix/abc",arg:"123" 456 789`}, nil},
			{"mountpoint/server/suffix", "EchoBlessings", nil, nil, nil, v{"[server]", "[client]"}, nil},
			{"mountpoint/server/suffix", "EchoAndError", v{"bugs bunny"}, nil, nil, v{`method:"EchoAndError",suffix:"suffix",arg:"bugs bunny"`}, nil},
			{"mountpoint/server/suffix", "EchoAndError", v{"error"}, nil, nil, nil, errMethod},
		}
		name = func(t testcase) string {
			return fmt.Sprintf("%s.%s(%v)", t.name, t.method, t.args)
		}

		pclient, pserver = newClientServerPrincipals()
		b                = createBundleWS(t, ctx, pserver, &testServer{}, shouldUseWebsocket)
	)
	defer b.cleanup(t, ctx)
	ctx, _ = v23.SetPrincipal(ctx, pclient)
	for _, test := range tests {
		vlog.VI(1).Infof("%s client.StartCall", name(test))
		vname := test.name
		if shouldUseWebsocket {
			var err error
			vname, err = fakeWSName(ctx, b.ns, vname)
			if err != nil && err != test.startErr {
				t.Errorf(`%s ns.Resolve got error "%v", want "%v"`, name(test), err, test.startErr)
				continue
			}
		}
		call, err := b.client.StartCall(ctx, vname, test.method, test.args)
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
		if got, want := err, test.finishErr; (got == nil) != (want == nil) {
			t.Errorf(`%s call.Finish got error "%v", want "%v'`, name(test), got, want)
		} else if want != nil && !verror.Is(got, verror.ErrorID(want)) {
			t.Errorf(`%s call.Finish got error "%v", want "%v"`, name(test), got, want)
		}
		checkResultPtrs(t, name(test), results, test.results)
	}
}

func TestMultipleFinish(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	type v []interface{}
	var (
		pclient, pserver = newClientServerPrincipals()
		b                = createBundle(t, ctx, pserver, &testServer{})
	)
	defer b.cleanup(t, ctx)
	ctx, _ = v23.SetPrincipal(ctx, pclient)
	call, err := b.client.StartCall(ctx, "mountpoint/server/suffix", "Echo", v{"foo"})
	if err != nil {
		t.Fatalf(`client.StartCall got error "%v"`, err)
	}
	var results string
	err = call.Finish(&results)
	if err != nil {
		t.Fatalf(`call.Finish got error "%v"`, err)
	}
	// Calling Finish a second time should result in a useful error.
	if err = call.Finish(&results); !matchesErrorPattern(err, verror.ErrBadState, "Finish has already been called") {
		t.Fatalf(`got "%v", want "%v"`, err, verror.ErrBadState)
	}
}

// granter implements rpc.Granter, returning a fixed (security.Blessings, error) pair.
type granter struct {
	rpc.CallOpt
	b   security.Blessings
	err error
}

func (g granter) Grant(id security.Blessings) (security.Blessings, error) { return g.b, g.err }

func TestGranter(t *testing.T) {
	var (
		pclient, pserver = newClientServerPrincipals()
		ctx, shutdown    = initForTest()
		b                = createBundle(t, ctx, pserver, &testServer{})
	)
	defer shutdown()
	defer b.cleanup(t, ctx)

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	tests := []struct {
		granter                       rpc.Granter
		startErrID, finishErrID       verror.IDAction
		blessing, starterr, finisherr string
	}{
		{blessing: ""},
		{granter: granter{b: bless(pclient, pserver, "blessed")}, blessing: "client/blessed"},
		{granter: granter{err: errors.New("hell no")}, startErrID: verror.ErrNotTrusted, starterr: "hell no"},
		{granter: granter{b: pclient.BlessingStore().Default()}, finishErrID: verror.ErrNoAccess, finisherr: "blessing granted not bound to this server"},
	}
	for i, test := range tests {
		call, err := b.client.StartCall(ctx, "mountpoint/server/suffix", "EchoGrantedBlessings", []interface{}{"argument"}, test.granter)
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

// dischargeTestServer implements the discharge service. Always fails to
// issue a discharge, but records the impetus and traceid of the RPC call.
type dischargeTestServer struct {
	p       security.Principal
	impetus []security.DischargeImpetus
	traceid []uniqueid.Id
}

func (s *dischargeTestServer) Discharge(call rpc.ServerCall, cav security.Caveat, impetus security.DischargeImpetus) (security.Discharge, error) {
	s.impetus = append(s.impetus, impetus)
	s.traceid = append(s.traceid, vtrace.GetSpan(call.Context()).Trace())
	return security.Discharge{}, fmt.Errorf("discharges not issued")
}

func (s *dischargeTestServer) Release() ([]security.DischargeImpetus, []uniqueid.Id) {
	impetus, traceid := s.impetus, s.traceid
	s.impetus, s.traceid = nil, nil
	return impetus, traceid
}

func TestDischargeImpetusAndContextPropagation(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pserver     = testutil.NewPrincipal("server")
		pdischarger = testutil.NewPrincipal("discharger")
		pclient     = testutil.NewPrincipal("client")
		sm          = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
		ns          = tnaming.NewSimpleNamespace()

		// Setup the client so that it shares a blessing with a third-party caveat with the server.
		setClientBlessings = func(req security.ThirdPartyRequirements) security.Principal {
			cav, err := security.NewPublicKeyCaveat(pdischarger.PublicKey(), "mountpoint/discharger", req, security.UnconstrainedUse())
			if err != nil {
				t.Fatalf("Failed to create ThirdPartyCaveat(%+v): %v", req, err)
			}
			b, err := pclient.BlessSelf("client_for_server", cav)
			if err != nil {
				t.Fatalf("BlessSelf failed: %v", err)
			}
			pclient.BlessingStore().Set(b, "server")
			return pclient
		}
	)
	// Initialize the client principal.
	// It trusts both the application server and the discharger.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	pclient.AddToRoots(pdischarger.BlessingStore().Default())

	// Setup the discharge server.
	var tester dischargeTestServer
	dischargeServer, err := testInternalNewServer(ctx, sm, ns, pdischarger)
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
	appServer, err := testInternalNewServer(ctx, sm, ns, pserver)
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
			Impetus:      security.DischargeImpetus{Server: []security.BlessingPattern{"server"}, Method: "Method", Arguments: []*vdl.Value{vdl.StringValue("argument")}},
		},
		{ // Require only the method name
			Requirements: security.ThirdPartyRequirements{ReportMethod: true},
			Impetus:      security.DischargeImpetus{Method: "Method"},
		},
	}

	for _, test := range tests {
		pclient := setClientBlessings(test.Requirements)
		ctx, _ = v23.SetPrincipal(ctx, pclient)
		client, err := InternalNewClient(sm, ns)
		if err != nil {
			t.Fatalf("InternalNewClient(%+v) failed: %v", test.Requirements, err)
		}
		defer client.Close()
		tid := vtrace.GetSpan(ctx).Trace()
		// StartCall should fetch the discharge, do not worry about finishing the RPC - do not care about that for this test.
		if _, err := client.StartCall(ctx, object, "Method", []interface{}{"argument"}); err != nil {
			t.Errorf("StartCall(%+v) failed: %v", test.Requirements, err)
			continue
		}
		impetus, traceid := tester.Release()
		// There should have been exactly 1 attempt to fetch discharges when making
		// the RPC to the remote object.
		if len(impetus) != 1 || len(traceid) != 1 {
			t.Errorf("Test %+v: Got (%d, %d) (#impetus, #traceid), wanted exactly one", test.Requirements, len(impetus), len(traceid))
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
		pclient, pserver = testutil.NewPrincipal("client"), testutil.NewPrincipal("server")
		pdischarger      = testutil.NewPrincipal("discharger")

		now = time.Now()

		serverName          = "mountpoint/server"
		dischargeServerName = "mountpoint/dischargeserver"

		// Caveats on blessings to the client: First-party caveats
		cavOnlyEcho = mkCaveat(security.MethodCaveat("Echo"))
		cavExpired  = mkCaveat(security.ExpiryCaveat(now.Add(-1 * time.Second)))
		// Caveats on blessings to the client: Third-party caveats
		cavTPValid   = mkThirdPartyCaveat(pdischarger.PublicKey(), dischargeServerName, mkCaveat(security.ExpiryCaveat(now.Add(24*time.Hour))))
		cavTPExpired = mkThirdPartyCaveat(pdischarger.PublicKey(), dischargeServerName, mkCaveat(security.ExpiryCaveat(now.Add(-1*time.Second))))

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
			// - aclAuth suffix: the AccessList only allows blessings matching the patterns "server" or "client"
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
			// the AccessList and the testServerAuthorizer policy.
			{bClient, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, false},
			{bClient, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, true},
			{bClient, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, true},
			{bClient, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, false},

			// The "random" blessing does not satisfy either the default policy or the AccessList, but does
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

	ctx, shutdown := initForTest()
	defer shutdown()
	// Start the main server.
	_, server := startServer(t, ctx, pserver, mgr, ns, serverName, testServerDisp{&testServer{}})
	defer stopServer(t, ctx, server, ns, serverName)

	// Start the discharge server.
	_, dischargeServer := startServer(t, ctx, pdischarger, mgr, ns, dischargeServerName, testutil.LeafDispatcher(&dischargeServer{}, &acceptAllAuthorizer{}))
	defer stopServer(t, ctx, dischargeServer, ns, dischargeServerName)

	// The server should recognize the client principal as an authority on "client" and "random" blessings.
	pserver.AddToRoots(bClient)
	pserver.AddToRoots(bRandom)
	// And the client needs to recognize the server's and discharger's blessings to decide which of its
	// own blessings to share.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	pclient.AddToRoots(pdischarger.BlessingStore().Default())
	// Set a blessing on the client's blessing store to be presented to the discharge server.
	pclient.BlessingStore().Set(pclient.BlessingStore().Default(), "discharger")
	// testutil.NewPrincipal sets up a principal that shares blessings with all servers, undo that.
	pclient.BlessingStore().Set(security.Blessings{}, security.AllPrincipals)

	for i, test := range tests {
		name := fmt.Sprintf("#%d: %q.%s(%v) by %v", i, test.name, test.method, test.args, test.blessings)
		client, err := InternalNewClient(mgr, ns)
		if err != nil {
			t.Fatalf("InternalNewClient failed: %v", err)
		}
		defer client.Close()

		pclient.BlessingStore().Set(test.blessings, "server")
		ctx, _ := v23.SetPrincipal(ctx, pclient)
		call, err := client.StartCall(ctx, test.name, test.method, test.args)
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
		} else if !test.authorized && !verror.Is(err, verror.ErrNoAccess.ID) {
			t.Errorf("%s. call.Finish returned error %v(%v), wanted %v", name, verror.ErrorID(verror.Convert(verror.ErrNoAccess, nil, err)), err, verror.ErrNoAccess)
		}
	}
}

// singleBlessingStore implements security.BlessingStore. It is a
// BlessingStore that marks the last blessing that was set on it as
// shareable with any peer. It does not care about the public key that
// blessing being set is bound to.
type singleBlessingStore struct {
	b security.Blessings
}

func (s *singleBlessingStore) Set(b security.Blessings, _ security.BlessingPattern) (security.Blessings, error) {
	s.b = b
	return security.Blessings{}, nil
}
func (s *singleBlessingStore) ForPeer(...string) security.Blessings {
	return s.b
}
func (*singleBlessingStore) SetDefault(b security.Blessings) error {
	return nil
}
func (*singleBlessingStore) Default() security.Blessings {
	return security.Blessings{}
}
func (*singleBlessingStore) PublicKey() security.PublicKey {
	return nil
}
func (*singleBlessingStore) DebugString() string {
	return ""
}
func (*singleBlessingStore) PeerBlessings() map[security.BlessingPattern]security.Blessings {
	return nil
}

// singleBlessingPrincipal implements security.Principal. It is a wrapper over
// a security.Principal that intercepts  all invocations on the
// principal's BlessingStore and serves them via a singleBlessingStore.
type singleBlessingPrincipal struct {
	security.Principal
	b singleBlessingStore
}

func (p *singleBlessingPrincipal) BlessingStore() security.BlessingStore {
	return &p.b
}

func TestRPCClientBlessingsPublicKey(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pprovider, pserver = testutil.NewPrincipal("root"), testutil.NewPrincipal("server")
		pclient            = &singleBlessingPrincipal{Principal: testutil.NewPrincipal("client")}

		bserver = bless(pprovider, pserver, "server")
		bclient = bless(pprovider, pclient, "client")
		bvictim = bless(pprovider, testutil.NewPrincipal("victim"), "victim")
	)
	// Make the client and server trust blessings from pprovider.
	pclient.AddToRoots(pprovider.BlessingStore().Default())
	pserver.AddToRoots(pprovider.BlessingStore().Default())

	// Make the server present bserver to all clients and start the server.
	pserver.BlessingStore().SetDefault(bserver)
	b := createBundle(t, ctx, pserver, &testServer{})
	defer b.cleanup(t, ctx)

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	tests := []struct {
		blessings security.Blessings
		errID     verror.IDAction
		err       string
	}{
		{blessings: bclient},
		// server disallows clients from authenticating with blessings not bound to
		// the client principal's public key
		{blessings: bvictim, errID: verror.ErrNoAccess, err: "bound to a different public key"},
		{blessings: bserver, errID: verror.ErrNoAccess, err: "bound to a different public key"},
	}
	for i, test := range tests {
		name := fmt.Sprintf("%d: Client RPCing with blessings %v", i, test.blessings)
		pclient.BlessingStore().Set(test.blessings, "root")
		call, err := b.client.StartCall(ctx, "mountpoint/server/suffix", "Closure", nil)
		if err != nil {
			t.Errorf("%v: StartCall failed: %v", name, err)
			continue
		}
		if err := call.Finish(); !matchesErrorPattern(err, test.errID, test.err) {
			t.Errorf("%v: Finish returned error %v", name, err)
			continue
		}
	}
}

func TestServerLocalBlessings(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pprovider, pclient, pserver = testutil.NewPrincipal("root"), testutil.NewPrincipal("client"), testutil.NewPrincipal("server")
		pdischarger                 = pprovider

		mgr = imanager.InternalNew(naming.FixedRoutingID(0x1111111))
		ns  = tnaming.NewSimpleNamespace()

		tpCav = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/dischargeserver", mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))

		bserver = bless(pprovider, pserver, "server", tpCav)
		bclient = bless(pprovider, pclient, "client")
	)
	// Make the client and server principals trust root certificates from
	// pprovider.
	pclient.AddToRoots(pprovider.BlessingStore().Default())
	pserver.AddToRoots(pprovider.BlessingStore().Default())

	// Make the server present bserver to all clients.
	pserver.BlessingStore().SetDefault(bserver)

	// Start the server and the discharger.
	_, server := startServer(t, ctx, pserver, mgr, ns, "mountpoint/server", testServerDisp{&testServer{}})
	defer stopServer(t, ctx, server, ns, "mountpoint/server")

	_, dischargeServer := startServer(t, ctx, pdischarger, mgr, ns, "mountpoint/dischargeserver", testutil.LeafDispatcher(&dischargeServer{}, &acceptAllAuthorizer{}))
	defer stopServer(t, ctx, dischargeServer, ns, "mountpoint/dischargeserver")

	// Make the client present bclient to all servers that are blessed
	// by pprovider.
	pclient.BlessingStore().Set(bclient, "root")
	client, err := InternalNewClient(mgr, ns)
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	defer client.Close()

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	call, err := client.StartCall(ctx, "mountpoint/server/suffix", "EchoBlessings", nil)
	if err != nil {
		t.Fatalf("StartCall failed: %v", err)
	}

	type v []interface{}
	var gotServer, gotClient string
	if err := call.Finish(&gotServer, &gotClient); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}
	if wantServer, wantClient := "[root/server]", "[root/client]"; gotServer != wantServer || gotClient != wantClient {
		t.Fatalf("EchoBlessings: got %v, %v want %v, %v", gotServer, gotClient, wantServer, wantClient)
	}
}

func TestDischargePurgeFromCache(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()

	var (
		pserver     = testutil.NewPrincipal("server")
		pdischarger = pserver // In general, the discharger can be a separate principal. In this test, it happens to be the server.
		pclient     = testutil.NewPrincipal("client")
		// Client is blessed with a third-party caveat. The discharger service issues discharges with a fakeTimeCaveat.
		// This blessing is presented to "server".
		bclient = bless(pserver, pclient, "client", mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/server/discharger", security.UnconstrainedUse()))

		b = createBundle(t, ctx, pserver, &testServer{})
	)
	defer b.cleanup(t, ctx)
	// Setup the client to recognize the server's blessing and present bclient to it.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	pclient.BlessingStore().Set(bclient, "server")

	var err error
	if b.client, err = InternalNewClient(b.sm, b.ns); err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	ctx, _ = v23.SetPrincipal(ctx, pclient)
	call := func() error {
		call, err := b.client.StartCall(ctx, "mountpoint/server/aclAuth", "Echo", []interface{}{"batman"})
		if err != nil {
			return err
		}
		var got string
		if err := call.Finish(&got); err != nil {
			return err
		}
		if want := `method:"Echo",suffix:"aclAuth",arg:"batman"`; got != want {
			return verror.Convert(verror.ErrBadArg, nil, fmt.Errorf("Got [%v] want [%v]", got, want))
		}
		return nil
	}

	// First call should succeed
	if err := call(); err != nil {
		t.Fatal(err)
	}
	// Advance virtual clock, which will invalidate the discharge
	clock.Advance(1)
	if err, want := call(), "not authorized"; !matchesErrorPattern(err, verror.ErrNoAccess, want) {
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

func (s *cancelTestServer) CancelStreamReader(call rpc.StreamServerCall) error {
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
func (s *cancelTestServer) CancelStreamIgnorer(call rpc.StreamServerCall) error {
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
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		ts               = newCancelTestServer(t)
		pclient, pserver = newClientServerPrincipals()
		b                = createBundle(t, ctx, pserver, ts)
	)
	defer b.cleanup(t, ctx)

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	ctx, cancel := context.WithCancel(ctx)
	_, err := b.client.StartCall(ctx, "mountpoint/server/suffix", "CancelStreamReader", []interface{}{})
	if err != nil {
		t.Fatalf("Start call failed: %v", err)
	}
	waitForCancel(t, ts, cancel)
}

// TestCancelWithFullBuffers tests that even if the writer has filled the buffers and
// the server is not reading that the cancel message gets through.
func TestCancelWithFullBuffers(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		ts               = newCancelTestServer(t)
		pclient, pserver = newClientServerPrincipals()
		b                = createBundle(t, ctx, pserver, ts)
	)
	defer b.cleanup(t, ctx)

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	ctx, cancel := context.WithCancel(ctx)
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

func (s *streamRecvInGoroutineServer) RecvInGoroutine(call rpc.StreamServerCall) error {
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
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pclient, pserver = newClientServerPrincipals()
		s                = &streamRecvInGoroutineServer{c: make(chan error, 1)}
		b                = createBundle(t, ctx, pserver, s)
	)
	defer b.cleanup(t, ctx)

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	call, err := b.client.StartCall(ctx, "mountpoint/server/suffix", "RecvInGoroutine", []interface{}{})
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
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pclient, pserver = newClientServerPrincipals()
		b                = createBundle(t, ctx, pserver, &testServer{})
	)
	defer b.cleanup(t, ctx)

	// Publish some incompatible endpoints.
	publisher := publisher.New(ctx, b.ns, publishPeriod)
	defer publisher.WaitForStop()
	defer publisher.Stop()
	publisher.AddName("incompatible")
	publisher.AddServer("/@2@tcp@localhost:10000@@1000000@2000000@@", false)
	publisher.AddServer("/@2@tcp@localhost:10001@@2000000@3000000@@", false)

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	_, err := b.client.StartCall(ctx, "incompatible/suffix", "Echo", []interface{}{"foo"}, options.NoRetry{})
	if !verror.Is(err, verror.ErrNoServers.ID) {
		t.Errorf("Expected error %s, found: %v", verror.ErrNoServers, err)
	}

	// Now add a server with a compatible endpoint and try again.
	publisher.AddServer("/"+b.ep.String(), false)
	publisher.AddName("incompatible")

	call, err := b.client.StartCall(ctx, "incompatible/suffix", "Echo", []interface{}{"foo"})
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
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	pa := func(string, []rpc.Address) ([]rpc.Address, error) {
		a := &net.IPAddr{}
		a.IP = net.ParseIP("1.1.1.1")
		return []rpc.Address{&netstate.AddrIfc{Addr: a}}, nil
	}
	server, err := testInternalNewServer(ctx, sm, ns, testutil.NewPrincipal("server"))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()

	spec := rpc.ListenSpec{
		Addrs:          rpc.ListenAddrs{{"tcp", ":0"}},
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
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	paerr := func(_ string, a []rpc.Address) ([]rpc.Address, error) {
		return nil, fmt.Errorf("oops")
	}
	server, err := testInternalNewServer(ctx, sm, ns, testutil.NewPrincipal("server"))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	spec := rpc.ListenSpec{
		Addrs:          rpc.ListenAddrs{{"tcp", ":0"}},
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
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(naming.FixedRoutingID(0x66666666))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := testInternalNewServer(ctx, sm, ns, nil, options.VCSecurityNone)
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
	if err := call.Finish(&got); err != nil {
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
	if !verror.Is(err, verror.ErrBadArg.ID) {
		t.Errorf("Expected an BadArg error, got: %s", err.Error())
	}
}

func TestServerBlessingsOpt(t *testing.T) {
	var (
		pserver   = testutil.NewPrincipal("server")
		pclient   = testutil.NewPrincipal("client")
		batman, _ = pserver.BlessSelf("batman")
	)
	ctx, shutdown := initForTest()
	defer shutdown()
	// Client and server recognize the servers blessings
	for _, p := range []security.Principal{pserver, pclient} {
		if err := p.AddToRoots(pserver.BlessingStore().Default()); err != nil {
			t.Fatal(err)
		}
		if err := p.AddToRoots(batman); err != nil {
			t.Fatal(err)
		}
	}
	// Start a server that uses the ServerBlessings option to configure itself
	// to act as batman (as opposed to using the default blessing).
	ns := tnaming.NewSimpleNamespace()

	defer runServer(t, ctx, ns, pserver, "mountpoint/batman", &testServer{}, options.ServerBlessings{batman}).Shutdown()
	defer runServer(t, ctx, ns, pserver, "mountpoint/default", &testServer{}).Shutdown()

	// And finally, make an RPC and see that the client sees "batman"
	runClient := func(server string) ([]string, error) {
		smc := imanager.InternalNew(naming.FixedRoutingID(0xc))
		defer smc.Shutdown()
		client, err := InternalNewClient(
			smc,
			ns)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		ctx, _ = v23.SetPrincipal(ctx, pclient)
		call, err := client.StartCall(ctx, server, "Closure", nil)
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

func TestNoDischargesOpt(t *testing.T) {
	var (
		pdischarger = testutil.NewPrincipal("discharger")
		pserver     = testutil.NewPrincipal("server")
		pclient     = testutil.NewPrincipal("client")
	)
	ctx, shutdown := initForTest()
	defer shutdown()
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

	// Setup the disharger and test server.
	discharger := &dischargeServer{}
	defer runServer(t, ctx, ns, pdischarger, "mountpoint/discharger", discharger).Shutdown()
	defer runServer(t, ctx, ns, pserver, "mountpoint/testServer", &testServer{}).Shutdown()

	runClient := func(noDischarges bool) {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		smc := imanager.InternalNew(rid)
		defer smc.Shutdown()
		client, err := InternalNewClient(smc, ns)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}
		defer client.Close()
		var opts []rpc.CallOpt
		if noDischarges {
			opts = append(opts, NoDischarges{})
		}
		ctx, _ = v23.SetPrincipal(ctx, pclient)
		if _, err = client.StartCall(ctx, "mountpoint/testServer", "Closure", nil, opts...); err != nil {
			t.Fatalf("failed to StartCall: %v", err)
		}
	}

	// Test that when the NoDischarges option is set, dischargeServer does not get called.
	if runClient(true); discharger.called {
		t.Errorf("did not expect discharger to be called")
	}
	discharger.called = false
	// Test that when the Nodischarges option is not set, dischargeServer does get called.
	if runClient(false); !discharger.called {
		t.Errorf("expected discharger to be called")
	}
}

func TestNoImplicitDischargeFetching(t *testing.T) {
	// This test ensures that discharge clients only fetch discharges for the specified tp caveats and not its own.
	var (
		pdischarger1     = testutil.NewPrincipal("discharger1")
		pdischarger2     = testutil.NewPrincipal("discharger2")
		pdischargeClient = testutil.NewPrincipal("dischargeClient")
	)
	ctx, shutdown := initForTest()
	defer shutdown()
	// Bless the client with a ThirdPartyCaveat from discharger1.
	tpcav1 := mkThirdPartyCaveat(pdischarger1.PublicKey(), "mountpoint/discharger1", mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))
	blessings, err := pdischarger1.Bless(pdischargeClient.PublicKey(), pdischarger1.BlessingStore().Default(), "tpcav1", tpcav1)
	if err != nil {
		t.Fatalf("failed to create Blessings: %v", err)
	}
	if err = pdischargeClient.BlessingStore().SetDefault(blessings); err != nil {
		t.Fatalf("failed to set blessings: %v", err)
	}
	// The client will only talk to the discharge services if it recognizes them.
	pdischargeClient.AddToRoots(pdischarger1.BlessingStore().Default())
	pdischargeClient.AddToRoots(pdischarger2.BlessingStore().Default())

	ns := tnaming.NewSimpleNamespace()

	// Setup the disharger and test server.
	discharger1 := &dischargeServer{}
	discharger2 := &dischargeServer{}
	defer runServer(t, ctx, ns, pdischarger1, "mountpoint/discharger1", discharger1).Shutdown()
	defer runServer(t, ctx, ns, pdischarger2, "mountpoint/discharger2", discharger2).Shutdown()

	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	sm := imanager.InternalNew(rid)

	c, err := InternalNewClient(sm, ns)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	dc := c.(*client).dc
	tpcav2, err := security.NewPublicKeyCaveat(pdischarger2.PublicKey(), "mountpoint/discharger2", security.ThirdPartyRequirements{}, mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))
	if err != nil {
		t.Error(err)
	}
	ctx, _ = v23.SetPrincipal(ctx, pdischargeClient)
	dc.PrepareDischarges(ctx, []security.Caveat{tpcav2}, security.DischargeImpetus{})

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
		pserver = testutil.NewPrincipal("server")
		pclient = testutil.NewPrincipal("client")
	)
	ctx, shutdown := initForTest()
	defer shutdown()
	// Make the client recognize all server blessings
	if err := pclient.AddToRoots(pserver.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}

	ns := tnaming.NewSimpleNamespace()

	serverSM := runServer(t, ctx, ns, pserver, "mountpoint/testServer", &testServer{})
	defer serverSM.Shutdown()
	rid := serverSM.RoutingID()

	ctx, _ = v23.SetPrincipal(ctx, pclient)

	newClient := func() rpc.Client {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		smc := imanager.InternalNew(rid)
		defer smc.Shutdown()
		client, err := InternalNewClient(smc, ns)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}
		return client
	}

	runClient := func(client rpc.Client) {
		if call, err := client.StartCall(ctx, "mountpoint/testServer", "Closure", nil); err != nil {
			t.Fatalf("failed to StartCall: %v", err)
		} else if err := call.Finish(); err != nil {
			t.Fatal(err)
		}
	}

	cachePrefix := naming.Join("rpc", "server", "routing-id", rid.String(), "security", "blessings", "cache")
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

func TestServerPublicKeyOpt(t *testing.T) {
	var (
		pserver = testutil.NewPrincipal("server")
		pother  = testutil.NewPrincipal("other")
		pclient = testutil.NewPrincipal("client")
	)
	ctx, shutdown := initForTest()
	defer shutdown()
	ns := tnaming.NewSimpleNamespace()
	mountName := "mountpoint/default"

	// Start a server with pserver.
	defer runServer(t, ctx, ns, pserver, mountName, &testServer{}).Shutdown()

	smc := imanager.InternalNew(naming.FixedRoutingID(0xc))
	client, err := InternalNewClient(smc, ns)
	if err != nil {
		t.Fatal(err)
	}
	defer smc.Shutdown()
	defer client.Close()

	ctx, _ = v23.SetPrincipal(ctx, pclient)
	// The call should succeed when the server presents the same public as the opt...
	if _, err = client.StartCall(ctx, mountName, "Closure", nil, options.SkipServerEndpointAuthorization{}, options.ServerPublicKey{pserver.PublicKey()}); err != nil {
		t.Errorf("Expected call to succeed but got %v", err)
	}
	// ...but fail if they differ.
	if _, err = client.StartCall(ctx, mountName, "Closure", nil, options.SkipServerEndpointAuthorization{}, options.ServerPublicKey{pother.PublicKey()}); !verror.Is(err, verror.ErrNotTrusted.ID) {
		t.Errorf("got %v, want %v", verror.ErrorID(err), verror.ErrNotTrusted.ID)
	}
}

type expiryDischarger struct {
	called bool
}

func (ed *expiryDischarger) Discharge(call rpc.StreamServerCall, cav security.Caveat, _ security.DischargeImpetus) (security.Discharge, error) {
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return security.Discharge{}, fmt.Errorf("discharger: %v does not represent a third-party caveat", cav)
	}
	if err := tp.Dischargeable(call); err != nil {
		return security.Discharge{}, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", cav, err)
	}
	expDur := 10 * time.Millisecond
	if ed.called {
		expDur = time.Second
	}
	expiry, err := security.ExpiryCaveat(time.Now().Add(expDur))
	if err != nil {
		return security.Discharge{}, fmt.Errorf("failed to create an expiration on the discharge: %v", err)
	}
	d, err := call.LocalPrincipal().MintDischarge(cav, expiry)
	if err != nil {
		return security.Discharge{}, err
	}
	ed.called = true
	return d, nil
}

func TestDischargeClientFetchExpiredDischarges(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pclient, pdischarger = newClientServerPrincipals()
		tpcav                = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/discharger", mkCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour))))
		ns                   = tnaming.NewSimpleNamespace()
		discharger           = &expiryDischarger{}
	)

	// Setup the disharge server.
	defer runServer(t, ctx, ns, pdischarger, "mountpoint/discharger", discharger).Shutdown()

	// Create a discharge client.
	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	smc := imanager.InternalNew(rid)
	defer smc.Shutdown()
	client, err := InternalNewClient(smc, ns)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()
	ctx, _ = v23.SetPrincipal(ctx, pclient)
	dc := InternalNewDischargeClient(ctx, client, 0)

	// Fetch discharges for tpcav.
	dis := dc.PrepareDischarges(ctx, []security.Caveat{tpcav}, security.DischargeImpetus{})[0]
	// Check that the discharges is not yet expired, but is expired after 100 milliseconds.
	expiry := dis.Expiry()
	// The discharge should expire.
	select {
	case <-time.After(time.Now().Sub(expiry)):
		break
	case <-time.After(time.Second):
		t.Fatalf("discharge didn't expire within a second")
	}
	// Preparing Discharges again to get fresh discharges.
	now := time.Now()
	dis = dc.PrepareDischarges(ctx, []security.Caveat{tpcav}, security.DischargeImpetus{})[0]
	if expiry = dis.Expiry(); expiry.Before(now) {
		t.Fatalf("discharge has expired %v, but should be fresh", dis)
	}
}

// newClientServerPrincipals creates a pair of principals and sets them up to
// recognize each others default blessings.
//
// If the client does not recognize the blessings presented by the server,
// then it will not even send it the request.
//
// If the server does not recognize the blessings presented by the client,
// it is likely to deny access (unless the server authorizes all principals).
func newClientServerPrincipals() (client, server security.Principal) {
	client = testutil.NewPrincipal("client")
	server = testutil.NewPrincipal("server")
	client.AddToRoots(server.BlessingStore().Default())
	server.AddToRoots(client.BlessingStore().Default())
	return
}

func init() {
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
	security.RegisterCaveatValidator(fakeTimeCaveat, func(_ security.Call, t int64) error {
		if now := clock.Now(); now > t {
			return fmt.Errorf("fakeTimeCaveat expired: now=%d > then=%d", now, t)
		}
		return nil
	})
}
