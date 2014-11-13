package impl_test

import (
	"io"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	service "veyron.io/veyron/veyron2/services/mgmt/vtrace"
	"veyron.io/veyron/veyron2/vtrace"

	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/services/mgmt/vtrace/impl"
)

func setup(t *testing.T) (string, ipc.Server, veyron2.Runtime) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not create runtime: %s", err)
	}

	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("Could not create server: %s", err)
	}
	endpoint, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen failed: %s", err)
	}
	if err := server.Serve("", impl.NewVtraceService(runtime.VtraceStore()), nil); err != nil {
		t.Fatalf("Serve failed: %s", err)
	}
	return endpoint.String(), server, runtime
}

func TestVtraceServer(t *testing.T) {
	endpoint, server, runtime := setup(t)
	defer server.Stop()

	sctx := runtime.NewContext()
	sctx, span := runtime.WithNewSpan(sctx, "The Span")
	span.Trace().ForceCollect()
	span.Finish()
	id := span.Trace().ID()

	client := service.StoreClient(naming.JoinAddressName(endpoint, ""))

	trace, err := client.Trace(runtime.NewContext(), id)
	if err != nil {
		t.Fatalf("Unexpected error getting trace: %s", err)
	}
	if len(trace.Spans) != 1 {
		t.Errorf("Returned trace should have 1 span, found %#v", trace)
	}
	if trace.Spans[0].Name != "The Span" {
		t.Errorf("Returned span has wrong name: %#v", trace)
	}

	call, err := client.AllTraces(runtime.NewContext())
	if err != nil {
		t.Fatalf("Unexpected error getting traces: %s", err)
	}
	ntraces := 0
	stream := call.RecvStream()
	var tr *vtrace.TraceRecord
	for stream.Advance() {
		trace := stream.Value()
		if trace.ID == id {
			tr = &trace
		}
		ntraces++
	}
	if err = stream.Err(); err != nil && err != io.EOF {
		t.Fatalf("Unexpected error reading trace stream: %s", err)
	}
	if ntraces < 2 {
		t.Fatalf("Expected at least 2 traces, got %#v", ntraces)
	}
	if tr == nil {
		t.Fatalf("Desired trace %x not found.", id)
	}
	if len(tr.Spans) != 1 {
		t.Errorf("Returned trace should have 1 span, found %#v", tr)
	}
	if tr.Spans[0].Name != "The Span" {
		t.Fatalf("Returned span has wrong name: %#v", tr)
	}
}
