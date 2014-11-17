package ipc

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"veyron.io/veyron/veyron/lib/testutil"
	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
)

func init() { testutil.Init() }

// newTestFlows returns the two ends of a bidirectional flow.  Each end has its
// own bookkeeping, to allow testing of method calls.
func newTestFlows() (*testFlow, *testFlow) {
	var (
		p0, p1               = tsecurity.NewPrincipal("p0"), tsecurity.NewPrincipal("p1")
		blessing0, blessing1 = p0.BlessingStore().Default(), p1.BlessingStore().Default()
		b0, b1               = new(bytes.Buffer), new(bytes.Buffer)
	)
	return &testFlow{r: b0, w: b1, p: p0, lb: blessing0, rb: blessing1}, &testFlow{r: b1, w: b0, p: p1, lb: blessing1, rb: blessing0}
}

type testFlow struct {
	r, w          *bytes.Buffer
	p             security.Principal
	lb, rb        security.Blessings
	numCloseCalls int
	errClose      error
}

func (f *testFlow) Read(b []byte) (int, error)                    { return f.r.Read(b) }
func (f *testFlow) Write(b []byte) (int, error)                   { return f.w.Write(b) }
func (*testFlow) LocalEndpoint() naming.Endpoint                  { return nil }
func (*testFlow) RemoteEndpoint() naming.Endpoint                 { return nil }
func (f *testFlow) LocalPrincipal() security.Principal            { return f.p }
func (f *testFlow) LocalBlessings() security.Blessings            { return f.lb }
func (f *testFlow) RemoteBlessings() security.Blessings           { return f.rb }
func (*testFlow) RemoteDischarges() map[string]security.Discharge { return nil }
func (*testFlow) SetDeadline(<-chan struct{})                     {}
func (*testFlow) IsClosed() bool                                  { return false }
func (*testFlow) Closed() <-chan struct{}                         { return nil }
func (*testFlow) Cancel()                                         {}

func (f *testFlow) Close() error {
	f.numCloseCalls++
	return f.errClose
}

// testDisp implements a simple test dispatcher, that uses the newInvoker
// factory function to create an underlying invoker on each Lookup.
type testDisp struct {
	newInvoker func(suffix string) ipc.Invoker
}

func (td testDisp) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	return td.newInvoker(suffix), testServerAuthorizer{}, nil
}

// closureInvoker serves a method with no user args or results:
//    func(ipc.ServerCall) error
type closureInvoker struct{ suffix string }

func newClosureInvoker(suffix string) ipc.Invoker {
	return closureInvoker{suffix}
}

func (closureInvoker) Prepare(method string, numArgs int) (argptrs, tags []interface{}, err error) {
	return nil, []interface{}{security.AdminLabel}, nil
}
func (inv closureInvoker) Invoke(method string, call ipc.ServerCall, argptrs []interface{}) (results []interface{}, err error) {
	if inv.suffix == "" {
		return nil, nil
	}
	return nil, errors.New(inv.suffix)
}
func (closureInvoker) Signature(ctx ipc.ServerContext) ([]ipc.InterfaceSig, error) {
	return nil, nil
}
func (closureInvoker) MethodSignature(ctx ipc.ServerContext, method string) (ipc.MethodSig, error) {
	return ipc.MethodSig{}, nil
}
func (closureInvoker) VGlob() *ipc.GlobState {
	return nil
}

// echoInvoker serves a method that takes a string and echoes it:
//    func(_ ServerCall, arg string) (string, error)
type echoInvoker struct{ suffix string }

func newEchoInvoker(suffix string) ipc.Invoker {
	return echoInvoker{suffix}
}

func (echoInvoker) Prepare(method string, numArgs int) (argptrs, tags []interface{}, err error) {
	var arg string
	return []interface{}{&arg}, []interface{}{security.AdminLabel}, nil
}
func (inv echoInvoker) Invoke(method string, call ipc.ServerCall, argptrs []interface{}) (results []interface{}, err error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", method, inv.suffix, *argptrs[0].(*string))
	return []interface{}{result}, nil
}
func (echoInvoker) Signature(ctx ipc.ServerContext) ([]ipc.InterfaceSig, error) {
	return nil, nil
}
func (echoInvoker) MethodSignature(ctx ipc.ServerContext, method string) (ipc.MethodSig, error) {
	return ipc.MethodSig{}, nil
}
func (echoInvoker) VGlob() *ipc.GlobState {
	return nil
}

func TestFlowClientServer(t *testing.T) {
	type v []interface{}
	type testcase struct {
		suffix string
		method string
		args   []interface{}
		expect []interface{}
		err    error
	}
	tests := []testcase{
		{"echo", "A", v{""}, v{`method:"A",suffix:"echo",arg:""`}, nil},
		{"echo", "B", v{"foo"}, v{`method:"B",suffix:"echo",arg:"foo"`}, nil},
		{"echo/abc", "C", v{""}, v{`method:"C",suffix:"echo/abc",arg:""`}, nil},
		{"echo/abc", "D", v{"foo"}, v{`method:"D",suffix:"echo/abc",arg:"foo"`}, nil},
	}
	name := func(t testcase) string {
		return fmt.Sprintf("%s.%s%v", t.suffix, t.method, t.args)
	}

	ipcServer := &server{
		ctx:   testContext(),
		disp:  testDisp{newEchoInvoker},
		stats: newIPCStats(""),
	}
	for _, test := range tests {
		clientFlow, serverFlow := newTestFlows()
		client := newFlowClient(testContext(), []string{"p0"}, clientFlow, nil)
		server := newFlowServer(serverFlow, ipcServer)
		err := client.start(test.suffix, test.method, test.args, 0, nil)
		if err != nil {
			t.Errorf("%s client.start unexpected error: %v", name(test), err)
		}
		if err := server.serve(); !verror.Equal(err, test.err) {
			t.Errorf("%s server.server returned %v want %v", name(test), err, test.err)
		}
		results := makeResultPtrs(test.expect)
		if err := client.Finish(results...); !verror.Equal(err, test.err) {
			t.Errorf(`%s client.Finish got error "%v", want "%v"`, name(test), err, test.err)
		}
		checkResultPtrs(t, name(test), results, test.expect)
		if clientFlow.numCloseCalls != 1 {
			t.Errorf("%s got %d client close calls, want 1", name(test), clientFlow.numCloseCalls)
		}
		if serverFlow.numCloseCalls != 1 {
			t.Errorf("%s got %d server close calls, want 1", name(test), serverFlow.numCloseCalls)
		}
	}
}
