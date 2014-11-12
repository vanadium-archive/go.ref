package vtrace_test

import (
	"strings"
	"testing"

	iipc "veyron.io/veyron/veyron/runtimes/google/ipc"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	tnaming "veyron.io/veyron/veyron/runtimes/google/testing/mocks/naming"
	truntime "veyron.io/veyron/veyron/runtimes/google/testing/mocks/runtime"
	ivtrace "veyron.io/veyron/veyron/runtimes/google/vtrace"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vtrace"
)

// We need a special way to create contexts for tests.  We
// can't create a real runtime in the runtime implementation
// so we use a fake one that panics if used.  The runtime
// implementation should not ever use the Runtime from a context.
func testContext() context.T {
	return iipc.InternalNewContext(&truntime.PanicRuntime{})
}

func TestNewFromContext(t *testing.T) {
	c0 := testContext()
	c1, s1 := ivtrace.WithNewSpan(c0, "s1")
	c2, s2 := ivtrace.WithNewSpan(c1, "s2")
	c3, s3 := ivtrace.WithNewSpan(c2, "s3")
	expected := map[context.T]vtrace.Span{
		c0: nil,
		c1: s1,
		c2: s2,
		c3: s3,
	}
	for ctx, expectedSpan := range expected {
		if s := ivtrace.FromContext(ctx); s != expectedSpan {
			t.Errorf("Wrong span for ctx %v.  Got %v, want %v", c0, s, expectedSpan)
		}
	}
}

type fakeAuthorizer int

func (fakeAuthorizer) Authorize(security.Context) error {
	return nil
}

type testServer struct {
	sm           stream.Manager
	ns           naming.Namespace
	name         string
	child        string
	stop         func() error
	forceCollect bool
}

func (c *testServer) Run(ctx ipc.ServerContext) error {
	if c.forceCollect {
		ivtrace.FromContext(ctx).Trace().ForceCollect()
	}

	client, err := iipc.InternalNewClient(c.sm, c.ns)
	if err != nil {
		vlog.Error(err)
		return err
	}

	ivtrace.FromContext(ctx).Annotate(c.name + "-begin")

	if c.child != "" {
		var call ipc.Call
		if call, err = client.StartCall(ctx, c.child, "Run", []interface{}{}); err != nil {
			vlog.Error(err)
			return err
		}
		var outerr error
		if err = call.Finish(&outerr); err != nil {
			vlog.Error(err)
			return err
		}
		if outerr != nil {
			vlog.Error(outerr)
			return outerr
		}
	}
	ivtrace.FromContext(ctx).Annotate(c.name + "-end")

	return nil
}

func makeTestServer(ns naming.Namespace, name, child string, forceCollect bool) (*testServer, error) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x111111111))
	ctx := testContext()
	s, err := iipc.InternalNewServer(ctx, sm, ns, nil)
	if err != nil {
		return nil, err
	}
	if _, err := s.Listen(ipc.ListenSpec{Protocol: "tcp", Address: "127.0.0.1:0"}); err != nil {
		return nil, err
	}

	c := &testServer{
		sm:           sm,
		ns:           ns,
		name:         name,
		child:        child,
		stop:         s.Stop,
		forceCollect: forceCollect,
	}

	if err := s.Serve(name, c, fakeAuthorizer(0)); err != nil {
		return nil, err
	}

	return c, nil
}

func summary(span *vtrace.SpanRecord) string {
	summary := span.Name
	if len(span.Annotations) > 0 {
		msgs := []string{}
		for _, annotation := range span.Annotations {
			msgs = append(msgs, annotation.Message)
		}
		summary += ": " + strings.Join(msgs, ", ")
	}
	return summary
}

func expectSequence(t *testing.T, trace vtrace.TraceRecord, expectedSpans []string) {
	if got, want := len(trace.Spans), len(expectedSpans); got != want {
		t.Errorf("Found %d spans, want %d", got, want)
	}

	spans := map[string]*vtrace.SpanRecord{}
	summaries := []string{}
	for i := range trace.Spans {
		span := &trace.Spans[i]

		// All spans should have a start.
		if span.Start == 0 {
			t.Errorf("span missing start: %#v", span)
		}
		// All spans except the root should have an end.
		if span.Name != "" && span.End == 0 {
			t.Errorf("span missing end: %#v", span)
			if span.Start >= span.End {
				t.Errorf("span end should be after start: %#v", span)
			}
		}

		summary := summary(span)
		summaries = append(summaries, summary)
		spans[summary] = span
	}

	for i := range expectedSpans {
		child, ok := spans[expectedSpans[i]]
		if !ok {
			t.Errorf("expected span %s not found in %#v", expectedSpans[i], summaries)
			continue
		}
		if i == 0 {
			continue
		}
		parent, ok := spans[expectedSpans[i-1]]
		if !ok {
			t.Errorf("expected span %s not found in %#v", expectedSpans[i-1], summaries)
			continue
		}
		if child.Parent != parent.ID {
			t.Errorf("%v should be a child of %v, but it's not.", child, parent)
		}
	}
}

func runCallChain(t *testing.T, ctx context.T, force1, force2 bool) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()

	client, err := iipc.InternalNewClient(sm, ns)
	if err != nil {
		t.Error(err)
	}

	c1, err := makeTestServer(ns, "c1", "c2", force1)
	if err != nil {
		t.Fatal("Can't start server:", err)
	}
	defer c1.stop()

	c2, err := makeTestServer(ns, "c2", "", force2)
	if err != nil {
		t.Fatal("Can't start server:", err)
	}
	defer c2.stop()

	call, err := client.StartCall(ctx, "c1", "Run", []interface{}{})
	if err != nil {
		t.Fatal("can't call: ", err)
	}
	var outerr error
	if err = call.Finish(&outerr); err != nil {
		t.Error(err)
	}
	if outerr != nil {
		t.Error(outerr)
	}
}

// TestCancellationPropagation tests that cancellation propogates along an
// RPC call chain without user intervention.
func TestTraceAcrossRPCs(t *testing.T) {
	ctx, span := ivtrace.WithNewSpan(testContext(), "")
	span.Trace().ForceCollect()
	span.Annotate("c0-begin")

	runCallChain(t, ctx, false, false)

	span.Annotate("c0-end")

	expectedSpans := []string{
		": c0-begin, c0-end",
		"<client>\"c1\".Run",
		"\"\".Run: c1-begin, c1-end",
		"<client>\"c2\".Run",
		"\"\".Run: c2-begin, c2-end",
	}
	expectSequence(t, span.Trace().Record(), expectedSpans)
}

// TestCancellationPropagationLateForce tests that cancellation propogates along an
// RPC call chain when tracing is initiated by someone deep in the call chain.
func TestTraceAcrossRPCsLateForce(t *testing.T) {
	ctx, span := ivtrace.WithNewSpan(testContext(), "")
	span.Annotate("c0-begin")

	runCallChain(t, ctx, false, true)

	span.Annotate("c0-end")

	expectedSpans := []string{
		": c0-end",
		"<client>\"c1\".Run",
		"\"\".Run: c1-end",
		"<client>\"c2\".Run",
		"\"\".Run: c2-begin, c2-end",
	}
	expectSequence(t, span.Trace().Record(), expectedSpans)
}
