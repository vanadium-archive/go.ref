package lib

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"veyron2"
	"veyron2/idl"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/wiretype"

	"veyron/runtimes/google/ipc/stream/proxy"
	mounttable "veyron/services/mounttable/lib"
)

var r veyron2.Runtime

func init() {
	r = rt.Init()
}

type simpleAdder struct{}

func (s simpleAdder) Add(_ ipc.ServerCall, a, b int32) (int32, error) {
	return a + b, nil
}

func (s simpleAdder) Divide(_ ipc.ServerCall, a, b int32) (int32, error) {
	if b == 0 {
		return 0, verror.BadArgf("can't divide by zero")
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
	result.TypeDefs = []idl.AnyData{
		wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

// A function that will register an handlers on the given server
type registerFunc func(ipc.Server) error

func startServer(registerer registerFunc) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		return nil, nil, err
	}

	// Register the "fortune" prefix with the fortune dispatcher.
	if err := registerer(s); err != nil {
		return nil, nil, err
	}

	endpoint, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	return s, endpoint, nil
}

func startAdderServer() (ipc.Server, naming.Endpoint, error) {
	return startServer(func(server ipc.Server) error {
		return server.Register("cache", ipc.SoloDispatcher(simpleAdder{}, nil))
	})
}

func startProxy() (*proxy.Proxy, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}

	return proxy.New(rid, nil, "tcp", "127.0.0.1:0")
}

func startMountTableServer() (ipc.Server, naming.Endpoint, error) {
	return startServer(func(server ipc.Server) error {
		mt, err := mounttable.NewMountTable("")
		if err != nil {
			return err
		}
		return server.Register("mt", mt)
	})
}

type testWriter struct {
	stream []response
	buf    bytes.Buffer
	err    error
	logger vlog.Logger
}

func (w *testWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)

}

func (w *testWriter) getLogger() vlog.Logger {
	return w.logger
}

func (w *testWriter) sendError(err error) {
	w.err = err
}

func (w *testWriter) FinishMessage() error {
	var resp response
	p := w.buf.Bytes()
	w.buf.Reset()
	if err := json.Unmarshal(p, &resp); err != nil {
		return err
	}
	w.stream = append(w.stream, resp)
	return nil
}

func checkResponses(w *testWriter, expectedStream []response, err error, t *testing.T) {
	if !reflect.DeepEqual(expectedStream, w.stream) {
		t.Errorf("streams don't match: expected %v, got %v", expectedStream, w.stream)
	}

	if !reflect.DeepEqual(err, w.err) {
		t.Errorf("unexpected error, got: %v, expected: %v", err, w.err)
	}
}

var adderServiceSignature JSONServiceSignature = JSONServiceSignature{
	"add": JSONMethodSignature{
		InArgs:      []string{"A", "B"},
		NumOutArgs:  2,
		IsStreaming: false,
	},
	"divide": JSONMethodSignature{
		InArgs:      []string{"A", "B"},
		NumOutArgs:  2,
		IsStreaming: false,
	},
	"streamingAdd": JSONMethodSignature{
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
	wspr := NewWSPR(0, "mockVeyronProxyEP")
	wspr.setup()
	wsp := websocketPipe{ctx: wspr}
	wsp.setup()
	jsSig, err := wsp.getSignature("/"+endpoint.String()+"/cache", "")
	if err != nil {
		t.Errorf("Failed to get signature: %v", err)
	}

	if !reflect.DeepEqual(jsSig, adderServiceSignature) {
		t.Errorf("Unexpected signature, got :%v, expected: %v", jsSig, adderServiceSignature)
	}
}

type goServerTestCase struct {
	method          string
	inArgs          []interface{}
	numOutArgs      int32
	streamingInputs []string
	expectedStream  []response
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

	wspr := NewWSPR(0, "mockVeyronProxyEP")
	wspr.setup()
	wsp := websocketPipe{ctx: wspr}
	wsp.setup()
	writer := testWriter{
		logger: wspr.logger,
	}

	var signal chan ipc.Stream
	if len(test.streamingInputs) > 0 {
		signal = make(chan ipc.Stream, 1)
		wsp.outstandingStreams[0] = startQueueingStream(signal)
		go func() {
			for _, value := range test.streamingInputs {
				wsp.sendOnStream(0, value, &writer)
			}
			wsp.closeStream(0)
		}()
	}

	request := veyronRPC{
		Name:        "/" + endpoint.String() + "/cache",
		Method:      test.method,
		InArgs:      test.inArgs,
		NumOutArgs:  test.numOutArgs,
		IsStreaming: signal != nil,
	}
	wsp.sendVeyronRequest(0, &request, &writer, signal)
	checkResponses(&writer, test.expectedStream, test.expectedError, t)
}

func TestCallingGoServer(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:     "Add",
		inArgs:     []interface{}{2, 3},
		numOutArgs: 2,
		expectedStream: []response{
			response{
				Message: []interface{}{float64(5)},
				Type:    responseFinal,
			},
		},
	})
}

func TestCallingGoServerWithError(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:        "Divide",
		inArgs:        []interface{}{1, 0},
		numOutArgs:    2,
		expectedError: verror.BadArgf("can't divide by zero"),
	})
}

func TestCallingGoWithStreaming(t *testing.T) {
	runGoServerTestCase(t, goServerTestCase{
		method:          "StreamingAdd",
		inArgs:          []interface{}{},
		streamingInputs: []string{"1", "2", "3", "4"},
		numOutArgs:      2,
		expectedStream: []response{
			response{
				Message: 1.0,
				Type:    responseStream,
			},
			response{
				Message: 3.0,
				Type:    responseStream,
			},
			response{
				Message: 6.0,
				Type:    responseStream,
			},
			response{
				Message: 10.0,
				Type:    responseStream,
			},
			response{
				Message: nil,
				Type:    responseStreamClose,
			},
			response{
				Message: []interface{}{10.0},
				Type:    responseFinal,
			},
		},
	})
}

func TestJavascriptPublish(t *testing.T) {
	mounttableServer, endpoint, err := startMountTableServer()

	if err != nil {
		t.Errorf("unable to start mounttable: %v", err)
		return
	}

	defer mounttableServer.Stop()

	proxyServer, err := startProxy()

	if err != nil {
		t.Errorf("unable to start proxy: %v", err)
		return
	}

	defer proxyServer.Shutdown()

	proxyEndpoint := proxyServer.Endpoint().String()

	wspr := NewWSPR(0, "/"+proxyEndpoint, veyron2.MountTableRoots{"/" + endpoint.String() + "/mt"})
	wspr.setup()
	wsp := websocketPipe{ctx: wspr}
	wsp.setup()
	defer wsp.cleanup()
	writer := testWriter{
		logger: wspr.logger,
	}
	wsp.publish(publishRequest{
		Name: "adder",
		Services: map[string]JSONServiceSignature{
			"adder": adderServiceSignature,
		},
	}, &writer)

	if len(writer.stream) != 1 {
		t.Errorf("expected only on response, got %d", len(writer.stream))
	}

	resp := writer.stream[0]

	if resp.Type != responseFinal {
		t.Errorf("unknown stream message Got: %v, expected: publish response", resp)
		return
	}

	if msg, ok := resp.Message.(string); ok {
		if _, err := r.NewEndpoint(msg); err == nil {
			return
		}

	}
	t.Errorf("invalid endpdoint returned from publish: %v", resp.Message)
}

// TODO(bjornick): Make sure that send on stream is nonblocking
