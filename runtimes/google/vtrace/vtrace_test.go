package vtrace_test

import (
	"bytes"
	"strings"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"

	_ "v.io/core/veyron/profiles"
	iipc "v.io/core/veyron/runtimes/google/ipc"
	"v.io/core/veyron/runtimes/google/ipc/stream/manager"
	tnaming "v.io/core/veyron/runtimes/google/testing/mocks/naming"
)

func TestNewFromContext(t *testing.T) {
	c0, shutdown := veyron2.Init()
	defer shutdown()
	c1, s1 := vtrace.SetNewSpan(c0, "s1")
	c2, s2 := vtrace.SetNewSpan(c1, "s2")
	c3, s3 := vtrace.SetNewSpan(c2, "s3")
	expected := map[*context.T]vtrace.Span{
		c1: s1,
		c2: s2,
		c3: s3,
	}
	for ctx, expectedSpan := range expected {
		if s := vtrace.GetSpan(ctx); s != expectedSpan {
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
		vtrace.ForceCollect(ctx.Context())
	}

	client, err := iipc.InternalNewClient(c.sm, c.ns)
	if err != nil {
		vlog.Error(err)
		return err
	}

	vtrace.GetSpan(ctx.Context()).Annotate(c.name + "-begin")

	if c.child != "" {
		var call ipc.Call
		if call, err = client.StartCall(ctx.Context(), c.child, "Run", []interface{}{}); err != nil {
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
	vtrace.GetSpan(ctx.Context()).Annotate(c.name + "-end")

	return nil
}

func makeTestServer(ctx *context.T, ns naming.Namespace, name, child string, forceCollect bool) (*testServer, error) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x111111111))
	client, err := iipc.InternalNewClient(sm, ns)
	if err != nil {
		return nil, err
	}
	s, err := iipc.InternalNewServer(ctx, sm, ns, client)
	if err != nil {
		return nil, err
	}

	if _, err := s.Listen(veyron2.GetListenSpec(ctx)); err != nil {
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

func traceString(trace *vtrace.TraceRecord) string {
	var b bytes.Buffer
	vtrace.FormatTrace(&b, trace, nil)
	return b.String()
}

func expectSequence(t *testing.T, trace vtrace.TraceRecord, expectedSpans []string) {
	// It's okay to have additional spans - someone may have inserted
	// additional spans for more debugging.
	if got, want := len(trace.Spans), len(expectedSpans); got < want {
		t.Errorf("Found %d spans, want %d", got, want)
	}

	spans := map[string]*vtrace.SpanRecord{}
	summaries := []string{}
	for i := range trace.Spans {
		span := &trace.Spans[i]

		// All spans should have a start.
		if span.Start == 0 {
			t.Errorf("span missing start: %x, %s", span.ID[12:], traceString(&trace))
		}
		// All spans except the root should have a valid end.
		// TODO(mattr): For now I'm also skipping connectFlow and
		// vc.HandshakeDialedVC spans because the ws endpoints are
		// currently non-deterministic in terms of whether they fail
		// before the test ends or not.  In the future it will be
		// configurable whether we listen on ws or not and then we should
		// adjust the test to not listen and remove this check.
		if span.Name != "" &&
			span.Name != "<client>connectFlow" &&
			span.Name != "vc.HandshakeDialedVC" {
			if span.End == 0 {
				t.Errorf("span missing end: %x, %s", span.ID[12:], traceString(&trace))
			} else if span.Start >= span.End {
				t.Errorf("span end should be after start: %x, %s", span.ID[12:], traceString(&trace))
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

func runCallChain(t *testing.T, ctx *context.T, force1, force2 bool) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()

	client, err := iipc.InternalNewClient(sm, ns)
	if err != nil {
		t.Error(err)
	}
	ctx1, _ := vtrace.SetNewTrace(ctx)
	c1, err := makeTestServer(ctx1, ns, "c1", "c2", force1)
	if err != nil {
		t.Fatal("Can't start server:", err)
	}
	defer c1.stop()

	ctx2, _ := vtrace.SetNewTrace(ctx)
	c2, err := makeTestServer(ctx2, ns, "c2", "", force2)
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
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, span := vtrace.SetNewSpan(ctx, "")
	vtrace.ForceCollect(ctx)
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
	record := vtrace.GetStore(ctx).TraceRecord(span.Trace())
	expectSequence(t, *record, expectedSpans)
}

// TestCancellationPropagationLateForce tests that cancellation propogates along an
// RPC call chain when tracing is initiated by someone deep in the call chain.
func TestTraceAcrossRPCsLateForce(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()
	ctx, span := vtrace.SetNewSpan(ctx, "")
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
	record := vtrace.GetStore(ctx).TraceRecord(span.Trace())
	expectSequence(t, *record, expectedSpans)
}
