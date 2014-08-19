package ipc

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	_ "veyron/lib/testutil"
	isecurity "veyron/runtimes/google/security"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/verror"
)

var testID = newID("test")

// newTestFlows returns the two ends of a bidirectional flow.  Each end has its
// own bookkeeping, to allow testing of method calls.
func newTestFlows() (*testFlow, *testFlow) {
	b0, b1 := new(bytes.Buffer), new(bytes.Buffer)
	return &testFlow{r: b0, w: b1}, &testFlow{r: b1, w: b0}
}

type testFlow struct {
	r, w          *bytes.Buffer
	numCloseCalls int
	errClose      error
}

func (f *testFlow) Read(b []byte) (int, error)         { return f.r.Read(b) }
func (f *testFlow) Write(b []byte) (int, error)        { return f.w.Write(b) }
func (f *testFlow) LocalAddr() net.Addr                { return nil }
func (f *testFlow) RemoteAddr() net.Addr               { return nil }
func (f *testFlow) LocalEndpoint() naming.Endpoint     { return nil }
func (f *testFlow) RemoteEndpoint() naming.Endpoint    { return nil }
func (f *testFlow) LocalID() security.PublicID         { return testID.PublicID() }
func (f *testFlow) RemoteID() security.PublicID        { return testID.PublicID() }
func (f *testFlow) SetReadDeadline(t time.Time) error  { return nil }
func (f *testFlow) SetWriteDeadline(t time.Time) error { return nil }
func (f *testFlow) SetDeadline(t time.Time) error      { return nil }
func (f *testFlow) IsClosed() bool                     { return false }
func (f *testFlow) Closed() <-chan struct{}            { return nil }
func (f *testFlow) Cancel()                            {}

func (f *testFlow) Close() error {
	f.numCloseCalls++
	return f.errClose
}

// testDisp implements a simple test dispatcher, that uses the newInvoker
// factory function to create an underlying invoker on each Lookup.
type testDisp struct {
	newInvoker func(suffix string) ipc.Invoker
}

func (td testDisp) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	return td.newInvoker(suffix), nil, nil
}

// closureInvoker serves a method with no user args or results:
//    func(ipc.ServerCall) error
type closureInvoker struct{ suffix string }

func newClosureInvoker(suffix string) ipc.Invoker {
	return closureInvoker{suffix}
}

func (closureInvoker) Prepare(method string, numArgs int) (argptrs []interface{}, label security.Label, err error) {
	return nil, security.AdminLabel, nil
}

func (inv closureInvoker) Invoke(method string, call ipc.ServerCall, argptrs []interface{}) (results []interface{}, err error) {
	if inv.suffix == "" {
		return nil, nil
	}
	return nil, errors.New(inv.suffix)
}

// echoInvoker serves a method that takes a string and echoes it:
//    func(_ ServerCall, arg string) (string, error)
type echoInvoker struct{ suffix string }

func newEchoInvoker(suffix string) ipc.Invoker {
	return echoInvoker{suffix}
}

func (echoInvoker) Prepare(method string, numArgs int) (argptrs []interface{}, label security.Label, err error) {
	var arg string
	return []interface{}{&arg}, security.AdminLabel, nil
}

func (inv echoInvoker) Invoke(method string, call ipc.ServerCall, argptrs []interface{}) (results []interface{}, err error) {
	result := fmt.Sprintf("method:%q,suffix:%q,arg:%q", method, inv.suffix, *argptrs[0].(*string))
	return []interface{}{result}, nil
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

	ipcServer := &server{disp: testDisp{newEchoInvoker}}
	for _, test := range tests {
		clientFlow, serverFlow := newTestFlows()
		client := newFlowClient(clientFlow, nil, nil)
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

func init() {
	isecurity.TrustIdentityProviders(testID)
}
