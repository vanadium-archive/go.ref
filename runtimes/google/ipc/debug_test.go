package ipc

import (
	"io"
	"reflect"
	"sort"
	"testing"

	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/x/lib/vlog"

	"v.io/core/veyron/lib/stats"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	"v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
	"v.io/core/veyron/services/mgmt/debug"
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

	debugDisp := debug.NewDispatcher(vlog.Log.LogDir, nil)

	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	ctx := testContext()
	server, err := testInternalNewServer(ctx, sm, ns, ReservedNameDispatcher{debugDisp}, vc.LocalPrincipal{pserver})
	if err != nil {
		t.Fatalf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	eps, err := server.Listen(listenSpec)
	if err != nil {
		t.Fatalf("server.Listen failed: %v", err)
	}
	if err := server.Serve("", &testObject{}, nil); err != nil {
		t.Fatalf("server.Serve failed: %v", err)
	}
	client, err := InternalNewClient(sm, ns, vc.LocalPrincipal{pclient})
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	defer client.Close()
	ep := eps[0]
	// Call the Foo method on ""
	{
		call, err := client.StartCall(ctx, ep.Name(), "Foo", nil)
		if err != nil {
			t.Fatalf("client.StartCall failed: %v", err)
		}
		var value string
		if err := call.Finish(&value); err != nil {
			t.Fatalf("call.Finish failed: %v", err)
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
		call, err := client.StartCall(ctx, addr, "Value", nil, options.NoResolve{})
		if err != nil {
			t.Fatalf("client.StartCall failed: %v", err)
		}
		var value string
		if err := call.Finish(&value); err != nil {
			t.Fatalf("call.Finish failed: %v", err)
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
		call, err := client.StartCall(ctx, addr, ipc.GlobMethod, []interface{}{tc.pattern}, options.NoResolve{})
		if err != nil {
			t.Fatalf("client.StartCall failed for %q: %v", tc.name, err)
		}
		results := []string{}
		for {
			var gr naming.VDLGlobReply
			if err := call.Recv(&gr); err != nil {
				if err != io.EOF {
					t.Fatalf("Recv failed for %q: %v. Results received thus far: %q", tc.name, err, results)
				}
				break
			}
			switch v := gr.(type) {
			case naming.VDLGlobReplyEntry:
				results = append(results, v.Value.Name)
			}
		}
		if err := call.Finish(); err != nil {
			t.Fatalf("call.Finish failed for %q: %v", tc.name, err)
		}
		sort.Strings(results)
		if !reflect.DeepEqual(tc.expected, results) {
			t.Errorf("unexpected results for %q. Got %v, want %v", tc.name, results, tc.expected)
		}
	}
}

type testObject struct {
}

func (o testObject) Foo(ipc.ServerContext) (string, error) {
	return "BAR", nil
}
