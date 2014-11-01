package impl

import (
	"reflect"
	"sort"
	"testing"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/mounttable"
)

func TestProxyInvoker(t *testing.T) {
	r := rt.R()

	// server1 is a normal server with a nil dispatcher.
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
	if err := server1.Serve("", nil); err != nil {
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
		security.Label(security.AllLabels),
		&stats.ServerStubStats{},
	}
	if err := server2.Serve("", disp); err != nil {
		t.Fatalf("server2.Serve: %v", err)
	}

	// Call Value()
	name := naming.JoinAddressName(ep2.String(), "system/start-time-rfc1123")
	c, err := stats.BindStats(name)
	if err != nil {
		t.Fatalf("BindStats error: %v", err)
	}
	if _, err := c.Value(r.NewContext()); err != nil {
		t.Errorf("%q.Value() error: %v", name, err)
	}

	// Call Glob()
	results := doGlob(t, naming.JoinAddressName(ep2.String(), "system"), "start-time-*")
	expected := []string{
		"start-time-rfc1123",
		"start-time-unix",
	}
	if !reflect.DeepEqual(results, expected) {
		t.Errorf("unexpected results. Got %q, want %q", results, expected)
	}
}

func doGlob(t *testing.T, name, pattern string) []string {
	c, err := mounttable.BindGlobbable(name)
	if err != nil {
		t.Fatalf("BindGlobbable failed: %v", err)
	}
	stream, err := c.Glob(rt.R().NewContext(), pattern)
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	results := []string{}
	iterator := stream.RecvStream()
	for iterator.Advance() {
		results = append(results, iterator.Value().Name)
	}
	if err := iterator.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", err)
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Finish failed: %v", err)
	}
	sort.Strings(results)
	return results
}

type proxyDispatcher struct {
	remote  string
	label   security.Label
	sigStub signatureStub
}

func (d *proxyDispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	return &proxyInvoker{naming.Join(d.remote, suffix), d.label, d.sigStub}, nil, nil
}
