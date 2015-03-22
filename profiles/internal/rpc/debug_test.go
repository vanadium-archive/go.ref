package rpc

import (
	"io"
	"reflect"
	"sort"
	"testing"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/stats"
	"v.io/x/ref/profiles/internal/rpc/stream/manager"
	tnaming "v.io/x/ref/profiles/internal/testing/mocks/naming"
	"v.io/x/ref/services/mgmt/debug"
	"v.io/x/ref/test/testutil"
)

func TestDebugServer(t *testing.T) {
	ctx, shutdown := initForTest()
	defer shutdown()
	// Setup the client and server principals, with the client willing to share its
	// blessing with the server.
	var (
		pclient = testutil.NewPrincipal("client")
		pserver = testutil.NewPrincipal("server")
		bclient = bless(pserver, pclient, "client") // server/client blessing.
	)
	pclient.AddToRoots(bclient)                    // Client recognizes "server" as a root of blessings.
	pclient.BlessingStore().Set(bclient, "server") // Client presents bclient to server

	debugDisp := debug.NewDispatcher(vlog.Log.LogDir, nil)

	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := testInternalNewServer(ctx, sm, ns, pserver, ReservedNameDispatcher{debugDisp})
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
	ctx, _ = v23.SetPrincipal(ctx, pclient)
	client, err := InternalNewClient(sm, ns)
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
		call, err := client.StartCall(ctx, addr, rpc.GlobMethod, []interface{}{tc.pattern}, options.NoResolve{})
		if err != nil {
			t.Fatalf("client.StartCall failed for %q: %v", tc.name, err)
		}
		results := []string{}
		for {
			var gr naming.GlobReply
			if err := call.Recv(&gr); err != nil {
				if err != io.EOF {
					t.Fatalf("Recv failed for %q: %v. Results received thus far: %q", tc.name, err, results)
				}
				break
			}
			switch v := gr.(type) {
			case naming.GlobReplyEntry:
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

func (o testObject) Foo(rpc.ServerCall) (string, error) {
	return "BAR", nil
}
