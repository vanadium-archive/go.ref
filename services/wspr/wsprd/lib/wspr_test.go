package lib

import (
	"bytes"
	"encoding/json"
	"log"
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

func startServer() (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		return nil, nil, err
	}

	// Register the "fortune" prefix with the fortune dispatcher.
	if err := s.Register("cache", ipc.SoloDispatcher(simpleAdder{}, nil)); err != nil {
		return nil, nil, err
	}

	endpoint, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	return s, endpoint, nil
}

type testWriter struct {
	t              *testing.T
	expectedStream []response
	buf            bytes.Buffer
	expectedError  error
	logger         vlog.Logger
}

func (t *testWriter) Write(p []byte) (int, error) {
	return t.buf.Write(p)

}

func (t *testWriter) getLogger() vlog.Logger {
	return t.logger
}

func (t *testWriter) sendError(err error) {
	if !reflect.DeepEqual(err, t.expectedError) {
		t.t.Errorf("unexpected error, got: %v, expected: %v", err, t.expectedError)
	}
}

func (t *testWriter) FinishMessage() error {
	var r response
	p := t.buf.Bytes()
	t.buf.Reset()
	log.Println("Finishing", string(p))
	if err := json.Unmarshal(p, &r); err != nil {
		return err
	}

	if len(t.expectedStream) == 0 {
		t.t.Errorf("Unexpected message %v", r)
		return nil
	}
	expectedValue := t.expectedStream[0]
	t.expectedStream = t.expectedStream[1:]
	if !reflect.DeepEqual(r, expectedValue) {
		t.t.Errorf("unknown stream message Got: %v, expected: %v", r, expectedValue)
	}
	return nil
}

func TestGetGoServerSignature(t *testing.T) {
	s, endpoint, err := startServer()
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
	if len(jsSig) != 3 {
		t.Errorf("Wrong number of methods, got: %d, expected: 3", len(jsSig))
	}

	add := jsSig["add"]
	if len(add.InArgs) != 2 {
		t.Errorf("Wrong number of arguments for put, got: %d, expected: 2", len(add.InArgs))
	}

	divide := jsSig["divide"]
	if len(divide.InArgs) != 2 {
		t.Errorf("Wrong number of arguments for put, got: %d, expected: 2", len(divide.InArgs))
	}
}

type testCase struct {
	method          string
	inArgs          []interface{}
	numOutArgs      int32
	streamingInputs []string
	expectedStream  []response
	expectedError   error
}

func runTestCase(t *testing.T, test testCase) {
	s, endpoint, err := startServer()
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
		t:              t,
		expectedStream: test.expectedStream,
		expectedError:  test.expectedError,
		logger:         wspr.logger,
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
}

func TestCallingGoServer(t *testing.T) {
	runTestCase(t, testCase{
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
	runTestCase(t, testCase{
		method:        "Divide",
		inArgs:        []interface{}{1, 0},
		numOutArgs:    2,
		expectedError: verror.BadArgf("can't divide by zero"),
	})
}

func TestCallingGoWithStreaming(t *testing.T) {
	runTestCase(t, testCase{
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

// TODO(bjornick): Make sure that send on stream is nonblocking
