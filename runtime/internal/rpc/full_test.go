// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"fmt"
	"io"
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
	"v.io/v23/flow"
	"v.io/v23/i18n"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/uniqueid"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/ref/lib/pubsub"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/runtime/internal/lib/websocket"
	"v.io/x/ref/runtime/internal/lib/xwebsocket"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/wsh"
	"v.io/x/ref/runtime/internal/rpc/stream"
	imanager "v.io/x/ref/runtime/internal/rpc/stream/manager"
	tnaming "v.io/x/ref/runtime/internal/testing/mocks/naming"
	"v.io/x/ref/test/testutil"
)

//go:generate jiri test generate

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

func testInternalNewServerWithPubsub(ctx *context.T, streamMgr stream.Manager, ns namespace.T, settingsPublisher *pubsub.Publisher, settingsStreamName string, opts ...rpc.ServerOpt) (DeprecatedServer, error) {
	client := DeprecatedNewClient(ctx, streamMgr, ns)
	return DeprecatedNewServer(ctx, streamMgr, ns, settingsPublisher, settingsStreamName, client, opts...)
}

func testInternalNewServer(ctx *context.T, streamMgr stream.Manager, ns namespace.T, opts ...rpc.ServerOpt) (DeprecatedServer, error) {
	return testInternalNewServerWithPubsub(ctx, streamMgr, ns, nil, "", opts...)
}

type userType string

type testServer struct{}

func (*testServer) Closure(*context.T, rpc.ServerCall) error {
	return nil
}

func (*testServer) Error(*context.T, rpc.ServerCall) error {
	return errMethod
}

func (*testServer) Echo(_ *context.T, call rpc.ServerCall, arg string) (string, error) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", "Echo", call.Suffix(), arg), nil
}

func (*testServer) EchoUser(_ *context.T, call rpc.ServerCall, arg string, u userType) (string, userType, error) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", "EchoUser", call.Suffix(), arg), u, nil
}

func (*testServer) EchoLang(ctx *context.T, call rpc.ServerCall) (string, error) {
	return string(i18n.GetLangID(ctx)), nil
}

func (*testServer) EchoBlessings(ctx *context.T, call rpc.ServerCall) (server, client string, _ error) {
	local := security.LocalBlessingNames(ctx, call.Security())
	remote, _ := security.RemoteBlessingNames(ctx, call.Security())
	return fmt.Sprintf("%v", local), fmt.Sprintf("%v", remote), nil
}

func (*testServer) EchoGrantedBlessings(_ *context.T, call rpc.ServerCall, arg string) (result, blessing string, _ error) {
	return arg, fmt.Sprintf("%v", call.GrantedBlessings()), nil
}

func (*testServer) EchoAndError(_ *context.T, call rpc.ServerCall, arg string) (string, error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", "EchoAndError", call.Suffix(), arg)
	if arg == "error" {
		return result, errMethod
	}
	return result, nil
}

func (*testServer) Stream(_ *context.T, call rpc.StreamServerCall, arg string) (string, error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", "Stream", call.Suffix(), arg)
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

func (*testServer) Unauthorized(*context.T, rpc.StreamServerCall) (string, error) {
	return "UnauthorizedResult", nil
}

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(ctx *context.T, call security.Call) error {
	// Verify that the Call object seen by the authorizer
	// has the necessary fields.
	lb := call.LocalBlessings()
	if lb.IsZero() {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalBlessings", call)
	}
	if tpcavs := lb.ThirdPartyCaveats(); len(tpcavs) > 0 && call.LocalDischarges() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalDischarges even when LocalBlessings have third-party caveats", call)

	}
	if call.LocalPrincipal() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalPrincipal", call)
	}
	if call.Method() == "" {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no Method", call)
	}
	if call.LocalEndpoint() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no LocalEndpoint", call)
	}
	if call.RemoteEndpoint() == nil {
		return fmt.Errorf("testServerAuthorzer: Call object %v has no RemoteEndpoint", call)
	}

	// Do not authorize the method "Unauthorized".
	if call.Method() == "Unauthorized" {
		return fmt.Errorf("testServerAuthorizer denied access")
	}
	return nil
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	// If suffix is "nilAuth" we use default authorization, if it is "aclAuth" we
	// use an AccessList-based authorizer, and otherwise we use the custom testServerAuthorizer.
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

func (ds *dischargeServer) Discharge(ctx *context.T, call rpc.StreamServerCall, cav security.Caveat, _ security.DischargeImpetus) (security.Discharge, error) {
	ds.mu.Lock()
	ds.called = true
	ds.mu.Unlock()
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return security.Discharge{}, fmt.Errorf("discharger: %v does not represent a third-party caveat", cav)
	}
	if err := tp.Dischargeable(ctx, call.Security()); err != nil {
		return security.Discharge{}, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", cav, err)
	}
	// Add a fakeTimeCaveat to be able to control discharge expiration via 'clock'.
	expiry, err := security.NewCaveat(fakeTimeCaveat, clock.Now())
	if err != nil {
		return security.Discharge{}, fmt.Errorf("failed to create an expiration on the discharge: %v", err)
	}
	return call.Security().LocalPrincipal().MintDischarge(cav, expiry)
}

func startServer(t *testing.T, ctx *context.T, principal security.Principal, sm stream.Manager, ns namespace.T, name string, disp rpc.Dispatcher, opts ...rpc.ServerOpt) (naming.Endpoint, rpc.Server) {
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

func startServerWS(t *testing.T, ctx *context.T, principal security.Principal, sm stream.Manager, ns namespace.T, name string, disp rpc.Dispatcher, shouldUseWebsocket websocketMode, opts ...rpc.ServerOpt) (naming.Endpoint, rpc.Server) {
	ctx.VI(1).Info("InternalNewServer")
	ctx, _ = v23.WithPrincipal(ctx, principal)
	server, err := testInternalNewServer(ctx, sm, ns, opts...)
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	ctx.VI(1).Info("server.Listen")
	spec := listenSpec
	if shouldUseWebsocket {
		spec = listenWSSpec
	}
	eps, err := server.Listen(spec)
	if err != nil {
		t.Errorf("server.Listen failed: %v", err)
	}
	ctx.VI(1).Info("server.Serve")
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

func verifyMount(t *testing.T, ctx *context.T, ns namespace.T, name string) []string {
	for {
		me, err := ns.Resolve(ctx, name)
		if err == nil {
			return me.Names()
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func verifyMountMissing(t *testing.T, ctx *context.T, ns namespace.T, name string) {
	for {
		if _, err := ns.Resolve(ctx, name); err != nil {
			// Assume that any error (since we're using a mock namespace) means
			// that the name is no longer present.
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stopServer(t *testing.T, ctx *context.T, server rpc.Server, ns namespace.T, name string) {
	ctx.VI(1).Info("server.Stop")
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
	if err == nil || verror.ErrorID(err) != verror.ErrBadState.ID {
		t.Errorf("either no error, or a wrong error was returned: %v", err)
	}
	ctx.VI(1).Info("server.Stop DONE")
}

// fakeWSName creates a name containing a endpoint address that forces
// the use of websockets. It does so by resolving the original name
// and choosing the 'ws' endpoint from the set of endpoints returned.
// It must return a name since it'll be passed to StartCall.
func fakeWSName(ctx *context.T, ns namespace.T, name string) (string, error) {
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
	ns     namespace.T
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
	b.sm = imanager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	b.ns = tnaming.NewSimpleNamespace()
	b.name = "mountpoint/server"
	if server != nil {
		b.ep, b.server = startServerWS(t, ctx, server, b.sm, b.ns, b.name, testServerDisp{ts}, shouldUseWebsocket)
	}
	b.client = DeprecatedNewClient(ctx, b.sm, b.ns)
	return
}

func matchesErrorPattern(err error, id verror.IDAction, pattern string) bool {
	if len(pattern) > 0 && err != nil && strings.Index(err.Error(), pattern) < 0 {
		return false
	}
	if err == nil && id.ID == "" {
		return true
	}
	return verror.ErrorID(err) == id.ID
}

func runServer(t *testing.T, ctx *context.T, ns namespace.T, name string, obj interface{}, opts ...rpc.ServerOpt) stream.Manager {
	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	sm := imanager.InternalNew(ctx, rid)
	server, err := testInternalNewServer(ctx, sm, ns, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Listen(listenSpec); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(name, obj, security.AllowEveryone()); err != nil {
		t.Fatal(err)
	}
	return sm
}

type websocketMode bool
type closeSendMode bool

const (
	useWebsocket websocketMode = true
	noWebsocket  websocketMode = false

	closeSend   closeSendMode = true
	noCloseSend closeSendMode = false
)

// dischargeTestServer implements the discharge service. Always fails to
// issue a discharge, but records the impetus and traceid of the RPC call.
type dischargeTestServer struct {
	p       security.Principal
	impetus []security.DischargeImpetus
	traceid []uniqueid.Id
}

func (s *dischargeTestServer) Discharge(ctx *context.T, _ rpc.ServerCall, cav security.Caveat, impetus security.DischargeImpetus) (security.Discharge, error) {
	s.impetus = append(s.impetus, impetus)
	s.traceid = append(s.traceid, vtrace.GetSpan(ctx).Trace())
	return security.Discharge{}, fmt.Errorf("discharges not issued")
}

func (s *dischargeTestServer) Release() ([]security.DischargeImpetus, []uniqueid.Id) {
	impetus, traceid := s.impetus, s.traceid
	s.impetus, s.traceid = nil, nil
	return impetus, traceid
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
func (*singleBlessingStore) CacheDischarge(security.Discharge, security.Caveat, security.DischargeImpetus) {
	return
}
func (*singleBlessingStore) ClearDischarges(...security.Discharge) {
	return
}
func (*singleBlessingStore) Discharge(security.Caveat, security.DischargeImpetus) security.Discharge {
	return security.Discharge{}
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

func TestSecurityNone(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(ctx, naming.FixedRoutingID(0x66666666))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := testInternalNewServer(ctx, sm, ns, nil, options.SecurityNone)
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
	client := DeprecatedNewClient(ctx, sm, ns)
	// When using SecurityNone, all authorization checks should be skipped, so
	// unauthorized methods should be callable.
	var got string
	if err := client.Call(ctx, "mp/server", "Unauthorized", nil, []interface{}{&got}, options.SecurityNone); err != nil {
		t.Fatalf("client.Call failed: %v", err)
	}
	if want := "UnauthorizedResult"; got != want {
		t.Errorf("got (%v), want (%v)", got, want)
	}
}

func TestAddNameLater(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(ctx, naming.FixedRoutingID(0x66666666))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := testInternalNewServer(ctx, sm, ns, nil, options.SecurityNone)
	if err != nil {
		t.Fatalf("InternalNewServer failed: %v", err)
	}
	if _, err = server.Listen(listenSpec); err != nil {
		t.Fatalf("server.Listen failed: %v", err)
	}
	disp := &testServerDisp{&testServer{}}
	if err := server.ServeDispatcher("", disp); err != nil {
		t.Fatalf("server.Serve failed: %v", err)
	}
	if err := server.AddName("mp/server"); err != nil {
		t.Fatalf("server.AddName failed: %v", err)
	}
	client := DeprecatedNewClient(ctx, sm, ns)
	var got string
	if err := client.Call(ctx, "mp/server", "Unauthorized", nil, []interface{}{&got}, options.SecurityNone); err != nil {
		t.Fatalf("client.Call failed: %v", err)
	}
	if want := "UnauthorizedResult"; got != want {
		t.Errorf("got (%v), want (%v)", got, want)
	}
}

func TestNoPrincipal(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(ctx, naming.FixedRoutingID(0x66666666))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("server"))
	server, err := testInternalNewServer(ctx, sm, ns)
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
	client := DeprecatedNewClient(ctx, sm, ns)

	// A call should fail if the principal in the ctx is nil and SecurityNone is not specified.
	ctx, err = v23.WithPrincipal(ctx, nil)
	if err != nil {
		t.Fatalf("failed to set principal: %v", err)
	}
	_, err = client.StartCall(ctx, "mp/server", "Echo", []interface{}{"foo"})
	if err == nil || verror.ErrorID(err) != errNoPrincipal.ID {
		t.Fatalf("Expected errNoPrincipal, got %v", err)
	}
}

func TestNoDischargesOpt(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pdischarger = testutil.NewPrincipal("discharger")
		pserver     = testutil.NewPrincipal("server")
		pclient     = testutil.NewPrincipal("client")
		cctx, _     = v23.WithPrincipal(ctx, pclient)
		sctx, _     = v23.WithPrincipal(ctx, pserver)
		pctx, _     = v23.WithPrincipal(ctx, pdischarger)
	)

	// Make the client recognize all server blessings
	if err := security.AddToRoots(pclient, pserver.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}
	if err := security.AddToRoots(pclient, pdischarger.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}

	// Bless the client with a ThirdPartyCaveat.
	tpcav := mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/discharger", mkCaveat(security.NewExpiryCaveat(time.Now().Add(time.Hour))))
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
	defer runServer(t, pctx, ns, "mountpoint/discharger", discharger).Shutdown()
	defer runServer(t, sctx, ns, "mountpoint/testServer", &testServer{}).Shutdown()

	runClient := func(noDischarges bool) {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		smc := imanager.InternalNew(ctx, rid)
		defer smc.Shutdown()
		client := DeprecatedNewClient(ctx, smc, ns)
		defer client.Close()
		var opts []rpc.CallOpt
		if noDischarges {
			opts = append(opts, NoDischarges{})
		}
		if _, err = client.StartCall(cctx, "mountpoint/testServer", "Closure", nil, opts...); err != nil {
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
	ctx, shutdown := initForTest()
	defer shutdown()
	// This test ensures that discharge clients only fetch discharges for the specified tp caveats and not its own.
	var (
		pdischarger1     = testutil.NewPrincipal("discharger1")
		pdischarger2     = testutil.NewPrincipal("discharger2")
		pdischargeClient = testutil.NewPrincipal("dischargeClient")
		p1ctx, _         = v23.WithPrincipal(ctx, pdischarger1)
		p2ctx, _         = v23.WithPrincipal(ctx, pdischarger2)
		cctx, _          = v23.WithPrincipal(ctx, pdischargeClient)
	)

	// Bless the client with a ThirdPartyCaveat from discharger1.
	tpcav1 := mkThirdPartyCaveat(pdischarger1.PublicKey(), "mountpoint/discharger1", mkCaveat(security.NewExpiryCaveat(time.Now().Add(time.Hour))))
	blessings, err := pdischarger1.Bless(pdischargeClient.PublicKey(), pdischarger1.BlessingStore().Default(), "tpcav1", tpcav1)
	if err != nil {
		t.Fatalf("failed to create Blessings: %v", err)
	}
	if err = pdischargeClient.BlessingStore().SetDefault(blessings); err != nil {
		t.Fatalf("failed to set blessings: %v", err)
	}
	// The client will only talk to the discharge services if it recognizes them.
	security.AddToRoots(pdischargeClient, pdischarger1.BlessingStore().Default())
	security.AddToRoots(pdischargeClient, pdischarger2.BlessingStore().Default())

	ns := tnaming.NewSimpleNamespace()

	// Setup the disharger and test server.
	discharger1 := &dischargeServer{}
	discharger2 := &dischargeServer{}
	defer runServer(t, p1ctx, ns, "mountpoint/discharger1", discharger1).Shutdown()
	defer runServer(t, p2ctx, ns, "mountpoint/discharger2", discharger2).Shutdown()

	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	sm := imanager.InternalNew(ctx, rid)

	c := DeprecatedNewClient(ctx, sm, ns)
	dc := c.(*client).dc
	tpcav2, err := security.NewPublicKeyCaveat(pdischarger2.PublicKey(), "mountpoint/discharger2", security.ThirdPartyRequirements{}, mkCaveat(security.NewExpiryCaveat(time.Now().Add(time.Hour))))
	if err != nil {
		t.Error(err)
	}
	dc.PrepareDischarges(cctx, []security.Caveat{tpcav2}, security.DischargeImpetus{})

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
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pserver = testutil.NewPrincipal("server")
		pclient = testutil.NewPrincipal("client")
		cctx, _ = v23.WithPrincipal(ctx, pclient)
		sctx, _ = v23.WithPrincipal(ctx, pserver)
	)

	// Make the client recognize all server blessings
	if err := security.AddToRoots(pclient, pserver.BlessingStore().Default()); err != nil {
		t.Fatal(err)
	}

	ns := tnaming.NewSimpleNamespace()

	serverSM := runServer(t, sctx, ns, "mountpoint/testServer", &testServer{})
	defer serverSM.Shutdown()
	rid := serverSM.RoutingID()

	newClient := func() rpc.Client {
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		smc := imanager.InternalNew(sctx, rid)
		defer smc.Shutdown()
		return DeprecatedNewClient(ctx, smc, ns)
	}

	runClient := func(client rpc.Client) {
		if err := client.Call(cctx, "mountpoint/testServer", "Closure", nil, nil); err != nil {
			t.Fatalf("failed to Call: %v", err)
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
	blessings, err := pserver.Bless(pclient.PublicKey(), pserver.BlessingStore().Default(), "cav", mkCaveat(security.NewExpiryCaveat(time.Now().Add(time.Hour))))
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

func TestServerAuthorizerOpt(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	var (
		pserver = testutil.NewPrincipal("server")
		pother  = testutil.NewPrincipal("other")
		pclient = testutil.NewPrincipal("client")
		cctx, _ = v23.WithPrincipal(ctx, pclient)
		sctx, _ = v23.WithPrincipal(ctx, pserver)
	)

	ns := tnaming.NewSimpleNamespace()
	mountName := "mountpoint/default"

	// Start a server with pserver.
	defer runServer(t, sctx, ns, mountName, &testServer{}).Shutdown()

	smc := imanager.InternalNew(sctx, naming.FixedRoutingID(0xc))
	client := DeprecatedNewClient(ctx, smc, ns)
	defer smc.Shutdown()
	defer client.Close()

	authopt := func(k security.PublicKey) options.ServerAuthorizer {
		return options.ServerAuthorizer{security.PublicKeyAuthorizer(k)}
	}
	// The call should succeed when the server presents the same public as the opt...
	var err error
	if _, err = client.StartCall(cctx, mountName, "Closure", nil, authopt(pserver.PublicKey())); err != nil {
		t.Errorf("Expected call to succeed but got %v", err)
	}
	// ...but fail if they differ.
	if _, err = client.StartCall(cctx, mountName, "Closure", nil, authopt(pother.PublicKey())); verror.ErrorID(err) != verror.ErrNotTrusted.ID {
		t.Errorf("got %v, want %v", verror.ErrorID(err), verror.ErrNotTrusted.ID)
	}
}

type expiryDischarger struct {
	called bool
}

func (ed *expiryDischarger) Discharge(ctx *context.T, call rpc.StreamServerCall, cav security.Caveat, _ security.DischargeImpetus) (security.Discharge, error) {
	tp := cav.ThirdPartyDetails()
	if tp == nil {
		return security.Discharge{}, fmt.Errorf("discharger: %v does not represent a third-party caveat", cav)
	}
	if err := tp.Dischargeable(ctx, call.Security()); err != nil {
		return security.Discharge{}, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", cav, err)
	}
	expDur := 10 * time.Millisecond
	if ed.called {
		expDur = time.Second
	}
	expiry, err := security.NewExpiryCaveat(time.Now().Add(expDur))
	if err != nil {
		return security.Discharge{}, fmt.Errorf("failed to create an expiration on the discharge: %v", err)
	}
	d, err := call.Security().LocalPrincipal().MintDischarge(cav, expiry)
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
		tpcav                = mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/discharger", mkCaveat(security.NewExpiryCaveat(time.Now().Add(time.Hour))))
		ns                   = tnaming.NewSimpleNamespace()
		discharger           = &expiryDischarger{}
		pctx, _              = v23.WithPrincipal(ctx, pdischarger)
	)
	ctx, _ = v23.WithPrincipal(ctx, pclient)

	// Setup the disharge server.
	defer runServer(t, pctx, ns, "mountpoint/discharger", discharger).Shutdown()

	// Create a discharge client.
	rid, err := naming.NewRoutingID()
	if err != nil {
		t.Fatal(err)
	}
	smc := imanager.InternalNew(ctx, rid)
	defer smc.Shutdown()
	client := DeprecatedNewClient(ctx, smc, ns)
	defer client.Close()

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
	security.AddToRoots(client, server.BlessingStore().Default())
	security.AddToRoots(server, client.BlessingStore().Default())
	return
}

func init() {
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener)
	flow.RegisterUnknownProtocol("wsh", xwebsocket.WSH{})
	security.RegisterCaveatValidator(fakeTimeCaveat, func(_ *context.T, _ security.Call, t int64) error {
		if now := clock.Now(); now > t {
			return fmt.Errorf("fakeTimeCaveat expired: now=%d > then=%d", now, t)
		}
		return nil
	})
}

func TestServerStates(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	sm := imanager.InternalNew(ctx, naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	sctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("test"))
	expectBadState := func(err error) {
		if verror.ErrorID(err) != verror.ErrBadState.ID {
			t.Fatalf("%s: unexpected error: %v", loc(1), err)
		}
	}

	expectNoError := func(err error) {
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", loc(1), err)
		}
	}

	server, err := testInternalNewServer(sctx, sm, ns)
	expectNoError(err)
	defer server.Stop()

	expectState := func(s rpc.ServerState) {
		if got, want := server.Status().State, s; got != want {
			t.Fatalf("%s: got %s, want %s", loc(1), got, want)
		}
	}

	expectState(rpc.ServerActive)

	// Need to call Listen first.
	err = server.Serve("", &testServer{}, nil)
	expectBadState(err)
	err = server.AddName("a")
	expectBadState(err)

	_, err = server.Listen(rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}})
	expectNoError(err)

	expectState(rpc.ServerActive)

	err = server.Serve("", &testServer{}, nil)
	expectNoError(err)

	err = server.Serve("", &testServer{}, nil)
	expectBadState(err)

	expectState(rpc.ServerActive)

	err = server.AddName("a")
	expectNoError(err)

	expectState(rpc.ServerActive)

	server.RemoveName("a")

	expectState(rpc.ServerActive)

	err = server.Stop()
	expectNoError(err)
	err = server.Stop()
	expectNoError(err)

	err = server.AddName("a")
	expectBadState(err)
}
