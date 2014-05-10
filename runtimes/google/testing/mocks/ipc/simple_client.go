package ipc

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"veyron2/ipc"
)

// NewSimpleClient creates a new mocked ipc client where the given map of method name
// to outputs is used for evaluating the method calls.
// It also adds some testing features such as counters for number of times a method is called
func NewSimpleClient(methodsResults map[string][]interface{}) *SimpleMockClient {
	return &SimpleMockClient{
		results:     methodsResults,
		timesCalled: make(map[string]int),
	}
}

// SimpleMockClient implements ipc.Client
type SimpleMockClient struct {
	// Protects timesCalled
	sync.Mutex

	// results is a map of method names to results
	results map[string][]interface{}
	// timesCalled is a counter for number of times StartCall is called on a specific method name
	timesCalled map[string]int
}

// TimesCalled returns number of times the given method has been called.
func (c *SimpleMockClient) TimesCalled(method string) int {
	return c.timesCalled[method]
}

// IPCBindOpt Implements ipc.Client
func (c *SimpleMockClient) IPCBindOpt() {}

// StartCall Implements ipc.Client
func (c *SimpleMockClient) StartCall(name, method string, args []interface{}, opts ...ipc.ClientCallOpt) (ipc.ClientCall, error) {
	results, ok := c.results[method]
	if !ok {
		return nil, errors.New(fmt.Sprintf("method %s not found", method))
	}

	clientCall := mockCall{
		results: results,
	}

	c.Lock()
	c.timesCalled[method]++
	c.Unlock()

	return &clientCall, nil
}

// Close Implements ipc.Client
func (*SimpleMockClient) Close() {
}

// mockCall implements ipc.ClientCall
type mockCall struct {
	mockStream
	results []interface{}
}

// Cancel implements ipc.ClientCall
func (*mockCall) Cancel() {
}

// CloseSend implements ipc.ClientCall
func (*mockCall) CloseSend() error {
	return nil
}

// Finish implements ipc.ClientCall
func (mc *mockCall) Finish(resultptrs ...interface{}) error {
	if got, want := len(resultptrs), len(mc.results); got != want {
		return errors.New(fmt.Sprintf("wrong number of output results; expected resultptrs of size %d but got %d", want, got))
	}
	for ax, res := range resultptrs {
		if mc.results[ax] != nil {
			reflect.ValueOf(res).Elem().Set(reflect.ValueOf(mc.results[ax]))
		}
	}

	return nil
}

//mockStream implements ipc.Stream
type mockStream struct{}

//Send implements ipc.Stream
func (*mockStream) Send(interface{}) error {
	return nil
}

//Recv implements ipc.Stream
func (*mockStream) Recv(interface{}) error {
	return nil
}
