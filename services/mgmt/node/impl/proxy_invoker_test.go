package impl

import (
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/security/access"

	"veyron.io/veyron/veyron/lib/testutil"
)

// TODO(toddw): Add tests of Signature and MethodSignature.

func TestProxyInvoker(t *testing.T) {
	r := rt.R()

	// server1 is a normal server
	server1, err := r.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server1.Stop()
	localSpec := ipc.ListenSpec{Protocol: "tcp", Address: "127.0.0.1:0"}
	ep1, err := server1.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := server1.Serve("", &dummy{}, nil); err != nil {
		t.Fatalf("server1.Serve: %v", err)
	}

	// server2 proxies requests to <suffix> to server1/__debug/stats/<suffix>
	server2, err := r.NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server2.Stop()
	ep2, err := server2.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	disp := &proxyDispatcher{
		naming.JoinAddressName(ep1.String(), "__debug/stats"),
		stats.StatsServer(nil),
	}
	if err := server2.ServeDispatcher("", disp); err != nil {
		t.Fatalf("server2.Serve: %v", err)
	}

	// Call Value()
	name := naming.JoinAddressName(ep2.String(), "system/start-time-rfc1123")
	c := stats.StatsClient(name)
	if _, err := c.Value(r.NewContext()); err != nil {
		t.Errorf("%q.Value() error: %v", name, err)
	}

	// Call Glob()
	results, err := testutil.GlobName(r.NewContext(), naming.JoinAddressName(ep2.String(), "system"), "start-time-*")
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
	remote  string
	sigStub signatureStub
}

func (d *proxyDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return &proxyInvoker{naming.Join(d.remote, suffix), access.Debug, d.sigStub}, nil, nil
}
