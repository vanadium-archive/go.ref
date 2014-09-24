package ipc

import (
	"reflect"
	"sort"
	"testing"

	"veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	tnaming "veyron.io/veyron/veyron/runtimes/google/testing/mocks/naming"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/services/mounttable/types"
)

func TestDebugServer(t *testing.T) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	defer sm.Shutdown()
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), sm, ns)
	if err != nil {
		t.Fatalf("InternalNewServer failed: %v", err)
	}
	defer server.Stop()
	server.Serve("", ipc.LeafDispatcher(&testObject{}, nil))
	ep, err := server.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("server.Listen failed: %v", err)
	}
	client, err := InternalNewClient(sm, ns)
	if err != nil {
		t.Fatalf("InternalNewClient failed: %v", err)
	}
	defer client.Close()
	ctx := testContext()
	// Call the Foo method on ""
	{
		addr := naming.JoinAddressName(ep.String(), "//")
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
	// Call Glob on __debug
	{
		addr := naming.JoinAddressName(ep.String(), "//__debug")
		call, err := client.StartCall(ctx, addr, "Glob", []interface{}{"*"})
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
		want := []string{
			"logs",
			"stats",
		}
		if !reflect.DeepEqual(want, results) {
			t.Errorf("unexpected results. Got %v, want %v", results, want)
		}
	}
	// Call Value on __debug/stats/testing/foo
	{
		foo := stats.NewString("testing/foo")
		foo.Set("The quick brown fox jumps over the lazy dog")
		addr := naming.JoinAddressName(ep.String(), "//__debug/stats/testing/foo")
		call, err := client.StartCall(ctx, addr, "Value", nil)
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
}

type testObject struct {
}

func (o testObject) Foo(ipc.ServerCall) string {
	return "BAR"
}
