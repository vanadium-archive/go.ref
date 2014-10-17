package ipc

import (
	"reflect"
	"sort"
	"testing"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/services/mounttable/types"

	"veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/sectest"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	tnaming "veyron.io/veyron/veyron/runtimes/google/testing/mocks/naming"
)

func TestDebugServer(t *testing.T) {
	// Setup the client and server principals, with the client willing to share its
	// blessing with the server.
	var (
		pclient = sectest.NewPrincipal("client")
		pserver = sectest.NewPrincipal("server")
		bclient = bless(pserver, pclient, "client") // server/client blessing.
	)
	pclient.AddToRoots(bclient)                    // Client recognizes "server" as a root of blessings.
	pclient.BlessingStore().Set(bclient, "server") // Client presents bclient to server

	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), sm, ns, vc.LocalPrincipal{pserver})
	if err != nil {
		t.Fatalf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	server.Serve("", ipc.LeafDispatcher(&testObject{}, nil))
	ep, err := server.ListenX(&listenSpec)
	if err != nil {
		t.Fatalf("server.Listen failed: %v", err)
	}
	client, err := InternalNewClient(sm, ns, vc.LocalPrincipal{pclient})
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	defer client.Close()
	ctx := testContext()
	// Call the Foo method on ""
	{
		addr := naming.JoinAddressName(ep.String(), "")
		call, err := client.StartCall(ctx, addr, "Foo", nil)
		if err != nil {
			t.Fatalf("client.StartCall failed: %v", err)
		}
		var value string
		if ferr := call.Finish(&value); ferr != nil {
			t.Fatalf("call.Finish failed: %v", ferr)
		}
		if want := "BAR"; value != want {
			t.Errorf("unexpected value: Got %v, want %v", value, want)
		}
	}
	// Call Value on __debug/stats/testing/foo
	{
		foo := stats.NewString("testing/foo")
		foo.Set("The quick brown fox jumps over the lazy dog")
		addr := naming.JoinAddressName(ep.String(), "__debug/stats/testing/foo")
		call, err := client.StartCall(ctx, addr, "Value", nil, options.NoResolve(true))
		if err != nil {
			t.Fatalf("client.StartCall failed: %v", err)
		}
		var value string
		if ferr := call.Finish(&value, &err); ferr != nil {
			t.Fatalf("call.Finish failed: %v", ferr)
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := foo.Value(); value != want {
			t.Errorf("unexpected result: Got %v, want %v", value, want)
		}
	}

	// Call Glob
	testcases := []struct {
		name, pattern string
		expected      []string
	}{
		{"", "*", []string{}},
		{"", "__*", []string{"__debug"}},
		{"", "__*/*", []string{"__debug/logs", "__debug/pprof", "__debug/stats"}},
		{"__debug", "*", []string{"logs", "pprof", "stats"}},
	}
	for _, tc := range testcases {
		addr := naming.JoinAddressName(ep.String(), "//"+tc.name)
		call, err := client.StartCall(ctx, addr, "Glob", []interface{}{tc.pattern})
		if err != nil {
			t.Fatalf("client.StartCall failed: %v", err)
		}
		results := []string{}
		for {
			var me types.MountEntry
			if err := call.Recv(&me); err != nil {
				break
			}
			results = append(results, me.Name)
		}
		if ferr := call.Finish(&err); ferr != nil {
			t.Fatalf("call.Finish failed: %v", ferr)
		}
		sort.Strings(results)
		if !reflect.DeepEqual(tc.expected, results) {
			t.Errorf("unexpected results. Got %v, want %v", results, tc.expected)
		}
	}
}

type testObject struct {
}

func (o testObject) Foo(ipc.ServerCall) string {
	return "BAR"
}
