package app

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vdl/vdlroot/src/signature"
	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/vom"
	"v.io/core/veyron2/vtrace"
	"v.io/wspr/veyron/services/wsprd/ipc/server"
	"v.io/wspr/veyron/services/wsprd/lib"
	"v.io/wspr/veyron/services/wsprd/lib/testwriter"

	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron/runtimes/google/ipc/stream/proxy"
	vsecurity "v.io/core/veyron/security"
	mounttable "v.io/core/veyron/services/mounttable/lib"
)

var (
	testPrincipalBlessing = "test"
	testPrincipal         = newPrincipal(testPrincipalBlessing)
)

// newBlessedPrincipal returns a new principal that has a blessing from the
// provided runtime's principal which is set on its BlessingStore such
// that it is revealed to all clients and servers.
func newBlessedPrincipal(ctx *context.T) security.Principal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}

	principal := veyron2.GetPrincipal(ctx)
	b, err := principal.Bless(p.PublicKey(), principal.BlessingStore().Default(), "delegate", security.UnconstrainedUse())
	if err != nil {
		panic(err)
	}
	tsecurity.SetDefaultBlessings(p, b)
	return p
}

// newPrincipal returns a new principal that has a self-blessing with
// the provided extension 'selfBlessing' which is set on its BlessingStore
// such that it is revealed to all clients and servers.
func newPrincipal(selfBlessing string) security.Principal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	b, err := p.BlessSelf(selfBlessing)
	if err != nil {
		panic(err)
	}
	tsecurity.SetDefaultBlessings(p, b)
	return p
}

type simpleAdder struct{}

func (s simpleAdder) Add(_ ipc.ServerContext, a, b int32) (int32, error) {
	return a + b, nil
}

func (s simpleAdder) Divide(_ ipc.ServerContext, a, b int32) (int32, error) {
	if b == 0 {
		return 0, verror.New(verror.ErrBadArg, nil, "div 0")
	}
	return a / b, nil
}

func (s simpleAdder) StreamingAdd(call ipc.ServerCall) (int32, error) {
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

func startAnyServer(ctx *context.T, servesMT bool, dispatcher ipc.Dispatcher) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := veyron2.NewServer(ctx, options.ServesMountTable(servesMT))
	if err != nil {
		return nil, nil, err
	}

	endpoints, err := s.Listen(veyron2.GetListenSpec(ctx))
	if err != nil {
		return nil, nil, err
	}

	if err := s.ServeDispatcher("", dispatcher); err != nil {
		return nil, nil, err
	}
	return s, endpoints[0], nil
}

func startAdderServer(ctx *context.T) (ipc.Server, naming.Endpoint, error) {
	return startAnyServer(ctx, false, testutil.LeafDispatcher(simpleAdder{}, nil))
}

func startProxy(ctx *context.T) (*proxy.Proxy, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	return proxy.New(rid, nil, "tcp", "127.0.0.1:0", "")
}

func startMountTableServer(ctx *context.T) (ipc.Server, naming.Endpoint, error) {
	mt, err := mounttable.NewMountTableDispatcher("")
	if err != nil {
		return nil, nil, err
	}
	return startAnyServer(ctx, true, mt)
}

func TestGetGoServerSignature(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	s, endpoint, err := startAdderServer(ctx)
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()

	spec := veyron2.GetListenSpec(ctx)
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

func runGoServerTestCase(t *testing.T, test goServerTestCase) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	s, endpoint, err := startAdderServer(ctx)
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()

	spec := veyron2.GetListenSpec(ctx)
	spec.Proxy = "mockVeyronProxyEP"
	controller, err := NewController(ctx, nil, &spec, nil, newBlessedPrincipal(ctx))

	if err != nil {
		t.Errorf("unable to create controller: %v", err)
		t.Fail()
		return
	}
	writer := testwriter.Writer{}
	var stream *outstandingStream
	if len(test.streamingInputs) > 0 {
		stream = newStream()
		controller.outstandingRequests[0] = &outstandingRequest{
			stream: stream,
		}
		go func() {
			for _, value := range test.streamingInputs {
				controller.SendOnStream(0, lib.VomEncodeOrDie(value), &writer)
			}
			controller.CloseStream(0)
		}()
	}

	request := VeyronRPCRequest{
		Name:        "/" + endpoint.String(),
		Method:      test.method,
		NumInArgs:   int32(len(test.inArgs)),
		NumOutArgs:  test.numOutArgs,
		IsStreaming: stream != nil,
	}
	controller.sendVeyronRequest(ctx, 0, &request, test.inArgs, &writer, stream, vtrace.GetSpan(ctx))

	if err := testwriter.CheckResponses(&writer, test.expectedStream, test.expectedError); err != nil {
		t.Error(err)
	}
}

func makeRPCResponse(outArgs ...vdl.AnyRep) string {
	return lib.VomEncodeOrDie(VeyronRPCResponse{
		OutArgs: outArgs,
		TraceResponse: vtrace.Response{
			Flags: vtrace.CollectInMemory,
		},
	})
}

func TestCallingGoServer(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:     "Add",
		inArgs:     []interface{}{2, 3},
		numOutArgs: 1,
		expectedStream: []lib.Response{
			lib.Response{
				Message: makeRPCResponse(int32(5)),
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
				Message: makeRPCResponse(int32(10)),
				Type:    lib.ResponseFinal,
			},
		},
	})
}

type runningTest struct {
	controller       *Controller
	writer           *testwriter.Writer
	mounttableServer ipc.Server
	proxyServer      *proxy.Proxy
}

func makeRequest(rpc VeyronRPCRequest, args ...interface{}) (string, error) {
	var buf bytes.Buffer
	encoder, err := vom.NewBinaryEncoder(&buf)
	if err != nil {
		return "", err
	}
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

func serveServer(ctx *context.T) (*runningTest, error) {
	mounttableServer, endpoint, err := startMountTableServer(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to start mounttable: %v", err)
	}

	proxyServer, err := startProxy(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to start proxy: %v", err)
	}

	proxyEndpoint := proxyServer.Endpoint().String()

	writer := testwriter.Writer{}

	writerCreator := func(int32) lib.ClientWriter {
		return &writer
	}
	spec := veyron2.GetListenSpec(ctx)
	spec.Proxy = "/" + proxyEndpoint
	controller, err := NewController(ctx, writerCreator, &spec, nil, testPrincipal)
	if err != nil {
		return nil, err
	}

	veyron2.GetNamespace(controller.Context()).SetRoots("/" + endpoint.String())

	req, err := makeRequest(VeyronRPCRequest{
		Name:       "controller",
		Method:     "Serve",
		NumInArgs:  2,
		NumOutArgs: 1,
		Timeout:    20000000000,
	}, "adder", 0)
	controller.HandleVeyronRequest(ctx, 0, req, &writer)

	return &runningTest{
		controller, &writer, mounttableServer, proxyServer,
	}, nil
}

func TestJavascriptServeServer(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	rt, err := serveServer(ctx)
	defer rt.mounttableServer.Stop()
	defer rt.proxyServer.Shutdown()
	defer rt.controller.Cleanup()
	if err != nil {
		t.Fatalf("could not serve server %v", err)
	}

	if len(rt.writer.Stream) != 1 {
		t.Errorf("expected only one response, got %d", len(rt.writer.Stream))
		return
	}

	resp := rt.writer.Stream[0]

	if resp.Type != lib.ResponseFinal {
		t.Errorf("unknown stream message Got: %v, expected: serve response", resp)
		return
	}
}

func TestJavascriptStopServer(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	rt, err := serveServer(ctx)
	defer rt.mounttableServer.Stop()
	defer rt.proxyServer.Shutdown()
	defer rt.controller.Cleanup()

	if err != nil {
		t.Errorf("could not serve server %v", err)
		return
	}

	// ensure there is only one server and then stop the server
	if len(rt.controller.servers) != 1 {
		t.Errorf("expected only one server but got: %d", len(rt.controller.servers))
		return
	}
	for serverId := range rt.controller.servers {
		rt.controller.removeServer(serverId)
	}

	// ensure there is no more servers now
	if len(rt.controller.servers) != 0 {
		t.Errorf("expected no server after stopping the only one but got: %d", len(rt.controller.servers))
		return
	}

	return
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
	finalResponse interface{}
	// The final error sent by the Javascript server to the app.
	err error

	// Whether or not the Javascript server has an authorizer or not.
	// If it does have an authorizer, then authError is sent back from the server
	// to the app.
	hasAuthorizer bool
	authError     error
}

func runJsServerTestCase(t *testing.T, test jsServerTestCase) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	rt, err := serveServer(ctx)
	defer rt.mounttableServer.Stop()
	defer rt.proxyServer.Shutdown()
	defer rt.controller.Cleanup()

	if err != nil {
		t.Errorf("could not serve server %v", err)
	}

	if len(rt.writer.Stream) != 1 {
		t.Errorf("expected only on response, got %d", len(rt.writer.Stream))
		return
	}

	resp := rt.writer.Stream[0]

	if resp.Type != lib.ResponseFinal {
		t.Errorf("unknown stream message Got: %v, expected: serve response", resp)
		return
	}

	rt.writer.Stream = nil

	vomClientStream := []string{}
	for _, m := range test.clientStream {
		vomClientStream = append(vomClientStream, lib.VomEncodeOrDie(m))
	}
	mock := &mockJSServer{
		controller:           rt.controller,
		t:                    t,
		method:               test.method,
		serviceSignature:     []signature.Interface{simpleAddrSig},
		expectedClientStream: vomClientStream,
		serverStream:         test.serverStream,
		hasAuthorizer:        test.hasAuthorizer,
		authError:            test.authError,
		inArgs:               test.inArgs,
		finalResponse:        test.finalResponse,
		finalError:           test.err,
	}
	// Let's replace the test writer with the mockJSServer
	rt.controller.writerCreator = func(int32) lib.ClientWriter {
		return mock
	}

	// Get the client that is relevant to the controller so it talks
	// to the right mounttable.
	client := veyron2.GetClient(rt.controller.Context())

	if err != nil {
		t.Errorf("unable to create client: %v", err)
	}

	call, err := client.StartCall(rt.controller.Context(), "adder/adder", test.method, test.inArgs)
	if err != nil {
		t.Errorf("failed to start call: %v", err)
	}

	for _, msg := range test.clientStream {
		if err := call.Send(msg); err != nil {
			t.Errorf("unexpected error while sending %v: %v", msg, err)
		}
	}
	if err := call.CloseSend(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	expectedStream := test.serverStream
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

	var result interface{}
	var err2 error

	err = call.Finish(&result, &err2)

	// If err is nil and test.authErr is nil reflect.DeepEqual will return
	// false because the types are different.  Because of this, we only use
	// reflect.DeepEqual if one of the values is non-nil.  If both values
	// are nil, then we consider them equal.
	if (err != nil || test.authError != nil) && !verror.Equal(err, test.authError) {
		t.Errorf("unexpected err: got %#v, expected %#v", err, test.authError)
	}

	if err != nil {
		return
	}

	if !reflect.DeepEqual(result, test.finalResponse) {
		t.Errorf("unexected final response: got %v, expected %v", result, test.finalResponse)
	}

	if (err2 != nil || test.err != nil) && !verror.Equal(err2, test.err) {
		t.Errorf("unexpected error: got %#v, expected %#v", err2, test.err)
	}
}

func TestSimpleJSServer(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{int32(1), int32(2)},
		finalResponse: int32(3),
	})
}

func TestJSServerWithAuthorizer(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{int32(1), int32(2)},
		finalResponse: int32(3),
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
		finalResponse: int32(3),
		hasAuthorizer: true,
		authError:     err,
	})
}
func TestJSServerWihStreamingInputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "StreamingAdd",
		clientStream:  []interface{}{int32(3), int32(4)},
		finalResponse: int32(10),
	})
}

func TestJSServerWihStreamingOutputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "StreamingAdd",
		serverStream:  []interface{}{int32(3), int32(4)},
		finalResponse: int32(10),
	})
}

func TestJSServerWihStreamingInputsAndOutputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "StreamingAdd",
		clientStream:  []interface{}{int32(1), int32(2)},
		serverStream:  []interface{}{int32(3), int32(4)},
		finalResponse: int32(10),
	})
}

func TestJSServerWithWrongNumberOfArgs(t *testing.T) {
	err := verror.New(server.ErrWrongNumberOfArgs, nil, "Add", 3, 2)
	runJsServerTestCase(t, jsServerTestCase{
		method:    "Add",
		inArgs:    []interface{}{int32(1), int32(2), int32(3)},
		authError: err,
	})
}

func TestJSServerWithMethodNotFound(t *testing.T) {
	methodName := "UnknownMethod"
	err := verror.New(server.ErrMethodNotFoundInSignature, nil, methodName)
	runJsServerTestCase(t, jsServerTestCase{
		method:    methodName,
		inArgs:    []interface{}{int32(1), int32(2)},
		authError: err,
	})
}
