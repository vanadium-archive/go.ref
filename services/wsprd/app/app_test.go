package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/options"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/security"
	"v.io/veyron/veyron2/vdl"
	"v.io/veyron/veyron2/vdl/vdlroot/src/signature"
	"v.io/veyron/veyron2/verror2"
	"v.io/wspr/veyron/services/wsprd/lib"
	"v.io/wspr/veyron/services/wsprd/lib/testwriter"

	tsecurity "v.io/veyron/veyron/lib/testutil/security"
	"v.io/veyron/veyron/profiles"
	"v.io/veyron/veyron/runtimes/google/ipc/stream/proxy"
	vsecurity "v.io/veyron/veyron/security"
	mounttable "v.io/veyron/veyron/services/mounttable/lib"
)

var (
	testPrincipalBlessing = "test"
	testPrincipal         = newPrincipal(testPrincipalBlessing)
	r                     veyron2.Runtime
)

func init() {
	var err error
	if r, err = rt.New(); err != nil {
		panic(err)
	}
}

// newBlessedPrincipal returns a new principal that has a blessing from the
// provided runtime's principal which is set on its BlessingStore such
// that it is revealed to all clients and servers.
func newBlessedPrincipal(r veyron2.Runtime) security.Principal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	b, err := r.Principal().Bless(p.PublicKey(), r.Principal().BlessingStore().Default(), "delegate", security.UnconstrainedUse())
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
		return 0, verror2.Make(verror2.BadArg, nil, "div 0")
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

var simpleAddrSig = []signature.Interface{
	{
		Doc: "The empty interface contains methods not attached to any interface.",
		Methods: []signature.Method{
			{
				Name:    "Add",
				InArgs:  []signature.Arg{{Type: vdl.Int32Type}, {Type: vdl.Int32Type}},
				OutArgs: []signature.Arg{{Type: vdl.Int32Type}, {Type: vdl.ErrorType}},
			},
			{
				Name:    "Divide",
				InArgs:  []signature.Arg{{Type: vdl.Int32Type}, {Type: vdl.Int32Type}},
				OutArgs: []signature.Arg{{Type: vdl.Int32Type}, {Type: vdl.ErrorType}},
			},
			{
				Name:      "StreamingAdd",
				OutArgs:   []signature.Arg{{Type: vdl.Int32Type}, {Type: vdl.ErrorType}},
				InStream:  &signature.Arg{Type: vdl.AnyType},
				OutStream: &signature.Arg{Type: vdl.AnyType},
			},
		},
	},
}

func startAnyServer(servesMT bool, dispatcher ipc.Dispatcher) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := r.NewServer(options.ServesMountTable(servesMT))
	if err != nil {
		return nil, nil, err
	}

	endpoints, err := s.Listen(profiles.LocalListenSpec)
	if err != nil {
		return nil, nil, err
	}

	if err := s.ServeDispatcher("", dispatcher); err != nil {
		return nil, nil, err
	}
	return s, endpoints[0], nil
}

func startAdderServer() (ipc.Server, naming.Endpoint, error) {
	return startAnyServer(false, ipc.LeafDispatcher(simpleAdder{}, nil))
}

func startProxy() (*proxy.Proxy, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	return proxy.New(rid, nil, "tcp", "127.0.0.1:0", "")
}

func startMountTableServer() (ipc.Server, naming.Endpoint, error) {
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		return nil, nil, err
	}
	return startAnyServer(true, mt)
}

func TestGetGoServerSignature(t *testing.T) {
	s, endpoint, err := startAdderServer()
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()
	spec := profiles.LocalListenSpec
	spec.Proxy = "mockVeyronProxyEP"
	controller, err := NewController(nil, nil, &spec, nil, options.RuntimePrincipal{newBlessedPrincipal(r)})

	if err != nil {
		t.Fatalf("Failed to create controller: %v", err)
	}
	sig, err := controller.getSignature(r.NewContext(), "/"+endpoint.String())
	if err != nil {
		t.Fatalf("Failed to get signature: %v", err)
	}
	if got, want := sig, simpleAddrSig; !reflect.DeepEqual(got, want) {
		t.Fatalf("Unexpected signature, got :%#v, want: %#v", got, want)
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
	s, endpoint, err := startAdderServer()
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()

	spec := profiles.LocalListenSpec
	spec.Proxy = "mockVeyronProxyEP"
	controller, err := NewController(nil, nil, &spec, nil, options.RuntimePrincipal{newBlessedPrincipal(r)})

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

	request := VeyronRPC{
		Name:        "/" + endpoint.String(),
		Method:      test.method,
		InArgs:      test.inArgs,
		NumOutArgs:  test.numOutArgs,
		IsStreaming: stream != nil,
	}
	controller.sendVeyronRequest(r.NewContext(), 0, &request, &writer, stream)

	if err := testwriter.CheckResponses(&writer, test.expectedStream, test.expectedError); err != nil {
		t.Error(err)
	}
}

func TestCallingGoServer(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:     "Add",
		inArgs:     []interface{}{2, 3},
		numOutArgs: 2,
		expectedStream: []lib.Response{
			lib.Response{
				Message: lib.VomEncodeOrDie([]interface{}{int32(5)}),
				Type:    lib.ResponseFinal,
			},
		},
	})
}

func TestCallingGoServerWithError(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:        "Divide",
		inArgs:        []interface{}{1, 0},
		numOutArgs:    2,
		expectedError: verror2.Make(verror2.BadArg, nil, "div 0"),
	})
}

func TestCallingGoWithStreaming(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:          "StreamingAdd",
		streamingInputs: []interface{}{1, 2, 3, 4},
		numOutArgs:      2,
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
				Message: lib.VomEncodeOrDie([]interface{}{int32(10)}),
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

func serveServer() (*runningTest, error) {
	mounttableServer, endpoint, err := startMountTableServer()

	if err != nil {
		return nil, fmt.Errorf("unable to start mounttable: %v", err)
	}

	proxyServer, err := startProxy()

	if err != nil {
		return nil, fmt.Errorf("unable to start proxy: %v", err)
	}

	proxyEndpoint := proxyServer.Endpoint().String()

	writer := testwriter.Writer{}

	writerCreator := func(int32) lib.ClientWriter {
		return &writer
	}
	spec := profiles.LocalListenSpec
	spec.Proxy = "/" + proxyEndpoint
	controller, err := NewController(writerCreator, nil, &spec, nil, options.RuntimePrincipal{testPrincipal})

	if err != nil {
		return nil, err
	}
	controller.rt.Namespace().SetRoots("/" + endpoint.String())

	controller.serve(serveRequest{
		Name: "adder",
	}, &writer)

	return &runningTest{
		controller, &writer, mounttableServer, proxyServer,
	}, nil
}

func TestJavascriptServeServer(t *testing.T) {
	rt, err := serveServer()
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
	rt, err := serveServer()
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
	err verror2.E

	// Whether or not the Javascript server has an authorizer or not.
	// If it does have an authorizer, then authError is sent back from the server
	// to the app.
	hasAuthorizer bool
	authError     verror2.E
}

func runJsServerTestCase(t *testing.T, test jsServerTestCase) {
	rt, err := serveServer()
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
		serviceSignature:     simpleAddrSig,
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

	// Create a client using app's runtime so it points to the right mounttable.
	client, err := rt.controller.rt.NewClient()

	if err != nil {
		t.Errorf("unable to create client: %v", err)
	}

	call, err := client.StartCall(rt.controller.rt.NewContext(), "adder/adder", test.method, test.inArgs)
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
	if (err == nil && test.authError != nil) || (err != nil && test.authError == nil) {
		t.Errorf("unexpected err: %v, %v", err, test.authError)
	}

	if err != nil {
		return
	}
	if !reflect.DeepEqual(result, test.finalResponse) {
		t.Errorf("unexected final response: got %v, expected %v", result, test.finalResponse)
	}

	// If err2 is nil and test.err is nil reflect.DeepEqual will return false because the
	// types are different.  Because of this, we only use reflect.DeepEqual if one of
	// the values is non-nil.  If both values are nil, then we consider them equal.
	if (err2 != nil || test.err != nil) && !verror2.Equal(err2, test.err) {
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
	err := verror2.Make(verror2.Internal, nil)
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{int32(1), int32(2)},
		finalResponse: int32(3),
		err:           err,
	})
}

func TestJSServerWithAuthorizerAndAuthError(t *testing.T) {
	err := verror2.Make(verror2.Internal, nil)
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

func TestDeserializeCaveat(t *testing.T) {
	C := func(cav security.Caveat, err error) security.Caveat {
		if err != nil {
			t.Fatal(err)
		}
		return cav
	}
	testCases := []struct {
		json          string
		expectedValue security.Caveat
	}{
		{
			json:          `{"_type":"MethodCaveat","service":"...","data":["Get","MultiGet"]}`,
			expectedValue: C(security.MethodCaveat("Get", "MultiGet")),
		},
	}

	for _, c := range testCases {
		var s jsonCaveatValidator
		if err := json.Unmarshal([]byte(c.json), &s); err != nil {
			t.Errorf("Failed to deserialize object: %v", err)
			return
		}

		caveat, err := decodeCaveat(s)
		if err != nil {
			t.Errorf("Failed to convert json caveat to go object: %v")
			return
		}

		if !reflect.DeepEqual(caveat, c.expectedValue) {
			t.Errorf("decoded produced the wrong value: got %v, expected %v", caveat, c.expectedValue)
		}
	}
}
