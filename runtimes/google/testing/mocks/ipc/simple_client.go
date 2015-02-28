package ipc

import (
	"errors"
	"fmt"
	"sync"

	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
)

type ClientWithTimesCalled interface {
	ipc.Client
	TimesCalled(method string) int
}

// NewSimpleClient creates a new mocked ipc client where the given map of method name
// to outputs is used for evaluating the method calls.
// It also adds some testing features such as counters for number of times a method is called
func NewSimpleClient(methodsResults map[string][]interface{}) ClientWithTimesCalled {
	return &simpleMockClient{
		results:     methodsResults,
		timesCalled: make(map[string]int),
	}
}

// simpleMockClient implements ipc.Client
type simpleMockClient struct {
	// Protects timesCalled
	sync.Mutex

	// results is a map of method names to results
	results map[string][]interface{}
	// timesCalled is a counter for number of times StartCall is called on a specific method name
	timesCalled map[string]int
}

// TimesCalled returns number of times the given method has been called.
func (c *simpleMockClient) TimesCalled(method string) int {
	return c.timesCalled[method]
}

// StartCall Implements ipc.Client
func (c *simpleMockClient) StartCall(ctx *context.T, name, method string, args []interface{}, opts ...ipc.CallOpt) (ipc.ClientCall, error) {
	defer vlog.LogCall()()
	results, ok := c.results[method]
	if !ok {
		return nil, errors.New(fmt.Sprintf("method %s not found", method))
	}

	// Copy the results so that they can be modified without effecting the original.
	// This must be done via vom encode and decode rather than a direct deep copy because (among other reasons)
	// reflect-based deep copy on vdl.Type objects will fail because of their private fields. This is not a problem with vom
	// as it manually creates the type objects. It is also more realistic to use the same mechanism as the ultimate calls.
	vomBytes, err := vom.Encode(results)
	if err != nil {
		panic(fmt.Sprintf("Error copying value with vom (failed on encode): %v", err))
	}
	var copiedResults []interface{}
	if err := vom.Decode(vomBytes, &copiedResults); err != nil {
		panic(fmt.Sprintf("Error copying value with vom (failed on decode): %v", err))
	}

	clientCall := mockCall{
		results: copiedResults,
	}

	c.Lock()
	c.timesCalled[method]++
	c.Unlock()

	return &clientCall, nil
}

// Close implements ipc.Client
func (*simpleMockClient) Close() {
	defer vlog.LogCall()()
}

// mockCall implements ipc.ClientCall
type mockCall struct {
	mockStream
	results []interface{}
}

// Cancel implements ipc.ClientCall
func (*mockCall) Cancel() {
	defer vlog.LogCall()()
}

// CloseSend implements ipc.ClientCall
func (*mockCall) CloseSend() error {
	defer vlog.LogCall()()
	return nil
}

// Finish implements ipc.ClientCall
func (mc *mockCall) Finish(resultptrs ...interface{}) error {
	defer vlog.LogCall()()
	if got, want := len(resultptrs), len(mc.results); got != want {
		return errors.New(fmt.Sprintf("wrong number of output results; expected resultptrs of size %d but got %d", want, got))
	}
	for ax, res := range resultptrs {
		if mc.results[ax] != nil {
			if err := vdl.Convert(res, mc.results[ax]); err != nil {
				panic(fmt.Sprintf("Error converting out argument %#v: %v", mc.results[ax], err))
			}
		}
	}
	return nil
}

// RemoteBlessings implements ipc.ClientCall
func (*mockCall) RemoteBlessings() ([]string, security.Blessings) {
	return []string{}, security.Blessings{}
}

//mockStream implements ipc.Stream
type mockStream struct{}

//Send implements ipc.Stream
func (*mockStream) Send(interface{}) error {
	defer vlog.LogCall()()
	return nil
}

//Recv implements ipc.Stream
func (*mockStream) Recv(interface{}) error {
	defer vlog.LogCall()()
	return nil
}
