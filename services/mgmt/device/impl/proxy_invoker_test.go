package impl

import (
	"reflect"
	"testing"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/security"
	"v.io/veyron/veyron2/services/mgmt/stats"
	"v.io/veyron/veyron2/services/security/access"
	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/lib/testutil"
)

// TODO(toddw): Add tests of Signature and MethodSignature.

func TestProxyInvoker(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %v", err)
	}
	defer runtime.Cleanup()

	// server1 is a normal server
	server1, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server1.Stop()
	localSpec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	eps1, err := server1.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := server1.Serve("", &dummy{}, nil); err != nil {
		t.Fatalf("server1.Serve: %v", err)
	}

	// server2 proxies requests to <suffix> to server1/__debug/stats/<suffix>
	server2, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server2.Stop()
	eps2, err := server2.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	disp := &proxyDispatcher{
		runtime,
		naming.JoinAddressName(eps1[0].String(), "__debug/stats"),
		stats.StatsServer(nil),
	}
	if err := server2.ServeDispatcher("", disp); err != nil {
		t.Fatalf("server2.Serve: %v", err)
	}

	// Call Value()
	name := naming.JoinAddressName(eps2[0].String(), "system/start-time-rfc1123")
	c := stats.StatsClient(name)
	if _, err := c.Value(runtime.NewContext()); err != nil {
		t.Fatalf("%q.Value() error: %v", name, err)
	}

	// Call Glob()
	results, err := testutil.GlobName(runtime.NewContext(), naming.JoinAddressName(eps2[0].String(), "system"), "start-time-*")
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	expected := []string{
		"start-time-rfc1123",
		"start-time-unix",
	}
	if !reflect.DeepEqual(results, expected) {
		t.Errorf("unexpected results. Got %q, want %q", results, expected)
	}
}

type dummy struct{}

func (*dummy) Method(_ ipc.ServerContext) error { return nil }

type proxyDispatcher struct {
	runtime veyron2.Runtime
	remote  string
	sigStub signatureStub
}

func (d *proxyDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	vlog.Infof("LOOKUP(%s): remote .... %s", suffix, d.remote)
	invoker := &proxyInvoker{
		remote:  naming.Join(d.remote, suffix),
		access:  access.Debug,
		sigStub: d.sigStub,
	}
	return invoker, nil, nil
}
