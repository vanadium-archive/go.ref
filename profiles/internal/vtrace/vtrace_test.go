// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vtrace_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/uniqueid"
	"v.io/v23/vtrace"

	"v.io/x/ref/lib/flags"
	_ "v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/profiles"
	ivtrace "v.io/x/ref/profiles/internal/vtrace"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

func init() {
	test.Init()
}

// initForTest initializes the vtrace runtime and starts a mounttable.
func initForTest(t *testing.T) (*context.T, v23.Shutdown, *testutil.IDProvider) {
	idp := testutil.NewIDProvider("base")
	ctx, shutdown := test.InitForTest()
	if err := idp.Bless(v23.GetPrincipal(ctx), "alice"); err != nil {
		t.Fatalf("Could not bless initial principal %v", err)
	}

	// Start a local mounttable.
	s, err := v23.NewServer(ctx, options.ServesMountTable(true))
	if err != nil {
		t.Fatalf("Could not create mt server %v", err)
	}
	eps, err := s.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Fatalf("Could not listen for mt %v", err)
	}
	disp, err := mounttablelib.NewMountTableDispatcher("", "", "mounttable")
	if err != nil {
		t.Fatalf("Could not create mt dispatcher %v", err)
	}
	if err := s.ServeDispatcher("", disp); err != nil {
		t.Fatalf("Could not serve mt dispatcher %v", err)
	}
	v23.GetNamespace(ctx).SetRoots(eps[0].Name())
	return ctx, shutdown, idp
}

func TestNewFromContext(t *testing.T) {
	c0, shutdown, _ := initForTest(t)
	defer shutdown()
	c1, s1 := vtrace.WithNewSpan(c0, "s1")
	c2, s2 := vtrace.WithNewSpan(c1, "s2")
	c3, s3 := vtrace.WithNewSpan(c2, "s3")
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

// testServer can be easily configured to have child servers of the
// same type which it will call when it receives a call.
type testServer struct {
	name         string
	child        string
	stop         func() error
	forceCollect bool
}

func (c *testServer) Run(ctx *context.T, call rpc.ServerCall) error {
	if c.forceCollect {
		vtrace.ForceCollect(ctx)
	}
	vtrace.GetSpan(ctx).Annotate(c.name + "-begin")
	if c.child != "" {
		clientCall, err := v23.GetClient(ctx).StartCall(ctx, c.child, "Run", nil)
		if err != nil {
			return err
		}
		if err := clientCall.Finish(); err != nil {
			return err
		}
	}
	vtrace.GetSpan(ctx).Annotate(c.name + "-end")
	return nil
}

func runCallChain(t *testing.T, ctx *context.T, idp *testutil.IDProvider, force1, force2 bool) *vtrace.TraceRecord {
	ctx, span := vtrace.WithNewSpan(ctx, "")
	span.Annotate("c0-begin")
	_, stop, err := makeChainedTestServers(ctx, idp, force1, force2)
	if err != nil {
		t.Fatalf("Could not start servers %v", err)
	}
	defer stop()
	call, err := v23.GetClient(ctx).StartCall(ctx, "c1", "Run", nil)
	if err != nil {
		t.Fatal("can't call: ", err)
	}
	if err := call.Finish(); err != nil {
		t.Error(err)
	}
	span.Annotate("c0-end")
	span.Finish()

	return vtrace.GetStore(ctx).TraceRecord(span.Trace())
}

func makeChainedTestServers(ctx *context.T, idp *testutil.IDProvider, force ...bool) ([]*testServer, func(), error) {
	out := []*testServer{}
	last := len(force) - 1
	ext := "alice"
	for i, f := range force {
		name := fmt.Sprintf("c%d", i+1)
		ext += "/" + name
		principal := testutil.NewPrincipal()
		if err := idp.Bless(principal, ext); err != nil {
			return nil, nil, err
		}
		c, err := makeTestServer(ctx, principal, name)
		if err != nil {
			return nil, nil, err
		}
		if i < last {
			c.child = fmt.Sprintf("c%d", i+2)
		}
		c.forceCollect = f
		out = append(out, c)
	}
	return out, func() {
		for _, s := range out {
			s.stop()
		}
	}, nil
}

func makeTestServer(ctx *context.T, principal security.Principal, name string) (*testServer, error) {
	// Set a new vtrace store to simulate a separate process.
	ctx, err := ivtrace.Init(ctx, flags.VtraceFlags{CacheSize: 100})
	if err != nil {
		return nil, err
	}
	ctx, _ = vtrace.WithNewTrace(ctx)
	ctx, err = v23.WithPrincipal(ctx, principal)
	if err != nil {
		return nil, err
	}
	s, err := v23.NewServer(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.Listen(v23.GetListenSpec(ctx)); err != nil {
		return nil, err
	}
	c := &testServer{
		name: name,
		stop: s.Stop,
	}
	if err := s.Serve(name, c, security.AllowEveryone()); err != nil {
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

type spanSet map[uniqueid.Id]*vtrace.SpanRecord

func newSpanSet(trace vtrace.TraceRecord) spanSet {
	out := spanSet{}
	for i := range trace.Spans {
		span := &trace.Spans[i]
		out[span.Id] = span
	}
	return out
}

func (s spanSet) hasAncestor(span *vtrace.SpanRecord, ancestor *vtrace.SpanRecord) bool {
	for span = s[span.Parent]; span != nil; span = s[span.Parent] {
		if span == ancestor {
			return true
		}
	}
	return false
}

func expectSequence(t *testing.T, trace vtrace.TraceRecord, expectedSpans []string) {
	s := newSpanSet(trace)
	found := make(map[string]*vtrace.SpanRecord)
	for _, es := range expectedSpans {
		found[es] = nil
	}

	for i := range trace.Spans {
		span := &trace.Spans[i]
		smry := summary(span)
		if _, ok := found[smry]; ok {
			found[smry] = span
		}
	}

	for i, es := range expectedSpans {
		span := found[es]
		if span == nil {
			t.Errorf("expected span %s not found in\n%s", es, traceString(&trace))
			continue
		}
		// All spans should have a start.
		if span.Start.IsZero() {
			t.Errorf("span missing start: %x\n%s", span.Id[12:], traceString(&trace))
		}
		// All spans except the root should have a valid end.
		if span.Parent != trace.Id {
			if span.End.IsZero() {
				t.Errorf("span missing end: %x\n%s", span.Id[12:], traceString(&trace))
			} else if !span.Start.Before(span.End) {
				t.Errorf("span end should be after start: %x\n%s", span.Id[12:], traceString(&trace))
			}
		}
		// Spans should decend from the previous span in the list.
		if i == 0 {
			continue
		}
		if ancestor := found[expectedSpans[i-1]]; ancestor != nil && !s.hasAncestor(span, ancestor) {
			t.Errorf("span %s does not have ancestor %s", es, expectedSpans[i-1])
		}
	}
}

// TestCancellationPropagation tests that cancellation propogates along an
// RPC call chain without user intervention.
func TestTraceAcrossRPCs(t *testing.T) {
	ctx, shutdown, idp := initForTest(t)
	defer shutdown()

	vtrace.ForceCollect(ctx)
	record := runCallChain(t, ctx, idp, false, false)

	expectSequence(t, *record, []string{
		": c0-begin, c0-end",
		"<rpc.Client>\"c1\".Run",
		"\"\".Run: c1-begin, c1-end",
		"<rpc.Client>\"c2\".Run",
		"\"\".Run: c2-begin, c2-end",
	})
}

// TestCancellationPropagationLateForce tests that cancellation propogates along an
// RPC call chain when tracing is initiated by someone deep in the call chain.
func TestTraceAcrossRPCsLateForce(t *testing.T) {
	ctx, shutdown, idp := initForTest(t)
	defer shutdown()

	record := runCallChain(t, ctx, idp, false, true)

	expectSequence(t, *record, []string{
		": c0-end",
		"<rpc.Client>\"c1\".Run",
		"\"\".Run: c1-end",
		"<rpc.Client>\"c2\".Run",
		"\"\".Run: c2-begin, c2-end",
	})
}

func traceWithAuth(t *testing.T, ctx *context.T, principal security.Principal) bool {
	s, err := makeTestServer(ctx, principal, "server")
	if err != nil {
		t.Fatalf("Couldn't start server %v", err)
	}
	defer s.stop()

	ctx, span := vtrace.WithNewTrace(ctx)
	vtrace.ForceCollect(ctx)

	ctx, client, err := v23.WithNewClient(ctx)
	if err != nil {
		t.Fatalf("Couldn't create client %v", err)
	}
	call, err := client.StartCall(ctx, "server", "Run", nil)
	if err != nil {
		t.Fatalf("Couldn't make call %v", err)
	}
	if err = call.Finish(); err != nil {
		t.Fatalf("Couldn't complete call %v", err)
	}
	record := vtrace.GetStore(ctx).TraceRecord(span.Trace())
	for _, sp := range record.Spans {
		if sp.Name == `"".Run` {
			return true
		}
	}
	return false
}

type debugDispatcher string

func (permsDisp debugDispatcher) Lookup(string) (interface{}, security.Authorizer, error) {
	perms, err := access.ReadPermissions(strings.NewReader(string(permsDisp)))
	if err != nil {
		return nil, nil, err
	}
	auth, err := access.PermissionsAuthorizer(perms, access.TypicalTagType())
	if err != nil {
		return nil, nil, err
	}
	return nil, auth, nil
}

// TestPermissions tests that only permitted users are allowed to gather tracing
// information.
func TestTracePermissions(t *testing.T) {
	ctx, shutdown, idp := initForTest(t)
	defer shutdown()

	type testcase struct {
		perms string
		spans bool
	}
	cases := []testcase{
		{`{}`, false},
		{`{"Read":{"In": ["base/alice"]}, "Write":{"In": ["base/alice"]}}`, false},
		{`{"Debug":{"In": ["base/alice"]}}`, true},
	}

	// Create a different principal for the server.
	pserver := testutil.NewPrincipal()
	idp.Bless(pserver, "server")

	for _, tc := range cases {
		ctx2 := v23.WithReservedNameDispatcher(ctx, debugDispatcher(tc.perms))
		if found := traceWithAuth(t, ctx2, pserver); found != tc.spans {
			t.Errorf("got %v wanted %v for perms %s", found, tc.spans, tc.perms)
		}
	}
}
