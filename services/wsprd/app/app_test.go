package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vom"
	vom_wiretype "veyron.io/veyron/veyron2/vom/wiretype"
	"veyron.io/veyron/veyron2/wiretype"
	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/lib/testwriter"
	"veyron.io/wspr/veyron/services/wsprd/signature"

	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/proxy"
	mounttable "veyron.io/veyron/veyron/services/mounttable/lib"
)

var r = rt.Init()

type simpleAdder struct{}

func (s simpleAdder) Add(_ ipc.ServerCall, a, b int32) (int32, error) {
	return a + b, nil
}

func (s simpleAdder) Divide(_ ipc.ServerCall, a, b int32) (int32, error) {
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

func (s simpleAdder) Signature(call ipc.ServerCall) (ipc.ServiceSignature, error) {
	result := ipc.ServiceSignature{Methods: make(map[string]ipc.MethodSignature)}
	result.Methods["Add"] = ipc.MethodSignature{
		InArgs: []ipc.MethodArgument{
			{Name: "A", Type: 36},
			{Name: "B", Type: 36},
		},
		OutArgs: []ipc.MethodArgument{
			{Name: "Value", Type: 36},
			{Name: "Err", Type: 65},
		},
	}

	result.Methods["Divide"] = ipc.MethodSignature{
		InArgs: []ipc.MethodArgument{
			{Name: "A", Type: 36},
			{Name: "B", Type: 36},
		},
		OutArgs: []ipc.MethodArgument{
			{Name: "Value", Type: 36},
			{Name: "Err", Type: 65},
		},
	}

	result.Methods["StreamingAdd"] = ipc.MethodSignature{
		InArgs: []ipc.MethodArgument{},
		OutArgs: []ipc.MethodArgument{
			{Name: "Value", Type: 36},
			{Name: "Err", Type: 65},
		},
		InStream:  36,
		OutStream: 36,
	}
	result.TypeDefs = []vdlutil.Any{
		wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

func startAnyServer(servesMT bool, dispatcher ipc.Dispatcher) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := r.NewServer(veyron2.ServesMountTableOpt(servesMT))
	if err != nil {
		return nil, nil, err
	}

	endpoint, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}

	if err := s.Serve("", dispatcher); err != nil {
		return nil, nil, err
	}
	return s, endpoint, nil
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

var adderServiceSignature signature.JSONServiceSignature = signature.JSONServiceSignature{
	"add": signature.JSONMethodSignature{
		InArgs:      []string{"A", "B"},
		NumOutArgs:  2,
		IsStreaming: false,
	},
	"divide": signature.JSONMethodSignature{
		InArgs:      []string{"A", "B"},
		NumOutArgs:  2,
		IsStreaming: false,
	},
	"streamingAdd": signature.JSONMethodSignature{
		InArgs:      []string{},
		NumOutArgs:  2,
		IsStreaming: true,
	},
}

func TestGetGoServerSignature(t *testing.T) {
	s, endpoint, err := startAdderServer()
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()
	spec := *profiles.LocalListenSpec
	spec.Proxy = "mockVeyronProxyEP"
	controller, err := NewController(nil, &spec)

	if err != nil {
		t.Errorf("Failed to create controller: %v", err)
	}
	jsSig, err := controller.getSignature(r.NewContext(), "/"+endpoint.String())
	if err != nil {
		t.Errorf("Failed to get signature: %v", err)
	}

	if !reflect.DeepEqual(jsSig, adderServiceSignature) {
		t.Errorf("Unexpected signature, got :%v, expected: %v", jsSig, adderServiceSignature)
	}
}

type goServerTestCase struct {
	method             string
	inArgs             []json.RawMessage
	numOutArgs         int32
	streamingInputs    []string
	streamingInputType vom.Type
	expectedStream     []testwriter.Response
	expectedError      error
}

func runGoServerTestCase(t *testing.T, test goServerTestCase) {
	s, endpoint, err := startAdderServer()
	if err != nil {
		t.Errorf("unable to start server: %v", err)
		t.Fail()
		return
	}
	defer s.Stop()

	spec := *profiles.LocalListenSpec
	spec.Proxy = "mockVeyronProxyEP"
	controller, err := NewController(nil, &spec)

	if err != nil {
		t.Errorf("unable to create controller: %v", err)
		t.Fail()
		return
	}

	writer := testwriter.Writer{}
	var stream *outstandingStream
	if len(test.streamingInputs) > 0 {
		stream = newStream()
		controller.outstandingStreams[0] = stream
		go func() {
			for _, value := range test.streamingInputs {
				controller.SendOnStream(0, value, &writer)
			}
			controller.CloseStream(0)
		}()
	}

	request := veyronTempRPC{
		Name:        "/" + endpoint.String(),
		Method:      test.method,
		InArgs:      test.inArgs,
		NumOutArgs:  test.numOutArgs,
		IsStreaming: stream != nil,
	}
	controller.sendVeyronRequest(r.NewContext(), 0, &request, &writer, stream)

	testwriter.CheckResponses(&writer, test.expectedStream, test.expectedError, t)
}

func TestCallingGoServer(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:     "Add",
		inArgs:     []json.RawMessage{json.RawMessage("2"), json.RawMessage("3")},
		numOutArgs: 2,
		expectedStream: []testwriter.Response{
			testwriter.Response{
				Message: []interface{}{5.0},
				Type:    lib.ResponseFinal,
			},
		},
	})
}

func TestCallingGoServerWithError(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:        "Divide",
		inArgs:        []json.RawMessage{json.RawMessage("1"), json.RawMessage("0")},
		numOutArgs:    2,
		expectedError: verror2.Make(verror2.BadArg, nil, "div 0"),
	})
}

func TestCallingGoWithStreaming(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:             "StreamingAdd",
		inArgs:             []json.RawMessage{},
		streamingInputs:    []string{"1", "2", "3", "4"},
		streamingInputType: vom_wiretype.Type{ID: 36},
		numOutArgs:         2,
		expectedStream: []testwriter.Response{
			testwriter.Response{
				Message: 1.0,
				Type:    lib.ResponseStream,
			},
			testwriter.Response{
				Message: 3.0,
				Type:    lib.ResponseStream,
			},
			testwriter.Response{
				Message: 6.0,
				Type:    lib.ResponseStream,
			},
			testwriter.Response{
				Message: 10.0,
				Type:    lib.ResponseStream,
			},
			testwriter.Response{
				Message: nil,
				Type:    lib.ResponseStreamClose,
			},
			testwriter.Response{
				Message: []interface{}{10.0},
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

	writerCreator := func(int64) lib.ClientWriter {
		return &writer
	}
	spec := *profiles.LocalListenSpec
	spec.Proxy = "/" + proxyEndpoint
	controller, err := NewController(writerCreator, &spec, veyron2.NamespaceRoots{"/" + endpoint.String()})

	if err != nil {
		return nil, err
	}

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

	if msg, ok := resp.Message.(string); ok {
		if _, err := r.NewEndpoint(msg); err == nil {
			return
		}
	}
	t.Errorf("invalid endpdoint returned from serve: %v", resp.Message)
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
	serverStream []string
	// The stream that we expect the client to see.
	expectedServerStream []interface{}
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

func sendServerStream(t *testing.T, controller *Controller, test *jsServerTestCase, w lib.ClientWriter, expectedFlow int64) {
	for _, msg := range test.serverStream {
		controller.SendOnStream(4, msg, w)
	}

	serverReply := map[string]interface{}{
		"Results": []interface{}{test.finalResponse},
		"Err":     test.err,
	}

	bytes, err := json.Marshal(serverReply)
	if err != nil {
		t.Fatalf("Failed to serialize the reply: %v", err)
	}
	controller.HandleServerResponse(expectedFlow, string(bytes))
}

// Replaces the "remoteEndpoint" in security context of the message
// passed in with a constant string "remoteEndpoint" since there is
// no way to get the endpoint of the client.
func cleanUpAuthRequest(message *testwriter.Response, t *testing.T) {
	// We should make sure that remoteEndpoint exists in the last message and
	// change it to a fixed string.
	if message.Type != lib.ResponseAuthRequest {
		t.Errorf("Unexpected auth message %v", message)
		return
	}
	context := message.Message.(map[string]interface{})["context"].(map[string]interface{})
	if context["remoteEndpoint"] == nil || context["remoteEndpoint"] == "" {
		t.Errorf("Unexpected auth message %v", message)
	}
	context["remoteEndpoint"] = "remoteEndpoint"
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

	msg, ok := resp.Message.(string)
	if !ok {
		t.Errorf("invalid endpdoint returned from serve: %v", resp.Message)
	}
	endpoint, err := r.NewEndpoint(msg)
	if err != nil {
		t.Errorf("invalid endpdoint returned from serve: %v", resp.Message)
	}

	rt.writer.Stream = nil

	// Create a client using app's runtime so it points to the right mounttable.
	client, err := rt.controller.rt.NewClient()

	if err != nil {
		t.Errorf("unable to create client: %v", err)
	}

	expectedWebsocketMessage := []testwriter.Response{
		testwriter.Response{
			Type: lib.ResponseDispatcherLookup,
			Message: map[string]interface{}{
				"serverId": 0.0,
				"suffix":   "adder",
				"method":   "resolveStep",
			},
		},
	}

	// We have to have a go routine handle the resolveStep call because StartCall blocks until the
	// resolve step is complete.
	go func() {
		// Wait until ResolveStep lookup has been called.
		if err := rt.writer.WaitForMessage(len(expectedWebsocketMessage)); err != nil {
			t.Errorf("didn't receive expected message: %v", err)
		}

		// Handle the ResolveStep
		dispatcherResponse := map[string]interface{}{
			"Err": map[string]interface{}{
				"id":      "veyron2/verror.NotFound",
				"message": "ResolveStep not found",
			},
		}
		bytes, err := json.Marshal(dispatcherResponse)
		if err != nil {
			t.Errorf("failed to serailize the response: %v", err)
			return
		}
		rt.controller.HandleLookupResponse(0, string(bytes))
	}()

	call, err := client.StartCall(rt.controller.rt.NewContext(), "/"+msg+"/adder", test.method, test.inArgs)
	if err != nil {
		t.Errorf("failed to start call: %v", err)
	}

	// This is lookup call for dispatcher.
	expectedWebsocketMessage = append(expectedWebsocketMessage, testwriter.Response{
		Type: lib.ResponseDispatcherLookup,
		Message: map[string]interface{}{
			"serverId": 0.0,
			"suffix":   "adder",
			"method":   lib.LowercaseFirstCharacter(test.method),
		},
	})

	if err := rt.writer.WaitForMessage(len(expectedWebsocketMessage)); err != nil {
		t.Errorf("didn't receive expected message: %v", err)
	}

	dispatcherResponse := map[string]interface{}{
		"handle":        0,
		"signature":     adderServiceSignature,
		"hasAuthorizer": test.hasAuthorizer,
	}

	bytes, err := json.Marshal(dispatcherResponse)
	if err != nil {
		t.Errorf("failed to serailize the response: %v", err)
		return
	}
	rt.controller.HandleLookupResponse(2, string(bytes))

	typedNames := rt.controller.rt.Identity().PublicID().Names()
	names := []interface{}{}
	for _, n := range typedNames {
		names = append(names, n)
	}

	// The expectedHandle for the javascript ID.  Since we don't always call the authorizer
	// this handle could be different by the time we make the start rpc call.
	expectedIDHandle := 1.0
	expectedFlowCount := int64(4)
	if test.hasAuthorizer {
		// If an authorizer exists, it gets called twice.  The first time to see if the
		// client is actually able to make this rpc and a second time to see if the server
		// is ok with the client getting trace information for the rpc.
		expectedWebsocketMessage = append(expectedWebsocketMessage, testwriter.Response{
			Type: lib.ResponseAuthRequest,
			Message: map[string]interface{}{
				"serverID": 0.0,
				"handle":   0.0,
				"context": map[string]interface{}{
					"method": lib.LowercaseFirstCharacter(test.method),
					"name":   "adder",
					"suffix": "adder",
					"label":  8.0, // This is a read label.
					"localId": map[string]interface{}{
						"Handle": 1.0,
						"Names":  names,
					},
					"remoteId": map[string]interface{}{
						"Handle": 2.0,
						"Names":  names,
					},
					"localEndpoint":  endpoint.String(),
					"remoteEndpoint": "remoteEndpoint",
				},
			},
		})
		if err := rt.writer.WaitForMessage(len(expectedWebsocketMessage)); err != nil {
			t.Errorf("didn't receive expected message: %v", err)
		}

		cleanUpAuthRequest(&rt.writer.Stream[len(expectedWebsocketMessage)-1], t)
		authResponse := map[string]interface{}{
			"err": test.authError,
		}

		bytes, err := json.Marshal(authResponse)
		if err != nil {
			t.Errorf("failed to serailize the response: %v", err)
			return
		}
		rt.controller.HandleAuthResponse(4, string(bytes))

		// If we expected an auth error, we should go ahead and finish this rpc to get back
		// the auth error.
		if test.authError != nil {
			var result interface{}
			var err2 error
			err := call.Finish(&result, &err2)
			testwriter.CheckResponses(rt.writer, expectedWebsocketMessage, nil, t)
			// We can't do a deep equal with authError because the error returned by the
			// authorizer is wrapped into another error by the ipc framework.
			if err == nil {
				t.Errorf("unexpected auth error, expected %v, got %v", test.authError, err)
			}
			return
		}

		// The debug authorize call is identical to the regular auth call with a different
		// label.
		expectedWebsocketMessage = append(expectedWebsocketMessage, testwriter.Response{
			Type: lib.ResponseAuthRequest,
			Message: map[string]interface{}{
				"serverID": 0.0,
				"handle":   0.0,
				"context": map[string]interface{}{
					"method": lib.LowercaseFirstCharacter(test.method),
					"name":   "adder",
					"suffix": "adder",
					"label":  16.0,
					"localId": map[string]interface{}{
						"Handle": 3.0,
						"Names":  names,
					},
					"remoteId": map[string]interface{}{
						"Handle": 4.0,
						"Names":  names,
					},
					"localEndpoint":  endpoint.String(),
					"remoteEndpoint": "remoteEndpoint",
				},
			},
		})

		if err := rt.writer.WaitForMessage(len(expectedWebsocketMessage)); err != nil {
			t.Errorf("didn't receive expected message: %v", err)
		}

		cleanUpAuthRequest(&rt.writer.Stream[len(expectedWebsocketMessage)-1], t)
		authResponse = map[string]interface{}{}

		bytes, err = json.Marshal(authResponse)
		if err != nil {
			t.Errorf("failed to serailize the response: %v", err)
			return
		}
		rt.controller.HandleAuthResponse(6, string(bytes))

		expectedIDHandle += 4
		expectedFlowCount += 4
	}

	// Now we expect the rpc to be invoked on the Javascript server.
	expectedWebsocketMessage = append(expectedWebsocketMessage, testwriter.Response{
		Type: lib.ResponseServerRequest,
		Message: map[string]interface{}{
			"ServerId": 0.0,
			"Method":   lib.LowercaseFirstCharacter(test.method),
			"Handle":   0.0,
			"Args":     test.inArgs,
			"Context": map[string]interface{}{
				"Name":   "adder",
				"Suffix": "adder",
				"RemoteID": map[string]interface{}{
					"Handle": expectedIDHandle,
					"Names":  names,
				},
			},
		},
	})

	// Wait until the rpc has started.
	if err := rt.writer.WaitForMessage(len(expectedWebsocketMessage)); err != nil {
		t.Errorf("didn't receive expected message: %v", err)
	}
	for _, msg := range test.clientStream {
		expectedWebsocketMessage = append(expectedWebsocketMessage, testwriter.Response{Type: lib.ResponseStream, Message: msg})
		if err := call.Send(msg); err != nil {
			t.Errorf("unexpected error while sending %v: %v", msg, err)
		}
	}

	// Wait until all the streaming messages have been acknowledged.
	if err := rt.writer.WaitForMessage(len(expectedWebsocketMessage)); err != nil {
		t.Errorf("didn't receive expected message: %v", err)
	}

	expectedWebsocketMessage = append(expectedWebsocketMessage, testwriter.Response{Type: lib.ResponseStreamClose})

	expectedStream := test.expectedServerStream
	go sendServerStream(t, rt.controller, &test, rt.writer, expectedFlowCount)
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

	if err := call.Finish(&result, &err2); err != nil {
		t.Errorf("unexpected err :%v", err)
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

	// Wait until the close streaming messages have been acknowledged.
	if err := rt.writer.WaitForMessage(len(expectedWebsocketMessage)); err != nil {
		t.Errorf("didn't receive expected message: %v", err)
	}

	testwriter.CheckResponses(rt.writer, expectedWebsocketMessage, nil, t)
}

func TestSimpleJSServer(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{1.0, 2.0},
		finalResponse: 3.0,
	})
}

func TestJSServerWithAuthorizer(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{1.0, 2.0},
		finalResponse: 3.0,
		hasAuthorizer: true,
	})
}

func TestJSServerWithError(t *testing.T) {
	err := verror2.Make(verror2.Internal, nil)
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{1.0, 2.0},
		finalResponse: 3.0,
		err:           err,
	})
}

func TestJSServerWithAuthorizerAndAuthError(t *testing.T) {
	err := verror2.Make(verror2.Internal, nil)
	runJsServerTestCase(t, jsServerTestCase{
		method:        "Add",
		inArgs:        []interface{}{1.0, 2.0},
		finalResponse: 3.0,
		hasAuthorizer: true,
		authError:     err,
	})
}
func TestJSServerWihStreamingInputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:        "StreamingAdd",
		inArgs:        []interface{}{},
		clientStream:  []interface{}{3.0, 4.0},
		finalResponse: 10.0,
	})
}

func TestJSServerWihStreamingOutputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:               "StreamingAdd",
		inArgs:               []interface{}{},
		serverStream:         []string{"3", "4"},
		expectedServerStream: []interface{}{3.0, 4.0},
		finalResponse:        10.0,
	})
}

func TestJSServerWihStreamingInputsAndOutputs(t *testing.T) {
	runJsServerTestCase(t, jsServerTestCase{
		method:               "StreamingAdd",
		inArgs:               []interface{}{},
		clientStream:         []interface{}{1.0, 2.0},
		serverStream:         []string{"3", "4"},
		expectedServerStream: []interface{}{3.0, 4.0},
		finalResponse:        10.0,
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
		{
			json:          `{"_type":"PeerBlessingsCaveat","service":"...","data":["veyron.io/veyron/veyron/batman","veyron.io/veyron/veyron/brucewayne"]}`,
			expectedValue: C(security.PeerBlessingsCaveat("veyron.io/veyron/veyron/batman", "veyron.io/veyron/veyron/brucewayne")),
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
