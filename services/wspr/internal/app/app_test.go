// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	vdltime "v.io/v23/vdlroot/time"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/services/wspr/internal/lib"
	"v.io/x/ref/services/wspr/internal/lib/testwriter"
	"v.io/x/ref/services/wspr/internal/rpc/server"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

//go:generate v23 test generate

var testPrincipal = testutil.NewPrincipal("test")

// newBlessedPrincipal returns a new principal that has a blessing from the
// provided runtime's principal which is set on its BlessingStore such
// that it is revealed to all clients and servers.
func newBlessedPrincipal(ctx *context.T) security.Principal {
	principal := v23.GetPrincipal(ctx)
	p := testutil.NewPrincipal()
	b, err := principal.Bless(p.PublicKey(), principal.BlessingStore().Default(), "delegate", security.UnconstrainedUse())
	if err != nil {
		panic(err)
	}
	if err := vsecurity.SetDefaultBlessings(p, b); err != nil {
		panic(err)
	}
	return p
}

type simpleAdder struct{}

func (s simpleAdder) Add(_ *context.T, _ rpc.ServerCall, a, b int32) (int32, error) {
	return a + b, nil
}

func (s simpleAdder) Divide(_ *context.T, _ rpc.ServerCall, a, b int32) (int32, error) {
	if b == 0 {
		return 0, verror.New(verror.ErrBadArg, nil, "div 0")
	}
	return a / b, nil
}

func (s simpleAdder) StreamingAdd(_ *context.T, call rpc.StreamServerCall) (int32, error) {
	total := int32(0)
	var value int32
	for err := call.Recv(&value); err == nil; err = call.Recv(&value) {
		total += value
		call.Send(total)
	}
	return total, nil
}

var simpleAddrSig = signature.Interface{
	Doc: "The empty interface contains methods not attached to any interface.",
	Methods: []signature.Method{
		{
			Name:    "Add",
			InArgs:  []signature.Arg{{Type: vdl.Int32Type}, {Type: vdl.Int32Type}},
			OutArgs: []signature.Arg{{Type: vdl.Int32Type}},
		},
		{
			Name:    "Divide",
			InArgs:  []signature.Arg{{Type: vdl.Int32Type}, {Type: vdl.Int32Type}},
			OutArgs: []signature.Arg{{Type: vdl.Int32Type}},
		},
		{
			Name:      "StreamingAdd",
			OutArgs:   []signature.Arg{{Type: vdl.Int32Type}},
			InStream:  &signature.Arg{Type: vdl.AnyType},
			OutStream: &signature.Arg{Type: vdl.AnyType},
		},
	},
}

func startAnyServer(ctx *context.T, servesMT bool, dispatcher rpc.Dispatcher) (rpc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := v23.NewServer(ctx, options.ServesMountTable(servesMT))
	if err != nil {
		return nil, nil, err
	}

	endpoints, err := s.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		return nil, nil, err
	}

	if err := s.ServeDispatcher("", dispatcher); err != nil {
		return nil, nil, err
	}
	return s, endpoints[0], nil
}

func startAdderServer(ctx *context.T) (rpc.Server, naming.Endpoint, error) {
	return startAnyServer(ctx, false, testutil.LeafDispatcher(simpleAdder{}, nil))
}

func startMountTableServer(ctx *context.T) (rpc.Server, naming.Endpoint, error) {
	mt, err := mounttablelib.NewMountTableDispatcher("", "", "mounttable")
	if err != nil {
		return nil, nil, err
	}
	return startAnyServer(ctx, true, mt)
}

func TestGetGoServerSignature(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	s, endpoint, err := startAdderServer(ctx)
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()

	spec := v23.GetListenSpec(ctx)
	spec.Proxy = "mockVeyronProxyEP"
	controller, err := NewController(ctx, nil, &spec, nil, newBlessedPrincipal(ctx))

	if err != nil {
		t.Fatalf("Failed to create controller: %v", err)
	}
	sig, err := controller.getSignature(ctx, "/"+endpoint.String())
	if err != nil {
		t.Fatalf("Failed to get signature: %v", err)
	}
	if got, want := len(sig), 2; got != want {
		t.Fatalf("got signature %#v len %d, want %d", sig, got, want)
	}
	if got, want := sig[0], simpleAddrSig; !reflect.DeepEqual(got, want) {
		t.Errorf("got sig[0] %#v, want: %#v", got, want)
	}
	if got, want := sig[1].Name, "__Reserved"; got != want {
		t.Errorf("got sig[1].Name %#v, want: %#v", got, want)
	}
}

type goServerTestCase struct {
	method          string
	inArgs          []interface{}
	numOutArgs      int32
	streamingInputs []interface{}
	expectedStream  []lib.Response
	expectedError   error
}

func runGoServerTestCase(t *testing.T, testCase goServerTestCase) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	s, endpoint, err := startAdderServer(ctx)
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()

	spec := v23.GetListenSpec(ctx)
	spec.Proxy = "mockVeyronProxyEP"
	controller, err := NewController(ctx, nil, &spec, nil, newBlessedPrincipal(ctx))

	if err != nil {
		t.Errorf("unable to create controller: %v", err)
		t.Fail()
		return
	}
	writer := testwriter.Writer{}
	var stream *outstandingStream
	if len(testCase.streamingInputs) > 0 {
		stream = newStream(nil)
		controller.outstandingRequests[0] = &outstandingRequest{
			stream: stream,
		}
		go func() {
			for _, value := range testCase.streamingInputs {
				controller.SendOnStream(0, lib.VomEncodeOrDie(value), &writer)
			}
			controller.CloseStream(0)
		}()
	}

	request := RpcRequest{
		Name:        "/" + endpoint.String(),
		Method:      testCase.method,
		NumInArgs:   int32(len(testCase.inArgs)),
		NumOutArgs:  testCase.numOutArgs,
		IsStreaming: stream != nil,
	}
	controller.sendVeyronRequest(ctx, 0, &request, testCase.inArgs, &writer, stream, vtrace.GetSpan(ctx))

	if err := testwriter.CheckResponses(&writer, testCase.expectedStream, testCase.expectedError); err != nil {
		t.Error(err)
	}
}

func makeRPCResponse(outArgs ...*vdl.Value) string {
	return lib.VomEncodeOrDie(RpcResponse{
		OutArgs:       outArgs,
		TraceResponse: vtrace.Response{},
	})
}

func TestCallingGoServer(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:     "Add",
		inArgs:     []interface{}{2, 3},
		numOutArgs: 1,
		expectedStream: []lib.Response{
			lib.Response{
				Message: makeRPCResponse(vdl.Int32Value(5)),
				Type:    lib.ResponseFinal,
			},
		},
	})
}

func TestCallingGoServerWithError(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:        "Divide",
		inArgs:        []interface{}{1, 0},
		numOutArgs:    1,
		expectedError: verror.New(verror.ErrBadArg, nil, "div 0"),
	})
}

func TestCallingGoWithStreaming(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:          "StreamingAdd",
		streamingInputs: []interface{}{1, 2, 3, 4},
		numOutArgs:      1,
		expectedStream: []lib.Response{
			lib.Response{
				Message: lib.VomEncodeOrDie(int32(1)),
				Type:    lib.ResponseStream,
			},
			lib.Response{
				Message: lib.VomEncodeOrDie(int32(3)),
				Type:    lib.ResponseStream,
			},
			lib.Response{
				Message: lib.VomEncodeOrDie(int32(6)),
				Type:    lib.ResponseStream,
			},
			lib.Response{
				Message: lib.VomEncodeOrDie(int32(10)),
				Type:    lib.ResponseStream,
			},
			lib.Response{
				Message: nil,
				Type:    lib.ResponseStreamClose,
			},
			lib.Response{
				Message: makeRPCResponse(vdl.Int32Value(10)),
				Type:    lib.ResponseFinal,
			},
		},
	})
}

type runningTest struct {
	controller       *Controller
	writer           *testwriter.Writer
	mounttableServer rpc.Server
	proxyShutdown    func()
}

func makeRequest(rpc RpcRequest, args ...interface{}) (string, error) {
	var buf bytes.Buffer
	encoder := vom.NewEncoder(&buf)
	if err := encoder.Encode(rpc); err != nil {
		return "", err
	}
	for _, arg := range args {
		if err := encoder.Encode(arg); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(buf.Bytes()), nil
}

func serveServer(ctx *context.T, writer lib.ClientWriter, setController func(*Controller)) (*runningTest, error) {
	mounttableServer, endpoint, err := startMountTableServer(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to start mounttable: %v", err)
	}
	proxySpec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	proxyShutdown, proxyEndpoint, err := profiles.NewProxy(ctx, proxySpec, security.AllowEveryone())
	if err != nil {
		return nil, fmt.Errorf("unable to start proxy: %v", err)
	}

	writerCreator := func(int32) lib.ClientWriter {
		return writer
	}
	spec := v23.GetListenSpec(ctx)
	spec.Proxy = proxyEndpoint.Name()
	controller, err := NewController(ctx, writerCreator, &spec, nil, testPrincipal)
	if err != nil {
		return nil, err
	}

	if setController != nil {
		setController(controller)
	}

	v23.GetNamespace(controller.Context()).SetRoots("/" + endpoint.String())

	req, err := makeRequest(RpcRequest{
		Name:       "__controller",
		Method:     "Serve",
		NumInArgs:  3,
		NumOutArgs: 1,
		Deadline:   vdltime.Deadline{},
	}, "adder", 0, []RpcServerOption{})
	controller.HandleVeyronRequest(ctx, 0, req, writer)

	testWriter, _ := writer.(*testwriter.Writer)
	return &runningTest{
		controller, testWriter, mounttableServer, proxyShutdown,
	}, nil
}

// A test case to simulate a Javascript server talking to the App.  All the
// responses from Javascript are mocked and sent back through the method calls.
// All messages from the client are sent using a go client.
type jsServerTestCase struct {
	method string
	inArgs []interface{}
	// The set of streaming inputs from the client to the server.
	// This is passed to the client, which then passes it to the app.
	clientStream []interface{}
	// The set of JSON streaming messages sent from Javascript to the
	// app.
	serverStream []interface{}
	// The final response sent by the Javascript server to the
	// app.
	finalResponse *vdl.Value
	// The final error sent by the Javascript server to the app.
	err error

	// Whether or not the Javascript server has an authorizer or not.
	// If it does have an authorizer, then err is sent back from the server
	// to the app.
	hasAuthorizer bool
}

func runJsServerTestCase(t *testing.T, testCase jsServerTestCase) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	vomClientStream := []string{}
	for _, m := range testCase.clientStream {
		vomClientStream = append(vomClientStream, lib.VomEncodeOrDie(m))
	}
	mock := &mockJSServer{
		t:                    t,
		method:               testCase.method,
		serviceSignature:     []signature.Interface{simpleAddrSig},
		expectedClientStream: vomClientStream,
		serverStream:         testCase.serverStream,
		hasAuthorizer:        testCase.hasAuthorizer,
		inArgs:               testCase.inArgs,
		finalResponse:        testCase.finalResponse,
		finalError:           testCase.err,
		controllerReady:      sync.RWMutex{},
	}
	rt, err := serveServer(ctx, mock, func(controller *Controller) {
		mock.controller = controller
	})
	defer rt.mounttableServer.Stop()
	defer rt.proxyShutdown()
	defer rt.controller.Cleanup()

	if err != nil {
		t.Fatalf("could not serve server %v", err)
	}

	// Get the client that is relevant to the controller so it talks
	// to the right mounttable.
	client := v23.GetClient(rt.controller.Context())
	// And have the client recognize the server, otherwise it won't
	// authorize calls to it.
	v23.GetPrincipal(rt.controller.Context()).AddToRoots(v23.GetPrincipal(ctx).BlessingStore().Default())

	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	call, err := client.StartCall(rt.controller.Context(), "adder/adder", testCase.method, testCase.inArgs)
	if err != nil {
		t.Fatalf("failed to start call: %v", err)
	}

	for _, msg := range testCase.clientStream {
		if err := call.Send(msg); err != nil {
			t.Errorf("unexpected error while sending %v: %v", msg, err)
		}
	}
	if err := call.CloseSend(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	expectedStream := testCase.serverStream
	for {
		var data interface{}
		if err := call.Recv(&data); err != nil {
			break
		}
		if len(expectedStream) == 0 {
			t.Errorf("unexpected stream value: %v", data)
			continue
		}
		if !reflect.DeepEqual(data, expectedStream[0]) {
			t.Errorf("unexpected stream value: got %v, expected %v", data, expectedStream[0])
		}
		expectedStream = expectedStream[1:]
	}

	var result *vdl.Value
	err = call.Finish(&result)

	if verror.ErrorID(err) != verror.ErrorID(testCase.err) {
		t.Errorf("unexpected err: got %#v, expected %#v", err, testCase.err)
	}

	if err != nil {
		return
	}

	if got, want := result, testCase.finalResponse; !vdl.EqualValue(got, want) {
		t.Errorf("unexected final response: got %v, want %v", got, want)
	}

	// ensure there is only one server and then stop the server
	if len(rt.controller.servers) != 1 {
		t.Errorf("expected only one server but got: %d", len(rt.controller.servers))
		return
	}
	for serverId := range rt.controller.servers {
		rt.controller.Stop(nil, nil, serverId)
	}

	// ensure there is no more servers now
	if len(rt.controller.servers) != 0 {
		t.Errorf("expected no server after stopping the only one but got: %d", len(rt.controller.servers))
		return
	}
}

func TestSimpleJSServer(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{int32(1), int32(2)},
		finalResponse: vdl.Int32Value(3),
	})
}

func TestJSServerWithAuthorizer(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{int32(1), int32(2)},
		finalResponse: vdl.Int32Value(3),
		hasAuthorizer: true,
	})
}

func TestJSServerWithError(t *testing.T) {
	err := verror.New(verror.ErrInternal, nil)
	runJsServerTestCase(t, jsServerTestCase{
		method: "Divide",
		inArgs: []interface{}{int32(1), int32(0)},
		err:    err,
	})
}

func TestJSServerWithAuthorizerAndAuthError(t *testing.T) {
	err := verror.New(verror.ErrNoAccess, nil)
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{int32(1), int32(2)},
		hasAuthorizer: true,
		finalResponse: vdl.Int32Value(3),
		err:           err,
	})
}
func TestJSServerWihStreamingInputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "StreamingAdd",
		clientStream:  []interface{}{int32(3), int32(4)},
		finalResponse: vdl.Int32Value(10),
	})
}

func TestJSServerWihStreamingOutputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "StreamingAdd",
		serverStream:  []interface{}{int32(3), int32(4)},
		finalResponse: vdl.Int32Value(10),
	})
}

func TestJSServerWihStreamingInputsAndOutputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "StreamingAdd",
		clientStream:  []interface{}{int32(1), int32(2)},
		serverStream:  []interface{}{int32(3), int32(4)},
		finalResponse: vdl.Int32Value(10),
	})
}

func TestJSServerWithWrongNumberOfArgs(t *testing.T) {
	err := verror.New(server.ErrWrongNumberOfArgs, nil, "Add", 3, 2)
	runJsServerTestCase(t, jsServerTestCase{
		method: "Add",
		inArgs: []interface{}{int32(1), int32(2), int32(3)},
		err:    err,
	})
}

func TestJSServerWithMethodNotFound(t *testing.T) {
	methodName := "UnknownMethod"
	err := verror.New(server.ErrMethodNotFoundInSignature, nil, methodName)
	runJsServerTestCase(t, jsServerTestCase{
		method: methodName,
		inArgs: []interface{}{int32(1), int32(2)},
		err:    err,
	})
}
