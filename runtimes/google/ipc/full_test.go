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

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	tsecurity "veyron/lib/testutil/security"
	imanager "veyron/runtimes/google/ipc/stream/manager"
	"veyron/runtimes/google/ipc/stream/proxy"
	"veyron/runtimes/google/ipc/stream/vc"
	"veyron/runtimes/google/ipc/version"
	"veyron/runtimes/google/lib/publisher"
	inaming "veyron/runtimes/google/naming"
	isecurity "veyron/runtimes/google/security"
	"veyron/security/caveat"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
)

var (
	errMethod = verror.Abortedf("server returned an error")
	clientID  security.PrivateID
	serverID  security.PrivateID
)

var errAuthorizer = errors.New("ipc: application Authorizer denied access")

type fakeContext struct{}

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

type testServerAuthorizer struct{}

func (testServerAuthorizer) Authorize(c security.Context) error {
	if c.Method() != "Unauthorized" {
		return nil
	}
	return errAuthorizer
}

type testServerDisp struct{ server interface{} }

func (t testServerDisp) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	// If suffix is "nilAuth" we use default authorization, if it is "aclAuth" we
	// use an ACL based authorizer, and otherwise we use the custom testServerAuthorizer.
	if suffix == "nilAuth" {
		return ipc.ReflectInvoker(t.server), nil, nil
	}
	if suffix == "aclAuth" {
		// Only authorize clients matching patterns "client" or "server/*".
		acl := security.ACL{
			"server/*": security.LabelSet(security.AdminLabel),
			"client":   security.LabelSet(security.AdminLabel),
		}
		return ipc.ReflectInvoker(t.server), security.NewACLAuthorizer(acl), nil
	}
	return ipc.ReflectInvoker(t.server), testServerAuthorizer{}, nil
}

// namespace is a simple partial implementation of naming.Namespace.  In
// particular, it ignores TTLs and not allow fully overlapping mount names.
type namespace struct {
	sync.Mutex
	mounts map[string][]string
}

func newNamespace() naming.Namespace {
	return &namespace{mounts: make(map[string][]string)}
}

func (ns *namespace) Mount(ctx context.T, name, server string, _ time.Duration) error {
	ns.Lock()
	defer ns.Unlock()
	for n, _ := range ns.mounts {
		if n != name && (strings.HasPrefix(name, n) || strings.HasPrefix(n, name)) {
			return fmt.Errorf("simple mount table does not allow names that are a prefix of each other")
		}
	}
	ns.mounts[name] = append(ns.mounts[name], server)
	return nil
}

func (ns *namespace) Unmount(ctx context.T, name, server string) error {
	var servers []string
	ns.Lock()
	defer ns.Unlock()
	for _, s := range ns.mounts[name] {
		// When server is "", we remove all servers under name.
		if len(server) > 0 && s != server {
			servers = append(servers, s)
		}
	}
	if len(servers) > 0 {
		ns.mounts[name] = servers
	} else {
		delete(ns.mounts, name)
	}
	return nil
}

func (ns *namespace) Resolve(ctx context.T, name string) ([]string, error) {
	if address, _ := naming.SplitAddressName(name); len(address) > 0 {
		return []string{name}, nil
	}
	ns.Lock()
	defer ns.Unlock()
	for prefix, servers := range ns.mounts {
		if strings.HasPrefix(name, prefix) {
			suffix := strings.TrimLeft(strings.TrimPrefix(name, prefix), "/")
			var ret []string
			for _, s := range servers {
				ret = append(ret, naming.Join(s, suffix))
			}
			return ret, nil
		}
	}
	return nil, verror.NotFoundf("Resolve name %q not found in %v", name, ns.mounts)
}

func (ns *namespace) ResolveToMountTable(ctx context.T, name string) ([]string, error) {
	panic("ResolveToMountTable not implemented")
	return nil, nil
}

func (ns *namespace) Unresolve(ctx context.T, name string) ([]string, error) {
	panic("Unresolve not implemented")
	return nil, nil
}

func (ns *namespace) Glob(ctx context.T, pattern string) (chan naming.MountEntry, error) {
	panic("Glob not implemented")
	return nil, nil
}

func (ns *namespace) SetRoots(...string) error {
	panic("SetRoots not implemented")
	return nil
}

func (ns *namespace) Roots() []string {
	panic("Roots not implemented")
	return nil
}

func startServer(t *testing.T, serverID security.PrivateID, sm stream.Manager, ns naming.Namespace, ts interface{}) (naming.Endpoint, ipc.Server) {
	vlog.VI(1).Info("InternalNewServer")
	server, err := InternalNewServer(InternalNewContext(), sm, ns, listenerID(serverID))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	vlog.VI(1).Info("server.Listen")
	ep, err := server.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("server.Listen failed: %v", err)
	}
	vlog.VI(1).Info("server.Serve")
	disp := testServerDisp{ts}
	if err := server.Serve("mountpoint/server", disp); err != nil {
		t.Errorf("server.Publish failed: %v", err)
	}
	return ep, server
}

func verifyMount(t *testing.T, ns naming.Namespace, name string) {
	if _, err := ns.Resolve(InternalNewContext(), name); err != nil {
		t.Errorf("%s not found in mounttable", name)
	}
}

func verifyMountMissing(t *testing.T, ns naming.Namespace, name string) {
	if servers, err := ns.Resolve(InternalNewContext(), name); err == nil {
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
	stopServer(t, b.server, b.ns)
	b.client.Close()
}

func createBundle(t *testing.T, clientID, serverID security.PrivateID, ts interface{}) (b bundle) {
	b.sm = imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	b.ns = newNamespace()
	b.ep, b.server = startServer(t, serverID, b.sm, b.ns, ts)
	var err error
	b.client, err = InternalNewClient(b.sm, b.ns, veyron2.LocalID(clientID))
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	return
}

func bless(blessor security.PrivateID, blessee security.PublicID, name string, caveats ...security.ServiceCaveat) security.PublicID {
	blessed, err := blessor.Bless(blessee, name, 24*time.Hour, caveats)
	if err != nil {
		panic(err)
	}
	return blessed
}

func derive(blessor security.PrivateID, name string, caveats ...security.ServiceCaveat) security.PrivateID {
	id, err := isecurity.NewPrivateID("irrelevant")
	if err != nil {
		panic(err)
	}
	derivedID, err := id.Derive(bless(blessor, id.PublicID(), name, caveats...))
	if err != nil {
		panic(err)
	}
	return derivedID
}

func matchesErrorPattern(err error, pattern string) bool {
	if (len(pattern) == 0) != (err == nil) {
		return false
	}
	return err == nil || strings.Index(err.Error(), pattern) >= 0
}

func TestMultipleCallsToServe(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := newNamespace()
	server, err := InternalNewServer(InternalNewContext(), sm, ns, listenerID(serverID))
	if err != nil {
		t.Errorf("InternalNewServer failed: %v", err)
	}
	_, err = server.Listen("tcp", "localhost:0")
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
	authorizeErr := "not authorized because"
	nameErr := "does not match the provided pattern"

	cavOnlyV1 := security.UniversalCaveat(caveat.PeerIdentity{"client/v1"})
	now := time.Now()
	cavExpired := security.ServiceCaveat{
		Service: security.AllPrincipals,
		Caveat:  &caveat.Expiry{IssueTime: now, ExpiryTime: now},
	}

	clientV1ID := derive(clientID, "v1")
	clientV2ID := derive(clientID, "v2")
	serverV1ID := derive(serverID, "v1", cavOnlyV1)
	serverExpiredID := derive(serverID, "expired", cavExpired)

	tests := []struct {
		clientID, serverID security.PrivateID
		pattern            security.PrincipalPattern // pattern on the server identity expected by client.
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
	ns := newNamespace()
	for _, test := range tests {
		name := fmt.Sprintf("(clientID:%q serverID:%q)", test.clientID, test.serverID)
		_, server := startServer(t, test.serverID, mgr, ns, &testServer{})
		client, err := InternalNewClient(mgr, ns, veyron2.LocalID(test.clientID))
		if err != nil {
			t.Errorf("%s: Client creation failed: %v", name, err)
			stopServer(t, server, ns)
			continue
		}
		if _, err := client.StartCall(&fakeContext{}, "mountpoint/server/suffix", "irrelevant", nil, veyron2.RemoteID(test.pattern)); !matchesErrorPattern(err, test.err) {
			t.Errorf(`%s: client.StartCall: got error "%v", want to match "%v"`, name, err, test.err)
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
		call, err := b.client.StartCall(&fakeContext{}, test.name, test.method, test.args)
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
			if err := call.CloseSend(); err != nil {
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
		call, err := b.client.StartCall(&fakeContext{}, "mountpoint/server/suffix", "EchoBlessing", []interface{}{"argument"}, test.granter)
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

func TestRPCAuthorization(t *testing.T) {
	cavOnlyEcho := security.ServiceCaveat{
		Service: security.AllPrincipals,
		Caveat:  caveat.MethodRestriction{"Echo"},
	}
	now := time.Now()
	cavExpired := security.ServiceCaveat{
		Service: security.AllPrincipals,
		Caveat:  &caveat.Expiry{IssueTime: now, ExpiryTime: now},
	}

	blessedByServerOnlyEcho := derive(serverID, "onlyEcho", cavOnlyEcho)
	blessedByServerExpired := derive(serverID, "expired", cavExpired)
	blessedByClient := derive(clientID, "blessed")

	const (
		expiredIDErr = "forbids credential from being used at this time"
		aclAuthErr   = "no matching ACL entry found"
	)
	invalidMethodErr := func(method string) string {
		return fmt.Sprintf(`caveat.MethodRestriction{"Echo"} forbids invocation of method %s`, method)
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
	}
	name := func(t testcase) string {
		return fmt.Sprintf("%q RPCing %s.%s(%v)", t.clientID.PublicID(), t.name, t.method, t.args)
	}

	b := createBundle(t, nil, serverID, &testServer{})
	defer b.cleanup(t)
	for _, test := range tests {
		client, err := InternalNewClient(b.sm, b.ns, veyron2.LocalID(test.clientID))
		if err != nil {
			t.Fatalf("InternalNewClient failed: %v", err)
		}
		defer client.Close()
		call, err := client.StartCall(&fakeContext{}, test.name, test.method, test.args)
		if err != nil {
			t.Errorf(`%s client.StartCall got unexpected error: "%v"`, name(test), err)
			continue
		}
		if err := call.CloseSend(); err != nil {
			t.Errorf(`%s call.CloseSend got unexpected error: "%v"`, name(test), err)
		}
		results := makeResultPtrs(test.results)
		err = call.Finish(results...)
		if !matchesErrorPattern(err, test.finishErr) {
			t.Errorf(`%s call.Finish got error: "%v", want to match: "%v"`, name(test), err, test.finishErr)
		}
	}
}

type cancelTestServer struct {
	started   chan struct{}
	cancelled chan struct{}
}

func newCancelTestServer() *cancelTestServer {
	return &cancelTestServer{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
}

func (s *cancelTestServer) CancelStreamReader(call ipc.ServerCall) error {
	close(s.started)
	for {
		var b []byte
		if err := call.Recv(&b); err != nil && err != io.EOF {
			return err
		}
		if call.IsClosed() {
			close(s.cancelled)
			return nil
		}
	}
}

// CancelStreamIgnorer doesn't read from it's input stream so all it's
// buffers fill.  The intention is to show that call.IsClosed is updated
// even when the stream is stalled.
func (s *cancelTestServer) CancelStreamIgnorer(call ipc.ServerCall) error {
	close(s.started)
	for {
		time.Sleep(time.Millisecond)
		if call.IsClosed() {
			close(s.cancelled)
			return nil
		}
	}
}

func waitForCancel(t *testing.T, ts *cancelTestServer, call ipc.Call) {
	<-ts.started
	call.Cancel()
	<-ts.cancelled
}

// TestCancel tests cancellation while the server is reading from a stream.
func TestCancel(t *testing.T) {
	ts := newCancelTestServer()
	b := createBundle(t, clientID, serverID, ts)
	defer b.cleanup(t)

	call, err := b.client.StartCall(&fakeContext{}, "mountpoint/server/suffix", "CancelStreamReader", []interface{}{})
	if err != nil {
		t.Fatalf("Start call failed: %v", err)
	}
	for i := 0; i <= 10; i++ {
		b := []byte{1, 2, 3}
		if err := call.Send(b); err != nil {
			t.Errorf("clientCall.Send error %q", err)
		}
	}
	waitForCancel(t, ts, call)
}

// TestCancelWithFullBuffers tests that even if the writer has filled the buffers and
// the server is not reading that the cancel message gets through.
func TestCancelWithFullBuffers(t *testing.T) {
	ts := newCancelTestServer()
	b := createBundle(t, clientID, serverID, ts)
	defer b.cleanup(t)

	call, err := b.client.StartCall(&fakeContext{}, "mountpoint/server/suffix", "CancelStreamIgnorer", []interface{}{})
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

	call, err := b.client.StartCall(&fakeContext{}, "mountpoint/server/suffix", "RecvInGoroutine", []interface{}{})
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
	publisher := publisher.New(InternalNewContext(), b.ns, publishPeriod)
	defer publisher.WaitForStop()
	defer publisher.Stop()
	publisher.AddName("incompatible")
	publisher.AddServer("/@2@tcp@localhost:10000@@1000000@2000000@@")
	publisher.AddServer("/@2@tcp@localhost:10001@@2000000@3000000@@")

	_, err := b.client.StartCall(&fakeContext{}, "incompatible/suffix", "Echo", []interface{}{"foo"})
	if !strings.Contains(err.Error(), version.NoCompatibleVersionErr.Error()) {
		t.Errorf("Expected error %v, found: %v", version.NoCompatibleVersionErr, err)
	}

	// Now add a server with a compatible endpoint and try again.
	publisher.AddServer("/" + b.ep.String())
	publisher.AddName("incompatible")

	call, err := b.client.StartCall(&fakeContext{}, "incompatible/suffix", "Echo", []interface{}{"foo"})
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

// TestPublishOptions verifies that the options that are relevant to how
// a server publishes its endpoints have the right effect.
func TestPublishOptions(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := newNamespace()
	cases := []struct {
		opts   []ipc.ServerOpt
		expect []string
	}{
		{[]ipc.ServerOpt{}, []string{"127.0.0.1", "127.0.0.1"}},
		{[]ipc.ServerOpt{veyron2.PublishAll}, []string{"127.0.0.1", "127.0.0.1"}},
		{[]ipc.ServerOpt{veyron2.PublishFirst}, []string{"127.0.0.1"}},
		{[]ipc.ServerOpt{veyron2.EndpointRewriteOpt("example1.com"), veyron2.EndpointRewriteOpt("example2.com")}, []string{"example2.com", "example2.com"}},
		{[]ipc.ServerOpt{veyron2.PublishFirst, veyron2.EndpointRewriteOpt("example.com")}, []string{"example.com"}},
	}
	for i, c := range cases {
		server, err := InternalNewServer(InternalNewContext(), sm, ns, append([]ipc.ServerOpt{listenerID(serverID)}, c.opts...)...)
		if err != nil {
			t.Errorf("InternalNewServer failed: %v", err)
			continue
		}
		if _, err := server.Listen("tcp", "localhost:0"); err != nil {
			t.Errorf("server.Listen failed: %v", err)
			server.Stop()
			continue
		}
		if _, err := server.Listen("tcp", "localhost:0"); err != nil {
			t.Errorf("server.Listen failed: %v", err)
			server.Stop()
			continue
		}
		if err := server.Serve("mountpoint", &testServerDisp{}); err != nil {
			t.Errorf("server.Publish failed: %v", err)
			server.Stop()
			continue
		}
		servers, err := ns.Resolve(InternalNewContext(), "mountpoint")
		if err != nil {
			t.Errorf("mountpoint not found in mounttable")
			server.Stop()
			continue
		}
		var got []string
		for _, s := range servers {
			address, _ := naming.SplitAddressName(s)
			ep, err := inaming.NewEndpoint(address)
			if err != nil {
				t.Errorf("case #%d: server with invalid endpoint %q: %v", i, address, err)
				continue
			}
			host, _, err := net.SplitHostPort(ep.Addr().String())
			if err != nil {
				t.Errorf("case #%d: server endpoint with invalid address %q: %v", i, ep.Addr(), err)
				continue
			}
			got = append(got, host)
		}
		if want := c.expect; !reflect.DeepEqual(want, got) {
			t.Errorf("case #%d: expected mounted servers with addresses %q, got %q instead", i, want, got)
		}
		server.Stop()
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
		call, err := b.client.StartCall(&fakeContext{}, serverName, "Echo", []interface{}{"bratman"})
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
	if err := h.ns.Mount(&fakeContext{}, "proxy", h.mount, time.Hour); err != nil {
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
	return h.ns.Unmount(&fakeContext{}, "proxy", h.mount)
}

func TestProxy(t *testing.T) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := newNamespace()
	client, err := InternalNewClient(sm, ns, veyron2.LocalID(clientID))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := InternalNewServer(InternalNewContext(), sm, ns, listenerID(serverID))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	name := "mountpoint/server/suffix"
	makeCall := func() (string, error) {
		call, err := client.StartCall(&fakeContext{}, name, "Echo", []interface{}{"batman"})
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
	if _, err := server.Listen(inaming.Network, "proxy"); err != nil {
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
		if _, err := ns.Resolve(InternalNewContext(), name); err != nil {
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
	id, err := security.LoadIdentity(f)
	f.Close()
	if err != nil {
		vlog.Fatalf("Failed to load identity from %v: %v", file, err)
	}
	return id
}

func runServer(argv []string) {
	mgr := imanager.InternalNew(naming.FixedRoutingID(0x1111111))
	ns := newNamespace()
	id := loadIdentityFromFile(argv[1])
	isecurity.TrustIdentityProviders(id)
	server, err := InternalNewServer(InternalNewContext(), mgr, ns, listenerID(id))
	if err != nil {
		vlog.Fatalf("InternalNewServer failed: %v", err)
	}
	disp := testServerDisp{new(testServer)}
	if err := server.Serve("server", disp); err != nil {
		vlog.Fatalf("server.Register failed: %v", err)
	}
	ep, err := server.Listen("tcp", argv[0])
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
	proxy, err := proxy.New(rid, nil, "tcp4", "127.0.0.1:0", "")
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
	var err error
	if clientID, err = isecurity.NewPrivateID("client"); err != nil {
		vlog.Fatalf("failed isecurity.NewPrivateID: %s", err)
	}
	if serverID, err = isecurity.NewPrivateID("server"); err != nil {
		vlog.Fatalf("failed isecurity.NewPrivateID: %s", err)
	}
	isecurity.TrustIdentityProviders(clientID)
	isecurity.TrustIdentityProviders(serverID)

	blackbox.CommandTable["runServer"] = runServer
	blackbox.CommandTable["runProxy"] = runProxy
}
