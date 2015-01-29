package impl

import (
	"reflect"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/stats"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/testutil"
)

// TODO(toddw): Add tests of Signature and MethodSignature.

func TestProxyInvoker(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	veyron2.GetNamespace(ctx).CacheCtl(naming.DisableCache(true))

	// server1 is a normal server
	server1, err := veyron2.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	localSpec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	eps1, err := server1.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := server1.Serve("", &dummy{}, nil); err != nil {
		t.Fatalf("server1.Serve: %v", err)
	}

	// server2 proxies requests to <suffix> to server1/__debug/stats/<suffix>
	server2, err := veyron2.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server2.Stop()
	eps2, err := server2.Listen(localSpec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	disp := &proxyDispatcher{
		naming.JoinAddressName(eps1[0].String(), "__debug/stats"),
		stats.StatsServer(nil).Describe__(),
	}
	if err := server2.ServeDispatcher("", disp); err != nil {
		t.Fatalf("server2.Serve: %v", err)
	}

	// Call Value()
	name := naming.JoinAddressName(eps2[0].String(), "system/start-time-rfc1123")
	c := stats.StatsClient(name)
	if _, err := c.Value(ctx); err != nil {
		t.Fatalf("%q.Value() error: %v", name, err)
	}

	// Call Glob()
	results, err := testutil.GlobName(ctx, naming.JoinAddressName(eps2[0].String(), "system"), "start-time-*")
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
	remote string
	desc   []ipc.InterfaceDesc
}

func (d *proxyDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	vlog.Infof("LOOKUP(%s): remote .... %s", suffix, d.remote)
	return newProxyInvoker(naming.Join(d.remote, suffix), access.Debug, d.desc), nil, nil
}
