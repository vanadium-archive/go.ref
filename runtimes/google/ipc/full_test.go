package ipc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/netstate"
	_ "veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/lib/testutil/blackbox"
	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"
	"veyron.io/veyron/veyron/profiles"
	imanager "veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/proxy"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	"veyron.io/veyron/veyron/runtimes/google/ipc/version"
	"veyron.io/veyron/veyron/runtimes/google/lib/publisher"
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
	isecurity "veyron.io/veyron/veyron/runtimes/google/security"
	tnaming "veyron.io/veyron/veyron/runtimes/google/testing/mocks/naming"
	vsecurity "veyron.io/veyron/veyron/security"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
)

var (
	errAuthorizer = errors.New("ipc: application Authorizer denied access")
	errMethod     = verror.Abortedf("server returned an error")
	clientID      = newID("client")
	serverID      = newID("server")
	clock         = new(fakeClock)
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

type fakeTimeCaveat int

func (c fakeTimeCaveat) Validate(security.Context) error {
	now := clock.Now()
	if now > int(c) {
		return fmt.Errorf("fakeTimeCaveat expired: now=%d > then=%d", now, c)
	}
	return nil
}

type userType string

type testServer struct{}

func (*testServer) Closure(call ipc.ServerCall) {
}

func (*testServer) Error(call ipc.ServerCall) error {
	return errMethod
}

func (*testServer) Echo(call ipc.ServerCall, arg string) string {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", call.Method(), call.Suffix(), arg)
}

func (*testServer) EchoUser(call ipc.ServerCall, arg string, u userType) (string, userType) {
	return fmt.Sprintf("method:%q,suffix:%q,arg:%q", call.Method(), call.Suffix(), arg), u
}

func (*testServer) EchoIDs(call ipc.ServerCall) (server, client string) {
	return fmt.Sprintf("%v", call.LocalID()), fmt.Sprintf("%v", call.RemoteID())
}

func (*testServer) EchoBlessing(call ipc.ServerCall, arg string) (result, blessing string) {
	return arg, fmt.Sprintf("%v", call.Blessing())
}

func (*testServer) EchoAndError(call ipc.ServerCall, arg string) (string, error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", call.Method(), call.Suffix(), arg)
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

type dischargeServer struct{}

func (*dischargeServer) Discharge(ctx ipc.ServerCall, cav vdlutil.Any, _ security.DischargeImpetus) (vdlutil.Any, error) {
	c, ok := cav.(security.ThirdPartyCaveat)
	if !ok {
		return nil, fmt.Errorf("discharger: unknown caveat(%T)", cav)
	}
	// Add a fakeTimeCaveat to allow the discharge to expire
	expiry := fakeTimeCaveat(clock.Now())
	if err := c.Dischargeable(ctx); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", c, err)
	}
	return serverID.MintDischarge(c, ctx, time.Hour, []security.Caveat{newCaveat(expiry)})
}

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(c security.Context) error {
	if c.Method() != "Unauthorized" {
		return nil
	}
	return errAuthorizer
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	// If suffix is "nilAuth" we use default authorization, if it is "aclAuth" we
	// use an ACL based authorizer, and otherwise we use the custom testServerAuthorizer.
	var authorizer security.Authorizer
	switch suffix {
	case "discharger":
		return ipc.ReflectInvoker(&dischargeServer{}), testServerAuthorizer{}, nil
	case "nilAuth":
		authorizer = nil
	case "aclAuth":
		// Only authorize clients matching patterns "client" or "server/...".
		authorizer = vsecurity.NewACLAuthorizer(security.ACL{In: map[security.BlessingPattern]security.LabelSet{
			"server/...": security.LabelSet(security.AdminLabel),
			"client":     security.LabelSet(security.AdminLabel),
		}})
	default:
		authorizer = testServerAuthorizer{}
	}
	return ipc.ReflectInvoker(t.server), authorizer, nil
}

func startServer(t *testing.T, serverID security.PrivateID, sm stream.Manager, ns naming.Namespace, ts interface{}) (naming.Endpoint, ipc.Server) {
	vlog.VI(1).Info("InternalNewServer")
	server, err := InternalNewServer(testContext(), sm, ns, vc.FixedLocalID(serverID))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	vlog.VI(1).Info("server.Listen")
	ep, err := server.ListenX(profiles.LocalListenSpec)
	if err != nil {
		t.Errorf("server.Listen failed: %v", err)
	}
	vlog.VI(1).Info("server.Serve")
	disp := testServerDisp{ts}
	if err := server.Serve("mountpoint/server", disp); err != nil {
		t.Errorf("server.Publish failed: %v", err)
	}
	if err := server.Serve("mountpoint/discharger", disp); err != nil {
		t.Errorf("server.Publish for discharger failed: %v", err)
	}
	return ep, server
}

func verifyMount(t *testing.T, ns naming.Namespace, name string) {
	if _, err := ns.Resolve(testContext(), name); err != nil {
		t.Errorf("%s not found in mounttable", name)
	}
}

func verifyMountMissing(t *testing.T, ns naming.Namespace, name string) {
	if servers, err := ns.Resolve(testContext(), name); err == nil {
		t.Errorf("%s not supposed to be found in mounttable; got %d servers instead", name, len(servers))
	}
}

func stopServer(t *testing.T, server ipc.Server, ns naming.Namespace) {
	vlog.VI(1).Info("server.Stop")
	n1 := "mountpoint/server"
	n2 := "should_appear_in_mt/server"
	verifyMount(t, ns, n1)

	// publish a second name
	if err := server.Serve(n2, nil); err != nil {
		t.Errorf("server.Serve failed: %v", err)
	}
	verifyMount(t, ns, n2)

	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}

	verifyMountMissing(t, ns, n1)
	verifyMountMissing(t, ns, n2)

	// Check that we can no longer serve after Stop.
	err := server.Serve("name doesn't matter", nil)
	if err == nil || err.Error() != "ipc: server is stopped" {
		t.Errorf("either no error, or a wrong error was returned: %v", err)
	}
	vlog.VI(1).Info("server.Stop DONE")
}

type bundle struct {
	client ipc.Client
	server ipc.Server
	ep     naming.Endpoint
	ns     naming.Namespace
	sm     stream.Manager
}

func (b bundle) cleanup(t *testing.T) {
	if b.server != nil {
		stopServer(t, b.server, b.ns)
	}
	if b.client != nil {
		b.client.Close()
	}
}

func createBundle(t *testing.T, clientID, serverID security.PrivateID, ts interface{}) (b bundle) {
	b.sm = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	b.ns = tnaming.NewSimpleNamespace()
	if serverID != nil {
		b.ep, b.server = startServer(t, serverID, b.sm, b.ns, ts)
	}
	if clientID != nil {
		var err error
		if b.client, err = InternalNewClient(b.sm, b.ns, vc.FixedLocalID(clientID)); err != nil {
			t.Fatalf("InternalNewClient failed: %v", err)
		}
	}
	return
}

func bless(blessor security.PrivateID, blessee security.PublicID, name string, caveats ...security.Caveat) security.PublicID {
	blessed, err := blessor.Bless(blessee, name, 24*time.Hour, caveats)
	if err != nil {
		panic(err)
	}
	return blessed
}

func derive(blessor security.PrivateID, name string, caveats ...security.Caveat) security.PrivateID {
	id := newID("irrelevant")
	derivedID, err := id.Derive(bless(blessor, id.PublicID(), name, caveats...))
	if err != nil {
		panic(err)
	}
	return derivedID
}

// deriveForThirdPartyCaveats creates a SetPrivateID that can be used for
//  1. talking to the server, if the caveats are fulfilled
//  2. getting discharges, even if the caveats are not fulfilled
// As an identity with an unfulfilled caveat is invalid (even for asking for  a
// discharge), this function creates a set of two identities. The first will
// have the caveats, the second will always be valid, but only for getting
// discharges. The client presents both blessings in both cases, the discharger
// ignores the first if it is invalid.
func deriveForThirdPartyCaveats(blessor security.PrivateID, name string, caveats ...security.Caveat) security.PrivateID {
	id := derive(blessor, name, caveats...)
	dischargeID, err := id.Derive(bless(blessor, id.PublicID(), name, mkCaveat(security.MethodCaveat("Discharge"))))
	if err != nil {
		panic(err)
	}
	id, err = isecurity.NewSetPrivateID(id, dischargeID)
	if err != nil {
		panic(err)
	}
	return id
}

func matchesErrorPattern(err error, pattern string) bool {
	if (len(pattern) == 0) != (err == nil) {
		return false
	}
	return err == nil || strings.Index(err.Error(), pattern) >= 0
}

func TestMultipleCallsToServe(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), sm, ns, vc.FixedLocalID(serverID))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	_, err = server.ListenX(profiles.LocalListenSpec)
	if err != nil {
		t.Errorf("server.Listen failed: %v", err)
	}

	disp := &testServerDisp{&testServer{}}
	if err := server.Serve("mountpoint/server", disp); err != nil {
		t.Errorf("server.Publish failed: %v", err)
	}

	n1 := "mountpoint/server"
	n2 := "should_appear_in_mt/server"
	n3 := "should_appear_in_mt/server"
	n4 := "should_not_appear_in_mt/server"

	verifyMount(t, ns, n1)

	if err := server.Serve(n2, disp); err != nil {
		t.Errorf("server.Serve failed: %v", err)
	}
	if err := server.Serve(n3, nil); err != nil {
		t.Errorf("server.Serve failed: %v", err)
	}
	verifyMount(t, ns, n2)
	verifyMount(t, ns, n3)

	if err := server.Serve(n4, &testServerDisp{&testServer{}}); err == nil {
		t.Errorf("server.Serve should have failed")
	}
	verifyMountMissing(t, ns, n4)

	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}

	verifyMountMissing(t, ns, n1)
	verifyMountMissing(t, ns, n2)
	verifyMountMissing(t, ns, n3)
}

func TestStartCall(t *testing.T) {
	const (
		authorizeErr = "not authorized because"
		nameErr      = "does not match the provided pattern"
	)

	var (
		cavOnlyV1, _    = security.PeerBlessingsCaveat("client/v1")
		cavExpired, _   = security.ExpiryCaveat(time.Now().Add(-1 * time.Second))
		clientV1ID      = derive(clientID, "v1")
		clientV2ID      = derive(clientID, "v2")
		serverV1ID      = derive(serverID, "v1", cavOnlyV1)
		serverExpiredID = derive(serverID, "expired", cavExpired)
	)

	tests := []struct {
		clientID, serverID security.PrivateID
		pattern            security.BlessingPattern // pattern on the server identity expected by client.
		err                string
	}{
		// Client accepts talking to server only if server's identity matches the
		// provided pattern.
		{clientID, serverID, security.AllPrincipals, ""},
		{clientID, serverID, "server", ""},
		{clientID, serverID, "server/v1", ""},
		{clientID, serverID, "anotherServer", nameErr},

		// All clients reject talking to a server with an expired identity.
		{clientID, serverExpiredID, security.AllPrincipals, authorizeErr},
		{clientV1ID, serverExpiredID, security.AllPrincipals, authorizeErr},
		{clientV2ID, serverExpiredID, security.AllPrincipals, authorizeErr},

		// Only clientV1 accepts talking to serverV1.
		{clientV1ID, serverV1ID, security.AllPrincipals, ""},
		{clientV2ID, serverV1ID, security.AllPrincipals, authorizeErr},
	}
	// Servers and clients will be created per-test, use the same stream manager and mounttable.
	mgr := imanager.InternalNew(naming.FixedRoutingID(0x1111111))
	ns := tnaming.NewSimpleNamespace()
	for _, test := range tests {
		name := fmt.Sprintf("(clientID:%q serverID:%q)", test.clientID, test.serverID)
		_, server := startServer(t, test.serverID, mgr, ns, &testServer{})
		client, err := InternalNewClient(mgr, ns, vc.FixedLocalID(test.clientID))
		if err != nil {
			t.Errorf("%s: Client creation failed: %v", name, err)
			stopServer(t, server, ns)
			continue
		}
		if call, err := client.StartCall(testContext(), "mountpoint/server/suffix", "irrelevant", nil, veyron2.RemoteID(test.pattern)); !matchesErrorPattern(err, test.err) {
			t.Errorf(`%s: client.StartCall: got error "%v", want to match "%v"`, name, err, test.err)
		} else if call != nil {
			serverBlessings, _ := call.RemoteBlessings()
			if !reflect.DeepEqual(serverBlessings, test.serverID.PublicID().Names()) {
				t.Errorf("%s: Server authenticated as %v, wanted %v", name, serverBlessings, test.serverID.PublicID().Names())
			}
		}
		client.Close()
		stopServer(t, server, ns)
	}
}

func TestRPC(t *testing.T) {
	testRPC(t, true)
}

// TestCloseSendOnFinish tests that Finish informs the server that no more
// inputs will be sent by the client if CloseSend has not already done so.
func TestRPCCloseSendOnFinish(t *testing.T) {
	testRPC(t, false)
}

func testRPC(t *testing.T, shouldCloseSend bool) {
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
	tests := []testcase{
		{"mountpoint/server/suffix", "Closure", nil, nil, nil, nil, nil},
		{"mountpoint/server/suffix", "Error", nil, nil, nil, v{errMethod}, nil},

		{"mountpoint/server/suffix", "Echo", v{"foo"}, nil, nil, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, nil},
		{"mountpoint/server/suffix/abc", "Echo", v{"bar"}, nil, nil, v{`method:"Echo",suffix:"suffix/abc",arg:"bar"`}, nil},

		{"mountpoint/server/suffix", "EchoUser", v{"foo", userType("bar")}, nil, nil, v{`method:"EchoUser",suffix:"suffix",arg:"foo"`, userType("bar")}, nil},
		{"mountpoint/server/suffix/abc", "EchoUser", v{"baz", userType("bla")}, nil, nil, v{`method:"EchoUser",suffix:"suffix/abc",arg:"baz"`, userType("bla")}, nil},
		{"mountpoint/server/suffix", "Stream", v{"foo"}, v{userType("bar"), userType("baz")}, nil, v{`method:"Stream",suffix:"suffix",arg:"foo" bar baz`, nil}, nil},
		{"mountpoint/server/suffix/abc", "Stream", v{"123"}, v{userType("456"), userType("789")}, nil, v{`method:"Stream",suffix:"suffix/abc",arg:"123" 456 789`, nil}, nil},
		{"mountpoint/server/suffix", "EchoIDs", nil, nil, nil, v{"server", "client"}, nil},
		{"mountpoint/server/suffix", "EchoAndError", v{"bugs bunny"}, nil, nil, v{`method:"EchoAndError",suffix:"suffix",arg:"bugs bunny"`, nil}, nil},
		{"mountpoint/server/suffix", "EchoAndError", v{"error"}, nil, nil, v{`method:"EchoAndError",suffix:"suffix",arg:"error"`, errMethod}, nil},
	}
	name := func(t testcase) string {
		return fmt.Sprintf("%s.%s(%v)", t.name, t.method, t.args)
	}
	b := createBundle(t, clientID, serverID, &testServer{})
	defer b.cleanup(t)
	for _, test := range tests {
		vlog.VI(1).Infof("%s client.StartCall", name(test))
		call, err := b.client.StartCall(testContext(), test.name, test.method, test.args)
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
	b := createBundle(t, clientID, serverID, &testServer{})
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
	err = call.Finish(&results)
	if got, want := err, verror.BadProtocolf("ipc: multiple calls to Finish not allowed"); got != want {
		t.Fatalf(`call.Finish got error "%v", want "%v"`, got, want)
	}
}

// granter implements ipc.Granter, returning a fixed (security.PublicID, error) pair.
type granter struct {
	ipc.CallOpt
	id  security.PublicID
	err error
}

func (g granter) Grant(id security.PublicID) (security.PublicID, error) { return g.id, g.err }

func TestBlessing(t *testing.T) {
	b := createBundle(t, clientID, serverID, &testServer{})
	defer b.cleanup(t)

	tests := []struct {
		granter                       ipc.CallOpt
		blessing, starterr, finisherr string
	}{
		{blessing: "<nil>"},
		{granter: granter{id: bless(clientID, serverID.PublicID(), "blessed")}, blessing: "client/blessed"},
		{granter: granter{err: errors.New("hell no")}, starterr: "hell no"},
		{granter: granter{id: clientID.PublicID()}, finisherr: "blessing provided not bound to this server"},
	}
	for _, test := range tests {
		call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "EchoBlessing", []interface{}{"argument"}, test.granter)
		if !matchesErrorPattern(err, test.starterr) {
			t.Errorf("%+v: StartCall returned error %v", test, err)
		}
		if err != nil {
			continue
		}
		var result, blessing string
		if err = call.Finish(&result, &blessing); !matchesErrorPattern(err, test.finisherr) {
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

func mkThirdPartyCaveat(discharger security.PublicID, location string, c security.Caveat) security.Caveat {
	tpc, err := security.NewPublicKeyCaveat(discharger.PublicKey(), location, security.ThirdPartyRequirements{}, c)
	if err != nil {
		panic(err)
	}
	return newCaveat(tpc)
}

type dischargeImpetusTester struct {
	LastDischargeImpetus security.DischargeImpetus
}

// Implements ipc.Dispatcher
func (s *dischargeImpetusTester) Lookup(_, _ string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(s), nil, nil
}

// Implements the discharge service: Always fails to issue a discharge, but records the impetus
func (s *dischargeImpetusTester) Discharge(ctx ipc.ServerCall, cav vdlutil.Any, impetus security.DischargeImpetus) (vdlutil.Any, error) {
	s.LastDischargeImpetus = impetus
	return nil, fmt.Errorf("discharges not issued")
}

func names2patterns(names []string) []security.BlessingPattern {
	ret := make([]security.BlessingPattern, len(names))
	for idx, n := range names {
		ret[idx] = security.BlessingPattern(n)
	}
	return ret
}

func TestDischargeImpetus(t *testing.T) {
	var (
		// The Discharge service can be run by anyone, but in these tests it is the same as the server.
		dischargerID = serverID.PublicID()

		mkClientID = func(req security.ThirdPartyRequirements) security.PrivateID {
			tpc, err := security.NewPublicKeyCaveat(dischargerID.PublicKey(), "mountpoint/discharger", req, newCaveat(alwaysValidCaveat{}))
			if err != nil {
				t.Fatalf("Failed to create ThirdPartyCaveat: %v", err)
			}
			return deriveForThirdPartyCaveats(serverID, "client", newCaveat(tpc))
		}
	)
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), sm, ns, vc.FixedLocalID(serverID))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	if _, err := server.ListenX(profiles.LocalListenSpec); err != nil {
		t.Fatal(err)
	}

	var tester dischargeImpetusTester
	if err := server.Serve("mountpoint", &tester); err != nil {
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
			Impetus:      security.DischargeImpetus{Server: names2patterns(serverID.PublicID().Names()), Method: "Method", Arguments: []vdlutil.Any{vdlutil.Any("argument")}},
		},
		{ // Require only the method name
			Requirements: security.ThirdPartyRequirements{ReportMethod: true},
			Impetus:      security.DischargeImpetus{Method: "Method"},
		},
	}

	for _, test := range tests {
		client, err := InternalNewClient(sm, ns, vc.FixedLocalID(mkClientID(test.Requirements)))
		if err != nil {
			t.Fatalf("InternalNewClient(%+v) failed: %v", test.Requirements, err)
		}
		defer client.Close()
		// StartCall should fetch the discharge, do not worry about finishing the RPC - do not care about that for this test.
		if _, err := client.StartCall(testContext(), "mountpoint/object", "Method", []interface{}{"argument"}); err != nil {
			t.Errorf("StartCall(%+v) failed: %v", test.Requirements, err)
			continue
		}
		if got, want := tester.LastDischargeImpetus, test.Impetus; !reflect.DeepEqual(got, want) {
			t.Errorf("Got [%v] want [%v] for test %+v", got, want, test.Requirements)
		}
	}
}

func TestRPCAuthorization(t *testing.T) {
	var (
		now = time.Now()
		// First-party caveats
		cavOnlyEcho = mkCaveat(security.MethodCaveat("Echo"))
		cavExpired  = mkCaveat(security.ExpiryCaveat(now))
		// Third-party caveats
		// The Discharge service can be run by any identity, but in our tests the same server runs
		// a Discharge service as well.
		dischargerID = serverID.PublicID()
		cavTPValid   = mkThirdPartyCaveat(dischargerID, "mountpoint/server/discharger", mkCaveat(security.ExpiryCaveat(now.Add(24*time.Hour))))
		cavTPExpired = mkThirdPartyCaveat(dischargerID, "mountpoint/server/discharger", mkCaveat(security.ExpiryCaveat(now)))

		// Client blessings that will be tested
		blessedByServerOnlyEcho  = derive(serverID, "onlyEcho", cavOnlyEcho)
		blessedByServerExpired   = derive(serverID, "expired", cavExpired)
		blessedByServerTPValid   = deriveForThirdPartyCaveats(serverID, "tpvalid", cavTPValid)
		blessedByServerTPExpired = deriveForThirdPartyCaveats(serverID, "tpexpired", cavTPExpired)
		blessedByClient          = derive(clientID, "blessed")
	)
	const (
		expiredIDErr = "security.unixTimeExpiryCaveat"
		aclAuthErr   = "no matching ACL entry found"
	)
	invalidMethodErr := func(method string) string {
		return fmt.Sprintf(`security.methodCaveat=[Echo] fails validation for method %q`, method)
	}

	type v []interface{}
	type testcase struct {
		clientID  security.PrivateID
		name      string
		method    string
		args      v
		results   v
		finishErr string
	}
	tests := []testcase{
		// Clients whose identities have invalid caveats are not by authorized by any authorizer.
		{blessedByServerExpired, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, expiredIDErr},
		{blessedByServerExpired, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, expiredIDErr},
		{blessedByServerOnlyEcho, "mountpoint/server/nilAuth", "Closure", nil, nil, invalidMethodErr("Closure")},
		{blessedByServerOnlyEcho, "mountpoint/server/suffix", "Closure", nil, nil, invalidMethodErr("Closure")},
		// Only clients with a trusted name that matches either the server's identity or an identity blessed
		// by the server are authorized by the (default) nilAuth authorizer.
		{clientID, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, aclAuthErr},
		{blessedByClient, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{""}, aclAuthErr},
		{serverID, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{`method:"Echo",suffix:"nilAuth",arg:"foo"`}, ""},
		{serverID, "mountpoint/server/nilAuth", "Closure", nil, nil, ""},
		{blessedByServerOnlyEcho, "mountpoint/server/nilAuth", "Echo", v{"foo"}, v{`method:"Echo",suffix:"nilAuth",arg:"foo"`}, ""},
		// Only clients matching the server's ACL are authorized.
		{clientID, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{`method:"Echo",suffix:"aclAuth",arg:"foo"`}, ""},
		{blessedByClient, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{""}, aclAuthErr},
		{serverID, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{`method:"Echo",suffix:"aclAuth",arg:"foo"`}, ""},
		{blessedByServerOnlyEcho, "mountpoint/server/aclAuth", "Echo", v{"foo"}, v{`method:"Echo",suffix:"aclAuth",arg:"foo"`}, ""},
		{clientID, "mountpoint/server/aclAuth", "Closure", nil, nil, ""},
		{blessedByClient, "mountpoint/server/aclAuth", "Closure", nil, nil, aclAuthErr},
		{serverID, "mountpoint/server/aclAuth", "Closure", nil, nil, ""},
		// All methods except "Unauthorized" are authorized by the custom authorizer.
		{clientID, "mountpoint/server/suffix", "Echo", v{"foo"}, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, ""},
		{blessedByClient, "mountpoint/server/suffix", "Echo", v{"foo"}, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, ""},
		{serverID, "mountpoint/server/suffix", "Echo", v{"foo"}, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, ""},
		{blessedByServerOnlyEcho, "mountpoint/server/suffix", "Echo", v{"foo"}, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, ""},
		{clientID, "mountpoint/server/suffix", "Closure", nil, nil, ""},
		{blessedByClient, "mountpoint/server/suffix", "Closure", nil, nil, ""},
		{serverID, "mountpoint/server/suffix", "Closure", nil, nil, ""},
		{clientID, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, "application Authorizer denied access"},
		{blessedByClient, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, "application Authorizer denied access"},
		{serverID, "mountpoint/server/suffix", "Unauthorized", nil, v{""}, "application Authorizer denied access"},
		// Third-party caveat discharges should be fetched and forwarded
		{blessedByServerTPValid, "mountpoint/server/suffix", "Echo", v{"foo"}, v{`method:"Echo",suffix:"suffix",arg:"foo"`}, ""},
		{blessedByServerTPExpired, "mountpoint/server/suffix", "Echo", v{"foo"}, v{""}, "missing discharge"},
	}
	name := func(t testcase) string {
		return fmt.Sprintf("%q RPCing %s.%s(%v)", t.clientID.PublicID(), t.name, t.method, t.args)
	}

	b := createBundle(t, nil, serverID, &testServer{}) // we only create the server, a separate client will be created for each test.
	defer b.cleanup(t)
	for _, test := range tests {
		client, err := InternalNewClient(b.sm, b.ns, vc.FixedLocalID(test.clientID))
		if err != nil {
			t.Fatalf("InternalNewClient failed: %v", err)
		}
		defer client.Close()
		call, err := client.StartCall(testContext(), test.name, test.method, test.args)
		if err != nil {
			t.Errorf(`%s client.StartCall got unexpected error: "%v"`, name(test), err)
			continue
		}
		results := makeResultPtrs(test.results)
		err = call.Finish(results...)
		if !matchesErrorPattern(err, test.finishErr) {
			t.Errorf(`%s call.Finish got error: "%v", want to match: "%v"`, name(test), err, test.finishErr)
		}
	}
}

type alwaysValidCaveat struct{}

func (alwaysValidCaveat) Validate(security.Context) error { return nil }

func TestDischargePurgeFromCache(t *testing.T) {
	var (
		dischargerID = serverID.PublicID()
		c            = mkThirdPartyCaveat(dischargerID, "mountpoint/server/discharger", newCaveat(alwaysValidCaveat{}))
		clientCID    = deriveForThirdPartyCaveats(serverID, "client", c)
	)
	b := createBundle(t, clientCID, serverID, &testServer{})
	defer b.cleanup(t)

	call := func() error {
		call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "Echo", []interface{}{"batman"})
		if err != nil {
			return fmt.Errorf("client.StartCall failed: %v", err)
		}
		var got string
		if err := call.Finish(&got); err != nil {
			return fmt.Errorf("client.Finish failed: %v", err)
		}
		if want := `method:"Echo",suffix:"suffix",arg:"batman"`; got != want {
			return fmt.Errorf("Got [%v] want [%v]", got, want)
		}
		return nil
	}

	// First call should succeed
	if err := call(); err != nil {
		t.Fatal(err)
	}
	// Advance virtual clock, which will invalidate the discharge
	clock.Advance(1)
	if err := call(); !matchesErrorPattern(err, "fakeTimeCaveat expired") {
		t.Errorf("Got error [%v] wanted to match pattern 'fakeTimeCaveat expired'", err)
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
	<-call.Done()
	close(s.cancelled)
	return nil
}

// CancelStreamIgnorer doesn't read from it's input stream so all it's
// buffers fill.  The intention is to show that call.Done() is closed
// even when the stream is stalled.
func (s *cancelTestServer) CancelStreamIgnorer(call ipc.ServerCall) error {
	close(s.started)
	<-call.Done()
	close(s.cancelled)
	return nil
}

func waitForCancel(t *testing.T, ts *cancelTestServer, call ipc.Call) {
	<-ts.started
	call.Cancel()
	<-ts.cancelled
}

// TestCancel tests cancellation while the server is reading from a stream.
func TestCancel(t *testing.T) {
	ts := newCancelTestServer(t)
	b := createBundle(t, clientID, serverID, ts)
	defer b.cleanup(t)

	call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "CancelStreamReader", []interface{}{})
	if err != nil {
		t.Fatalf("Start call failed: %v", err)
	}
	waitForCancel(t, ts, call)
}

// TestCancelWithFullBuffers tests that even if the writer has filled the buffers and
// the server is not reading that the cancel message gets through.
func TestCancelWithFullBuffers(t *testing.T) {
	ts := newCancelTestServer(t)
	b := createBundle(t, clientID, serverID, ts)
	defer b.cleanup(t)

	call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "CancelStreamIgnorer", []interface{}{})
	if err != nil {
		t.Fatalf("Start call failed: %v", err)
	}
	// Fill up all the write buffers to ensure that cancelling works even when the stream
	// is blocked.
	call.Send(make([]byte, vc.MaxSharedBytes))
	call.Send(make([]byte, vc.DefaultBytesBufferedPerFlow))

	waitForCancel(t, ts, call)
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
	b := createBundle(t, clientID, serverID, s)
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
	b := createBundle(t, clientID, serverID, &testServer{})
	defer b.cleanup(t)

	// Publish some incompatible endpoints.
	publisher := publisher.New(testContext(), b.ns, publishPeriod)
	defer publisher.WaitForStop()
	defer publisher.Stop()
	publisher.AddName("incompatible")
	publisher.AddServer("/@2@tcp@localhost:10000@@1000000@2000000@@")
	publisher.AddServer("/@2@tcp@localhost:10001@@2000000@3000000@@")

	_, err := b.client.StartCall(testContext(), "incompatible/suffix", "Echo", []interface{}{"foo"})
	if !strings.Contains(err.Error(), version.NoCompatibleVersionErr.Error()) {
		t.Errorf("Expected error %v, found: %v", version.NoCompatibleVersionErr, err)
	}

	// Now add a server with a compatible endpoint and try again.
	publisher.AddServer("/" + b.ep.String())
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

// TestReconnect verifies that the client transparently re-establishes the
// connection to the server if the server dies and comes back (on the same
// endpoint).
func TestReconnect(t *testing.T) {
	b := createBundle(t, clientID, nil, nil) // We only need the client from the bundle.
	defer b.cleanup(t)
	idFile := tsecurity.SaveIdentityToFile(derive(clientID, "server"))
	server := blackbox.HelperCommand(t, "runServer", "127.0.0.1:0", idFile)
	server.Cmd.Start()
	addr, err := server.ReadLineFromChild()
	if err != nil {
		t.Fatalf("Failed to read server address from process: %v", err)
	}
	ep, err := inaming.NewEndpoint(addr)
	if err != nil {
		t.Fatalf("inaming.NewEndpoint(%q): %v", addr, err)
	}
	serverName := naming.JoinAddressName(ep.String(), "suffix")
	makeCall := func() (string, error) {
		call, err := b.client.StartCall(testContext(), serverName, "Echo", []interface{}{"bratman"})
		if err != nil {
			return "", err
		}
		var result string
		if err = call.Finish(&result); err != nil {
			return "", err
		}
		return result, nil
	}
	expected := `method:"Echo",suffix:"suffix",arg:"bratman"`
	if result, err := makeCall(); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
	}
	// Kill the server, verify client can't talk to it anymore.
	server.Cleanup()
	if _, err := makeCall(); err == nil {
		t.Fatal("Expected call to fail since server is dead")
	}
	// Resurrect the server with the same address, verify client
	// re-establishes the connection.
	server = blackbox.HelperCommand(t, "runServer", addr, idFile)
	defer server.Cleanup()
	server.Cmd.Start()
	if addr2, err := server.ReadLineFromChild(); addr2 != addr || err != nil {
		t.Fatalf("Got (%q, %v) want (%q, nil)", addr2, err, addr)
	}
	if result, err := makeCall(); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
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
	server, err := InternalNewServer(testContext(), sm, ns, vc.FixedLocalID(serverID))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	spec := *profiles.LocalListenSpec
	spec.Address = ":0"
	spec.AddressChooser = pa
	ep, err := server.ListenX(&spec)
	iep := ep.(*inaming.Endpoint)
	host, _, err := net.SplitHostPort(iep.Address)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if got, want := host, "1.1.1.1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Won't override the specified address.
	ep, err = server.ListenX(profiles.LocalListenSpec)
	iep = ep.(*inaming.Endpoint)
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
	server, err := InternalNewServer(testContext(), sm, ns, vc.FixedLocalID(serverID))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	spec := *profiles.LocalListenSpec
	spec.Address = ":0"
	spec.AddressChooser = paerr
	ep, err := server.ListenX(&spec)
	iep := ep.(*inaming.Endpoint)
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

type proxyHandle struct {
	ns      naming.Namespace
	process *blackbox.Child
	mount   string
}

func (h *proxyHandle) Start(t *testing.T) error {
	h.process = blackbox.HelperCommand(t, "runProxy")
	h.process.Cmd.Start()
	var err error
	if h.mount, err = h.process.ReadLineFromChild(); err != nil {
		return err
	}
	if err := h.ns.Mount(testContext(), "proxy", h.mount, time.Hour); err != nil {
		return err
	}
	return nil
}

func (h *proxyHandle) Stop() error {
	if h.process == nil {
		return nil
	}
	h.process.Cleanup()
	h.process = nil
	if len(h.mount) == 0 {
		return nil
	}
	return h.ns.Unmount(testContext(), "proxy", h.mount)
}

func TestProxy(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	client, err := InternalNewClient(sm, ns, vc.FixedLocalID(clientID))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := InternalNewServer(testContext(), sm, ns, vc.FixedLocalID(serverID))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	name := "mountpoint/server/suffix"
	makeCall := func() (string, error) {
		call, err := client.StartCall(testContext(), name, "Echo", []interface{}{"batman"})
		if err != nil {
			return "", err
		}
		var result string
		if err = call.Finish(&result); err != nil {
			return "", err
		}
		return result, nil
	}
	proxy := &proxyHandle{ns: ns}
	if err := proxy.Start(t); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop()
	spec := *profiles.LocalListenSpec
	spec.Proxy = "proxy"
	if _, err := server.ListenX(&spec); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve("mountpoint/server", testServerDisp{&testServer{}}); err != nil {
		t.Fatal(err)
	}
	verifyMount(t, ns, name)
	// Proxied endpoint should be published and RPC should succeed (through proxy)
	const expected = `method:"Echo",suffix:"suffix",arg:"batman"`
	if result, err := makeCall(); result != expected || err != nil {
		t.Fatalf("Got (%v, %v) want (%v, nil)", result, err, expected)
	}

	// Proxy dies, calls should fail and the name should be unmounted.
	if err := proxy.Stop(); err != nil {
		t.Fatal(err)
	}
	if result, err := makeCall(); err == nil {
		t.Fatalf(`Got (%v, %v) want ("", <non-nil>) as proxy is down`, result, err)
	}
	for {
		if _, err := ns.Resolve(testContext(), name); err != nil {
			break
		}
	}
	verifyMountMissing(t, ns, name)

	// Proxy restarts, calls should eventually start succeeding.
	if err := proxy.Start(t); err != nil {
		t.Fatal(err)
	}
	for {
		if result, err := makeCall(); err == nil {
			if result != expected {
				t.Errorf("Got (%v, %v) want (%v, nil)", result, err, expected)
			}
			break
		}
	}
}

func loadIdentityFromFile(file string) security.PrivateID {
	f, err := os.Open(file)
	if err != nil {
		vlog.Fatalf("failed to open %v: %v", file, err)
	}
	id, err := vsecurity.LoadIdentity(f)
	f.Close()
	if err != nil {
		vlog.Fatalf("Failed to load identity from %v: %v", file, err)
	}
	return id
}

func runServer(argv []string) {
	mgr := imanager.InternalNew(naming.FixedRoutingID(0x1111111))
	ns := tnaming.NewSimpleNamespace()
	id := loadIdentityFromFile(argv[1])
	isecurity.TrustIdentityProviders(id)
	server, err := InternalNewServer(testContext(), mgr, ns, vc.FixedLocalID(id))
	if err != nil {
		vlog.Fatalf("InternalNewServer failed: %v", err)
	}
	disp := testServerDisp{new(testServer)}
	if err := server.Serve("server", disp); err != nil {
		vlog.Fatalf("server.Register failed: %v", err)
	}
	spec := *profiles.LocalListenSpec
	spec.Address = argv[0]
	ep, err := server.ListenX(&spec)
	if err != nil {
		vlog.Fatalf("server.Listen failed: %v", err)
	}
	fmt.Println(ep.Addr())
	// Live forever (parent process should explicitly kill us).
	<-make(chan struct{})
}

func runProxy([]string) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		vlog.Fatal(err)
	}
	proxy, err := proxy.New(rid, nil, "tcp", "127.0.0.1:0", "")
	if err != nil {
		vlog.Fatal(err)
	}
	fmt.Println("/" + proxy.Endpoint().String())
	<-make(chan struct{})
}

// Required by blackbox framework.
func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func init() {
	isecurity.TrustIdentityProviders(clientID)
	isecurity.TrustIdentityProviders(serverID)

	blackbox.CommandTable["runServer"] = runServer
	blackbox.CommandTable["runProxy"] = runProxy

	vom.Register(fakeTimeCaveat(0))
	vom.Register(alwaysValidCaveat{})
}
