package ipc

import (
	"io"
	"reflect"
	"sort"
	"testing"

	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/options"
	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/lib/stats"
	tsecurity "v.io/veyron/veyron/lib/testutil/security"
	"v.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"v.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	tnaming "v.io/veyron/veyron/runtimes/google/testing/mocks/naming"
	"v.io/veyron/veyron/runtimes/google/vtrace"
	"v.io/veyron/veyron/services/mgmt/debug"
)

func TestDebugServer(t *testing.T) {
	// Setup the client and server principals, with the client willing to share its
	// blessing with the server.
	var (
		pclient = tsecurity.NewPrincipal("client")
		pserver = tsecurity.NewPrincipal("server")
		bclient = bless(pserver, pclient, "client") // server/client blessing.
	)
	pclient.AddToRoots(bclient)                    // Client recognizes "server" as a root of blessings.
	pclient.BlessingStore().Set(bclient, "server") // Client presents bclient to server

	store := vtrace.NewStore(10)
	debugDisp := debug.NewDispatcher(vlog.Log.LogDir(), nil, store)

	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), sm, ns, nil, options.ReservedNameDispatcher{debugDisp}, vc.LocalPrincipal{pserver})
	if err != nil {
		t.Fatalf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	server.Serve("", &testObject{}, nil)
	eps, err := server.Listen(listenSpec)
	if err != nil {
		t.Fatalf("server.Listen failed: %v", err)
	}
	client, err := InternalNewClient(sm, ns, vc.LocalPrincipal{pclient})
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	defer client.Close()
	ctx := testContext()
	ep := eps[0]
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
		{"", "__*/*", []string{"__debug/logs", "__debug/pprof", "__debug/stats", "__debug/vtrace"}},
		{"__debug", "*", []string{"logs", "pprof", "stats", "vtrace"}},
	}
	for _, tc := range testcases {
		addr := naming.JoinAddressName(ep.String(), tc.name)
		call, err := client.StartCall(ctx, addr, ipc.GlobMethod, []interface{}{tc.pattern}, options.NoResolve(true))
		if err != nil {
			t.Fatalf("client.StartCall failed for %q: %v", tc.name, err)
		}
		results := []string{}
		for {
			var me naming.VDLMountEntry
			if err := call.Recv(&me); err != nil {
				if err != io.EOF {
					t.Fatalf("Recv failed for %q: %v. Results received thus far: %q", tc.name, err, results)
				}
				break
			}
			results = append(results, me.Name)
		}
		if ferr := call.Finish(&err); ferr != nil {
			t.Fatalf("call.Finish failed for %q: %v", tc.name, ferr)
		}
		sort.Strings(results)
		if !reflect.DeepEqual(tc.expected, results) {
			t.Errorf("unexpected results for %q. Got %v, want %v", tc.name, results, tc.expected)
		}
	}
}

type testObject struct {
}

func (o testObject) Foo(ipc.ServerContext) string {
	return "BAR"
}
