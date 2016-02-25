// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"errors"
	"fmt"
	"sync"

	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vom"
)

type clientWithTimesCalled interface {
	rpc.Client
	TimesCalled(method string) int
}

// NewSimpleClient creates a new mocked rpc client where the given map of method name
// to outputs is used for evaluating the method calls.
// It also adds some testing features such as counters for number of times a method is called
func newSimpleClient(methodsResults map[string][]interface{}) clientWithTimesCalled {
	return &simpleMockClient{
		results:     methodsResults,
		timesCalled: make(map[string]int),
	}
}

// simpleMockClient implements rpc.Client
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

// StartCall Implements rpc.Client
func (c *simpleMockClient) StartCall(ctx *context.T, name, method string, args []interface{}, opts ...rpc.CallOpt) (rpc.ClientCall, error) {
	results, ok := c.results[method]
	if !ok {
		return nil, fmt.Errorf("method %s not found", method)
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

func (c *simpleMockClient) Call(ctx *context.T, name, method string, inArgs, outArgs []interface{}, callOpts ...rpc.CallOpt) error {
	call, err := c.StartCall(ctx, name, method, inArgs, callOpts...)
	if err != nil {
		return err
	}
	return call.Finish(outArgs...)
}

// Implement rpc.Client.
func (*simpleMockClient) PinConnection(*context.T, string, ...rpc.CallOpt) (flow.PinnedConn, error) {
	return nil, nil
}
func (*simpleMockClient) Close()                  {}
func (*simpleMockClient) Closed() <-chan struct{} { return nil }

// mockCall implements rpc.ClientCall
type mockCall struct {
	mockStream
	results []interface{}
}

func (*mockCall) Cancel()                 {}
func (*mockCall) CloseSend() error        { return nil }
func (*mockCall) Security() security.Call { return nil }
func (mc *mockCall) Finish(resultptrs ...interface{}) error {
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
func (*mockCall) RemoteBlessings() ([]string, security.Blessings) {
	return []string{}, security.Blessings{}
}

//mockStream implements rpc.Stream
type mockStream struct{}

//Send implements rpc.Stream
func (*mockStream) Send(interface{}) error {
	return nil
}

//Recv implements rpc.Stream
func (*mockStream) Recv(interface{}) error {
	return nil
}
