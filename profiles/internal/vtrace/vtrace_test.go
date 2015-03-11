package vtrace_test

import (
	"bytes"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/security"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/testutil"
	tsecurity "v.io/x/ref/lib/testutil/security"
	_ "v.io/x/ref/profiles"
	iipc "v.io/x/ref/profiles/internal/ipc"
	"v.io/x/ref/profiles/internal/ipc/stream"
	"v.io/x/ref/profiles/internal/ipc/stream/manager"
	tnaming "v.io/x/ref/profiles/internal/testing/mocks/naming"
)

func TestNewFromContext(t *testing.T) {
	c0, shutdown := testutil.InitForTest()
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

func (fakeAuthorizer) Authorize(security.Call) error {
	return nil
}

type testServer struct {
	sm           stream.Manager
	ns           ns.Namespace
	name         string
	child        string
	stop         func() error
	forceCollect bool
}

func (c *testServer) Run(call ipc.ServerCall) error {
	if c.forceCollect {
		vtrace.ForceCollect(call.Context())
	}

	client, err := iipc.InternalNewClient(c.sm, c.ns)
	if err != nil {
		vlog.Error(err)
		return err
	}

	vtrace.GetSpan(call.Context()).Annotate(c.name + "-begin")

	if c.child != "" {
		var clientCall ipc.ClientCall
		if clientCall, err = client.StartCall(call.Context(), c.child, "Run", []interface{}{}); err != nil {
			vlog.Error(err)
			return err
		}
		if err := clientCall.Finish(); err != nil {
			vlog.Error(err)
			return err
		}
	}
	vtrace.GetSpan(call.Context()).Annotate(c.name + "-end")

	return nil
}

func makeTestServer(ctx *context.T, ns ns.Namespace, name, child string, forceCollect bool) (*testServer, error) {
	sm := manager.InternalNew(naming.FixedRoutingID(0x111111111))
	client, err := iipc.InternalNewClient(sm, ns)
	if err != nil {
		return nil, err
	}
	s, err := iipc.InternalNewServer(ctx, sm, ns, client, tsecurity.NewPrincipal("test"))
	if err != nil {
		return nil, err
	}

	if _, err := s.Listen(v23.GetListenSpec(ctx)); err != nil {
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
		if span.Start.IsZero() {
			t.Errorf("span missing start: %x, %s", span.Id[12:], traceString(&trace))
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
			if span.End.IsZero() {
				t.Errorf("span missing end: %x, %s", span.Id[12:], traceString(&trace))
			} else if !span.Start.Before(span.End) {
				t.Errorf("span end should be after start: %x, %s", span.Id[12:], traceString(&trace))
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
		if child.Parent != parent.Id {
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
	if err := call.Finish(); err != nil {
		t.Error(err)
	}
}

// TestCancellationPropagation tests that cancellation propogates along an
// RPC call chain without user intervention.
func TestTraceAcrossRPCs(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
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
	ctx, shutdown := testutil.InitForTest()
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
