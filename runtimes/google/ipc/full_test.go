package ipc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"

	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/lib/testutil"
	imanager "veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/sectest"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	"veyron.io/veyron/veyron/runtimes/google/ipc/version"
	"veyron.io/veyron/veyron/runtimes/google/lib/publisher"
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
	tnaming "veyron.io/veyron/veyron/runtimes/google/testing/mocks/naming"
	vsecurity "veyron.io/veyron/veyron/security"
)

var (
	errMethod  = verror.Abortedf("server returned an error")
	clock      = new(fakeClock)
	listenSpec = ipc.ListenSpec{Protocol: "tcp", Address: "127.0.0.1:0"}
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

func (*testServer) EchoBlessings(call ipc.ServerCall) (server, client string) {
	return fmt.Sprintf("%v", call.LocalBlessings().ForContext(call)), fmt.Sprintf("%v", call.RemoteBlessings().ForContext(call))
}

func (*testServer) EchoGrantedBlessings(call ipc.ServerCall, arg string) (result, blessing string) {
	return arg, fmt.Sprintf("%v", call.Blessings())
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
	if err := c.Dischargeable(ctx); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", c, err)
	}
	// Add a fakeTimeCaveat to be able to control discharge expiration via 'clock'.
	return ctx.LocalPrincipal().MintDischarge(c, newCaveat(fakeTimeCaveat(clock.Now())))
}

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(c security.Context) error {
	if c.Method() != "Unauthorized" {
		return nil
	}
	return fmt.Errorf("testServerAuthorizer denied access")
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
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

func startServer(t *testing.T, principal security.Principal, sm stream.Manager, ns naming.Namespace, ts interface{}) (naming.Endpoint, ipc.Server) {
	vlog.VI(1).Info("InternalNewServer")
	server, err := InternalNewServer(testContext(), sm, ns, nil, vc.LocalPrincipal{principal})
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	vlog.VI(1).Info("server.Listen")
	ep, err := server.Listen(listenSpec)
	if err != nil {
		t.Errorf("server.Listen failed: %v", err)
	}
	vlog.VI(1).Info("server.Serve")
	disp := testServerDisp{ts}
	if err := server.ServeDispatcher("mountpoint/server", disp); err != nil {
		t.Errorf("server.Publish failed: %v", err)
	}
	if err := server.AddName("mountpoint/discharger"); err != nil {
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
	if err := server.AddName(n2); err != nil {
		t.Errorf("server.Serve failed: %v", err)
	}
	verifyMount(t, ns, n2)

	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}

	verifyMountMissing(t, ns, n1)
	verifyMountMissing(t, ns, n2)

	// Check that we can no longer serve after Stop.
	err := server.AddName("name doesn't matter")
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

func createBundle(t *testing.T, client, server security.Principal, ts interface{}) (b bundle) {
	b.sm = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	b.ns = tnaming.NewSimpleNamespace()
	if server != nil {
		b.ep, b.server = startServer(t, server, b.sm, b.ns, ts)
	}
	if client != nil {
		var err error
		if b.client, err = InternalNewClient(b.sm, b.ns, vc.LocalPrincipal{client}); err != nil {
			t.Fatalf("InternalNewClient failed: %v", err)
		}
	}
	return
}

func matchesErrorPattern(err error, pattern string) bool {
	if (len(pattern) == 0) != (err == nil) {
		return false
	}
	return err == nil || strings.Index(err.Error(), pattern) >= 0
}

func TestMultipleCallsToServeAndName(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), sm, ns, nil, vc.LocalPrincipal{sectest.NewPrincipal()})
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

	if err := server.RemoveName(n1); err != nil {
		t.Errorf("server.RemoveName failed: %v", err)
	}
	verifyMountMissing(t, ns, n1)

	if err := server.RemoveName("some randome name"); err == nil {
		t.Errorf("server.RemoveName should have failed")
	}

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

func TestRemoteIDCallOpt(t *testing.T) {
	const (
		vcErr   = "VC handshake failed"
		nameErr = "does not match the provided pattern"
	)

	var (
		pprovider, pclient, pserver = sectest.NewPrincipal("root"), sectest.NewPrincipal(), sectest.NewPrincipal()
		bserver                     = bless(pprovider, pserver, "server")
		bexpiredserver              = bless(pprovider, pserver, "server", mkCaveat(security.ExpiryCaveat(time.Now().Add(-1*time.Second))))

		mgr   = imanager.InternalNew(naming.FixedRoutingID(0x1111111))
		ns    = tnaming.NewSimpleNamespace()
		tests = []struct {
			server  security.Blessings       // blessings presented by the server to the client.
			pattern security.BlessingPattern // pattern on the server identity expected by the client.
			err     string
		}{
			// Client accepts talking to the server only if the server's identity matches the provided pattern
			{bserver, security.AllPrincipals, ""},
			{bserver, "root/server", ""},
			{bserver, "root/otherserver", nameErr},
			{bserver, "otherroot/server", nameErr},

			// Client does not talk to a server that presents an expired identity.
			{bexpiredserver, security.AllPrincipals, vcErr},
		}
		_, server = startServer(t, pserver, mgr, ns, &testServer{})
	)
	defer stopServer(t, server, ns)
	// Make the client and server principals trust root certificates from pprovider
	pclient.AddToRoots(pprovider.BlessingStore().Default())
	pserver.AddToRoots(pprovider.BlessingStore().Default())
	// Create a blessing that the client is willing to share with server.
	pclient.BlessingStore().Set(bless(pprovider, pclient, "client"), "root/server")
	for _, test := range tests {
		name := fmt.Sprintf("(%q@%q)", test.pattern, test.server)
		pserver.BlessingStore().SetDefault(test.server)
		// Recreate client in each test (so as to not re-use VCs to the server).
		client, err := InternalNewClient(mgr, ns, vc.LocalPrincipal{pclient})
		if err != nil {
			t.Errorf("%s: failed to create client: %v", name, err)
			continue
		}
		if call, err := client.StartCall(testContext(), fmt.Sprintf("[%s]mountpoint/server/suffix", test.pattern), "Method", nil); !matchesErrorPattern(err, test.err) {
			t.Errorf(`%s: client.StartCall: got error "%v", want to match "%v"`, name, err, test.err)
		} else if call != nil {
			blessings, proof := call.RemoteBlessings()
			if proof == nil {
				t.Errorf("%s: Returned nil for remote blessings", name)
			}
			if !test.pattern.MatchedBy(blessings...) {
				t.Errorf("%s: %q.MatchedBy(%v) failed", name, test.pattern, blessings)
			}
		}
		client.Close()
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

		pserver = sectest.NewPrincipal("server")
		pclient = sectest.NewPrincipal("client")

		b = createBundle(t, pclient, pserver, &testServer{})
	)
	defer b.cleanup(t)
	// The server needs to recognize the client's root certificate.
	pserver.AddToRoots(pclient.BlessingStore().Default())
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
	b := createBundle(t, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"), &testServer{})
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

// granter implements ipc.Granter, returning a fixed (security.Blessings, error) pair.
type granter struct {
	ipc.CallOpt
	b   security.Blessings
	err error
}

func (g granter) Grant(id security.Blessings) (security.Blessings, error) { return g.b, g.err }

func TestGranter(t *testing.T) {
	var (
		pclient = sectest.NewPrincipal("client")
		pserver = sectest.NewPrincipal("server")
		b       = createBundle(t, pclient, pserver, &testServer{})
	)
	defer b.cleanup(t)

	tests := []struct {
		granter                       ipc.Granter
		blessing, starterr, finisherr string
	}{
		{blessing: "<nil>"},
		{granter: granter{b: bless(pclient, pserver, "blessed")}, blessing: "client/blessed"},
		{granter: granter{err: errors.New("hell no")}, starterr: "hell no"},
		{granter: granter{b: pclient.BlessingStore().Default()}, finisherr: "blessing granted not bound to this server"},
	}
	for _, test := range tests {
		call, err := b.client.StartCall(testContext(), "mountpoint/server/suffix", "EchoGrantedBlessings", []interface{}{"argument"}, test.granter)
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

func mkThirdPartyCaveat(discharger security.PublicKey, location string, c security.Caveat) security.Caveat {
	tpc, err := security.NewPublicKeyCaveat(discharger, location, security.ThirdPartyRequirements{}, c)
	if err != nil {
		panic(err)
	}
	return newCaveat(tpc)
}

type dischargeImpetusTester struct {
	LastDischargeImpetus security.DischargeImpetus
}

// Implements ipc.Dispatcher
func (s *dischargeImpetusTester) Lookup(_, _ string) (interface{}, security.Authorizer, error) {
	return ipc.ReflectInvoker(s), testServerAuthorizer{}, nil
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
		pserver     = sectest.NewPrincipal("server")
		pdischarger = pserver // In general, the discharger can be a separate principal. In this test, it happens to be the server.
		sm          = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
		ns          = tnaming.NewSimpleNamespace()

		mkClient = func(req security.ThirdPartyRequirements) vc.LocalPrincipal {
			pclient := sectest.NewPrincipal()
			tpc, err := security.NewPublicKeyCaveat(pdischarger.PublicKey(), "mountpoint/discharger", req, security.UnconstrainedUse())
			if err != nil {
				t.Fatalf("Failed to create ThirdPartyCaveat(%+v): %v", req, err)
			}
			cav, err := security.NewCaveat(tpc)
			if err != nil {
				t.Fatal(err)
			}
			b, err := pclient.BlessSelf("client", cav)
			if err != nil {
				t.Fatalf("BlessSelf failed: %v", err)
			}
			pclient.AddToRoots(pserver.BlessingStore().Default()) // make the client recognize the server.
			pclient.BlessingStore().Set(b, "server")
			return vc.LocalPrincipal{pclient}
		}
	)
	server, err := InternalNewServer(testContext(), sm, ns, nil, vc.LocalPrincipal{pserver})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	if _, err := server.Listen(listenSpec); err != nil {
		t.Fatal(err)
	}

	var tester dischargeImpetusTester
	if err := server.ServeDispatcher("mountpoint", &tester); err != nil {
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
			Impetus:      security.DischargeImpetus{Server: []security.BlessingPattern{"server"}, Method: "Method", Arguments: []vdlutil.Any{vdlutil.Any("argument")}},
		},
		{ // Require only the method name
			Requirements: security.ThirdPartyRequirements{ReportMethod: true},
			Impetus:      security.DischargeImpetus{Method: "Method"},
		},
	}

	for _, test := range tests {
		client, err := InternalNewClient(sm, ns, mkClient(test.Requirements))
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
		// Principals
		pclient     = sectest.NewPrincipal("client")
		pserver     = sectest.NewPrincipal("server")
		pdischarger = pserver

		now = time.Now()

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
	)
	// The server should recognize the client principal as an authority on "client/..." and "random/..." blessings.
	pserver.AddToRoots(bClient)
	pserver.AddToRoots(bRandom)
	// And the client needs to recognize the server's blessing to decide which of its own blessings to share.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	// sectest.NewPrincipal sets up a principal that shares blessings with all servers, undo that.
	pclient.BlessingStore().Set(nil, security.AllPrincipals)

	type v []interface{}
	tests := []struct {
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
		// - aclAuth suffix: the ACL only allows "server/..." or "client"
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
	b := createBundle(t, nil, pserver, &testServer{}) // we only create the server, a separate client will be created for each test.
	defer b.cleanup(t)
	for _, test := range tests {
		name := fmt.Sprintf("%q.%s(%v) by %v", test.name, test.method, test.args, test.blessings)
		client, err := InternalNewClient(b.sm, b.ns, vc.LocalPrincipal{pclient})
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
		} else if !test.authorized && !verror.Is(err, verror.NoAccess) {
			t.Errorf("%s. call.Finish returned error %v(%v), wanted %v", name, verror.Convert(err).ErrorID(), err, verror.NoAccess)
		}
	}
}

func TestDischargePurgeFromCache(t *testing.T) {
	var (
		pserver     = sectest.NewPrincipal("server")
		pdischarger = pserver // In general, the discharger can be a separate principal. In this test, it happens to be the server.
		pclient     = sectest.NewPrincipal("client")
		// Client is blessed with a third-party caveat. The discharger service issues discharges with a fakeTimeCaveat.
		// This blessing is presented to "server".
		bclient = bless(pserver, pclient, "client", mkThirdPartyCaveat(pdischarger.PublicKey(), "mountpoint/server/discharger", security.UnconstrainedUse()))
	)
	// Setup the client to recognize the server's blessing and present bclient to it.
	pclient.AddToRoots(pserver.BlessingStore().Default())
	pclient.BlessingStore().Set(bclient, "server")

	b := createBundle(t, pclient, pserver, &testServer{})
	defer b.cleanup(t)

	call := func() error {
		call, err := b.client.StartCall(testContext(), "mountpoint/server/aclAuth", "Echo", []interface{}{"batman"})
		if err != nil {
			return fmt.Errorf("client.StartCall failed: %v", err)
		}
		var got string
		if err := call.Finish(&got); err != nil {
			return fmt.Errorf("client.Finish failed: %v", err)
		}
		if want := `method:"Echo",suffix:"aclAuth",arg:"batman"`; got != want {
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
	if err, want := call(), "not authorized"; !matchesErrorPattern(err, want) {
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
	b := createBundle(t, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"), ts)
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
	b := createBundle(t, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"), ts)
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
	b := createBundle(t, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"), s)
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
	b := createBundle(t, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"), &testServer{})
	defer b.cleanup(t)

	// Publish some incompatible endpoints.
	publisher := publisher.New(testContext(), b.ns, publishPeriod)
	defer publisher.WaitForStop()
	defer publisher.Stop()
	publisher.AddName("incompatible")
	publisher.AddServer("/@2@tcp@localhost:10000@@1000000@2000000@@", false)
	publisher.AddServer("/@2@tcp@localhost:10001@@2000000@3000000@@", false)

	_, err := b.client.StartCall(testContext(), "incompatible/suffix", "Echo", []interface{}{"foo"})
	if !strings.Contains(err.Error(), version.NoCompatibleVersionErr.Error()) {
		t.Errorf("Expected error %v, found: %v", version.NoCompatibleVersionErr, err)
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
	server, err := InternalNewServer(testContext(), sm, ns, nil, vc.LocalPrincipal{sectest.NewPrincipal("server")})
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	spec := listenSpec
	spec.Address = ":0"
	spec.AddressChooser = pa
	ep, err := server.Listen(spec)
	iep := ep.(*inaming.Endpoint)
	host, _, err := net.SplitHostPort(iep.Address)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if got, want := host, "1.1.1.1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Won't override the specified address.
	ep, err = server.Listen(listenSpec)
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
	server, err := InternalNewServer(testContext(), sm, ns, nil, vc.LocalPrincipal{sectest.NewPrincipal("server")})
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	spec := listenSpec
	spec.Address = ":0"
	spec.AddressChooser = paerr
	ep, err := server.Listen(spec)
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

func TestSecurityNone(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x66666666))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), sm, ns, nil, options.VCSecurityNone)
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
	client, err := InternalNewClient(sm, ns, options.VCSecurityNone)
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	// When using VCSecurityNone, all authorization checks should be skipped, so
	// unauthorized methods shoudl be callable.
	call, err := client.StartCall(testContext(), "mp/server", "Unauthorized", nil)
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
	client, err := InternalNewClient(sm, ns, options.VCSecurityNone)
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	call, err := client.StartCall(nil, "foo", "bar", []interface{}{})
	if call != nil {
		t.Errorf("Expected nil interface got: %#v", call)
	}
	if !verror.Is(err, verror.BadArg) {
		t.Errorf("Expected a BadArg error, got: %s", err.Error())
	}
}

func TestSplitObjectName(t *testing.T) {
	cases := []struct {
		input, mt, server, name string
	}{
		{"[foo/bar]", "", "foo/bar", ""},
		{"[x/y/...]/", "x/y/...", "", "/"},
		{"[foo]a", "", "foo", "a"},
		{"[foo]/a", "foo", "", "/a"},
		{"[foo]/a/[bar]", "foo", "bar", "/a"},
		{"a/b", "", "", "a/b"},
		{"[foo]a/b", "", "foo", "a/b"},
		{"/a/b", "", "", "/a/b"},
		{"[foo]/a/b", "foo", "", "/a/b"},
		{"/a/[bar]b", "", "bar", "/a/b"},
		{"[foo]/a/[bar]b", "foo", "bar", "/a/b"},
		{"/a/b[foo]", "", "", "/a/b[foo]"},
		{"/a/b/[foo]c", "", "", "/a/b/[foo]c"},
		{"/[01:02::]:444", "", "", "/[01:02::]:444"},
		{"[foo]/[01:02::]:444", "foo", "", "/[01:02::]:444"},
		{"/[01:02::]:444/foo", "", "", "/[01:02::]:444/foo"},
		{"[a]/[01:02::]:444/foo", "a", "", "/[01:02::]:444/foo"},
		{"/[01:02::]:444/[b]foo", "", "b", "/[01:02::]:444/foo"},
		{"[c]/[01:02::]:444/[d]foo", "c", "d", "/[01:02::]:444/foo"},
	}
	for _, c := range cases {
		mt, server, name := splitObjectName(c.input)
		if string(mt) != c.mt {
			t.Errorf("%q: unexpected mt pattern: %q not %q", c.input, mt, c.mt)
		}
		if string(server) != c.server {
			t.Errorf("%q: unexpected server pattern: %q not %q", c.input, server, c.server)
		}
		if name != c.name {
			t.Errorf("%q: unexpected name: %q not %q", c.input, name, c.name)
		}
	}
}

func TestServerBlessingsOpt(t *testing.T) {
	var (
		pserver   = sectest.NewPrincipal("server")
		pclient   = sectest.NewPrincipal("client")
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
	runServer := func(name string, opts ...ipc.ServerOpt) stream.Manager {
		opts = append(opts, vc.LocalPrincipal{pserver})
		rid, err := naming.NewRoutingID()
		if err != nil {
			t.Fatal(err)
		}
		sm := imanager.InternalNew(rid)
		server, err := InternalNewServer(
			testContext(),
			sm,
			ns,
			nil,
			opts...)
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

func init() {
	testutil.Init()
	vom.Register(fakeTimeCaveat(0))
}
