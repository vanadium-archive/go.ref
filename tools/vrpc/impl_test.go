package main

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/vlog"

	"v.io/lib/cmdline"
	"v.io/veyron/veyron/profiles"
	"v.io/veyron/veyron/tools/vrpc/test_base"
)

type server struct{}

// TypeTester interface implementation

func (*server) EchoBool(call ipc.ServerContext, i1 bool) (bool, error) {
	vlog.VI(2).Info("EchoBool(%v) was called.", i1)
	return i1, nil
}

func (*server) EchoFloat32(call ipc.ServerContext, i1 float32) (float32, error) {
	vlog.VI(2).Info("EchoFloat32(%u) was called.", i1)
	return i1, nil
}

func (*server) EchoFloat64(call ipc.ServerContext, i1 float64) (float64, error) {
	vlog.VI(2).Info("EchoFloat64(%u) was called.", i1)
	return i1, nil
}

func (*server) EchoInt32(call ipc.ServerContext, i1 int32) (int32, error) {
	vlog.VI(2).Info("EchoInt32(%v) was called.", i1)
	return i1, nil
}

func (*server) EchoInt64(call ipc.ServerContext, i1 int64) (int64, error) {
	vlog.VI(2).Info("EchoInt64(%v) was called.", i1)
	return i1, nil
}

func (*server) EchoString(call ipc.ServerContext, i1 string) (string, error) {
	vlog.VI(2).Info("EchoString(%v) was called.", i1)
	return i1, nil
}

func (*server) EchoByte(call ipc.ServerContext, i1 byte) (byte, error) {
	vlog.VI(2).Info("EchoByte(%v) was called.", i1)
	return i1, nil
}

func (*server) EchoUInt32(call ipc.ServerContext, i1 uint32) (uint32, error) {
	vlog.VI(2).Info("EchoUInt32(%u) was called.", i1)
	return i1, nil
}

func (*server) EchoUInt64(call ipc.ServerContext, i1 uint64) (uint64, error) {
	vlog.VI(2).Info("EchoUInt64(%u) was called.", i1)
	return i1, nil
}

func (*server) InputArray(call ipc.ServerContext, i1 [2]uint8) error {
	vlog.VI(2).Info("CInputArray(%v) was called.", i1)
	return nil
}

func (*server) OutputArray(call ipc.ServerContext) ([2]uint8, error) {
	vlog.VI(2).Info("COutputArray() was called.")
	return [2]uint8{1, 2}, nil
}

func (*server) InputMap(call ipc.ServerContext, i1 map[uint8]uint8) error {
	vlog.VI(2).Info("CInputMap(%v) was called.", i1)
	return nil
}

func (*server) OutputMap(call ipc.ServerContext) (map[uint8]uint8, error) {
	vlog.VI(2).Info("COutputMap() was called.")
	return map[uint8]uint8{1: 2}, nil
}

func (*server) InputSlice(call ipc.ServerContext, i1 []uint8) error {
	vlog.VI(2).Info("CInputSlice(%v) was called.", i1)
	return nil
}

func (*server) OutputSlice(call ipc.ServerContext) ([]uint8, error) {
	vlog.VI(2).Info("COutputSlice() was called.")
	return []uint8{1, 2}, nil
}

func (*server) InputStruct(call ipc.ServerContext, i1 test_base.Struct) error {
	vlog.VI(2).Info("CInputStruct(%v) was called.", i1)
	return nil
}

func (*server) OutputStruct(call ipc.ServerContext) (test_base.Struct, error) {
	vlog.VI(2).Info("COutputStruct() was called.")
	return test_base.Struct{X: 1, Y: 2}, nil
}

func (*server) NoArguments(call ipc.ServerContext) error {
	vlog.VI(2).Info("NoArguments() was called.")
	return nil
}

func (*server) MultipleArguments(call ipc.ServerContext, i1, i2 int32) (int32, int32, error) {
	vlog.VI(2).Info("MultipleArguments(%v,%v) was called.", i1, i2)
	return i1, i2, nil
}

func (*server) StreamingOutput(ctx test_base.TypeTesterStreamingOutputContext, nStream int32, item bool) error {
	vlog.VI(2).Info("StreamingOutput(%v,%v) was called.", nStream, item)
	sender := ctx.SendStream()
	for i := int32(0); i < nStream; i++ {
		sender.Send(item)
	}
	return nil
}

func startServer(t *testing.T, r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	obj := test_base.TypeTesterServer(&server{})
	server, err := r.NewServer()
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}

	endpoints, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		return nil, nil, err
	}
	if err := server.Serve("", obj, nil); err != nil {
		t.Errorf("Serve failed: %v", err)
		return nil, nil, err
	}
	return server, endpoints[0], nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func testInvocation(t *testing.T, buffer *bytes.Buffer, cmd *cmdline.Command, args []string, expected string) {
	buffer.Reset()
	if err := cmd.Execute(args); err != nil {
		t.Errorf("%v", err)
		return
	}
	if output := strings.Trim(buffer.String(), "\n"); output != expected {
		t.Errorf("Incorrect invoke output: expected %s, got %s", expected, output)
		return
	}
}

func testError(t *testing.T, cmd *cmdline.Command, args []string, expected string) {
	if err := cmd.Execute(args); err == nil || !strings.Contains(err.Error(), expected) {
		t.Errorf("Expected error: ...%v..., got: %v", expected, err)
	}
}

func TestVRPC(t *testing.T) {
	var err error
	runtime, err = rt.New()
	if err != nil {
		t.Fatalf("Unexpected error initializing runtime: %s", err)
	}
	defer runtime.Cleanup()

	// Skip defer runtime.Cleanup() to avoid messing up other tests in the
	// same process.
	server, endpoint, err := startServer(t, runtime)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)

	name := naming.JoinAddressName(endpoint.String(), "")
	// Test the 'describe' command.
	if err := cmd.Execute([]string{"describe", name}); err != nil {
		t.Errorf("%v", err)
		return
	}

	// TODO(toddw): Switch VRPC to new __Signature, and update these tests.
	expectedSignature := []string{
		"func EchoBool(I1 bool) (O1 bool, err error)",
		"func EchoFloat32(I1 float32) (O1 float32, err error)",
		"func EchoFloat64(I1 float64) (O1 float64, err error)",
		"func EchoInt32(I1 int32) (O1 int32, err error)",
		"func EchoInt64(I1 int64) (O1 int64, err error)",
		"func EchoString(I1 string) (O1 string, err error)",
		"func EchoByte(I1 byte) (O1 byte, err error)",
		"func EchoUInt32(I1 uint32) (O1 uint32, err error)",
		"func EchoUInt64(I1 uint64) (O1 uint64, err error)",
		"func InputArray(I1 [2]byte) (error)",
		"func InputMap(I1 map[byte]byte) (error)",
		"func InputSlice(I1 []byte) (error)",
		"func InputStruct(I1 struct{X int32, Y int32}) (error)",
		"func OutputArray() (O1 [2]byte, err error)",
		"func OutputMap() (O1 map[byte]byte, err error)",
		"func OutputSlice() (O1 []byte, err error)",
		"func OutputStruct() (O1 struct{X int32, Y int32}, err error)",
		"func NoArguments() (error)",
		"func MultipleArguments(I1 int32, I2 int32) (O1 int32, O2 int32, err error)",
		"func StreamingOutput(NumStreamItems int32, StreamItem bool) stream<_, bool> (error)",
	}

	signature := make([]string, 0, len(expectedSignature))
	line, err := stdout.ReadBytes('\n')
	for err == nil {
		signature = append(signature, strings.Trim(string(line), "\n"))
		line, err = stdout.ReadBytes('\n')
	}

	sort.Strings(signature)
	sort.Strings(expectedSignature)

	if len(signature) != len(expectedSignature) {
		t.Fatalf("signature lengths don't match %v and %v.", len(signature), len(expectedSignature))
	}

	for i, expectedSig := range expectedSignature {
		if expectedSig != signature[i] {
			t.Errorf("signature line doesn't match: %v and %v\n", expectedSig, signature[i])
		}
	}

	// Test the 'invoke' command.

	tests := [][]string{
		[]string{"EchoBool", "EchoBool(true) = [true, <nil>]", "[\"bool\",true]"},
		[]string{"EchoFloat32", "EchoFloat32(3.2) = [3.2, <nil>]", "[\"float32\",3.2]"},
		[]string{"EchoFloat64", "EchoFloat64(6.4) = [6.4, <nil>]", "[\"float64\",6.4]"},
		[]string{"EchoInt32", "EchoInt32(-32) = [-32, <nil>]", "[\"int32\",-32]"},
		[]string{"EchoInt64", "EchoInt64(-64) = [-64, <nil>]", "[\"int64\",-64]"},
		[]string{"EchoString", "EchoString(Hello World!) = [Hello World!, <nil>]", "[\"string\",\"Hello World!\"]"},
		[]string{"EchoByte", "EchoByte(8) = [8, <nil>]", "[\"byte\",8]"},
		[]string{"EchoUInt32", "EchoUInt32(32) = [32, <nil>]", "[\"uint32\",32]"},
		[]string{"EchoUInt64", "EchoUInt64(64) = [64, <nil>]", "[\"uint64\",64]"},
		// TODO(jsimsa): The InputArray currently triggers an error in the
		// vom decoder. Benj is looking into this.
		//
		// []string{"InputArray", "InputArray([1 2]) = []", "[\"[2]uint\",[1,2]]"},
		[]string{"InputMap", "InputMap(map[1:2]) = [<nil>]", "[\"map[uint]uint\",{\"1\":\"2\"}]"},
		// TODO(jsimsa): The InputSlice currently triggers an error in the
		// vom decoder. Benj is looking into this.
		//
		// []string{"InputSlice", "InputSlice([1 2]) = []", "[\"[]uint\",[1,2]]"},
		[]string{"InputStruct", "InputStruct({1 2}) = [<nil>]",
			"[\"type\",\"v.io/veyron/veyron2/vrpc/test_base.Struct struct{X int32;Y int32}\"] [\"Struct\",{\"X\":1,\"Y\":2}]"},
		// TODO(jsimsa): The OutputArray currently triggers an error in the
		// vom decoder. Benj is looking into this.
		//
		// []string{"OutputArray", "OutputArray() = [1 2]"}
		[]string{"OutputMap", "OutputMap() = [map[1:2], <nil>]"},
		[]string{"OutputSlice", "OutputSlice() = [[1 2], <nil>]"},
		[]string{"OutputStruct", "OutputStruct() = [{1 2}, <nil>]"},
		[]string{"NoArguments", "NoArguments() = [<nil>]"},
		[]string{"MultipleArguments", "MultipleArguments(1, 2) = [1, 2, <nil>]", "[\"uint32\",1]", "[\"uint32\",2]"},
		[]string{"StreamingOutput", "StreamingOutput(3, true) = <<\n0: true\n1: true\n2: true\n>> [<nil>]", "[\"int8\",3]", "[\"bool\",true ]"},
		[]string{"StreamingOutput", "StreamingOutput(0, true) = [<nil>]", "[\"int8\",0]", "[\"bool\",true ]"},
	}

	for _, test := range tests {
		testInvocation(t, &stdout, cmd, append([]string{"invoke", name, test[0]}, test[2:]...), test[1])
	}

	testErrors := [][]string{
		[]string{"EchoBool", "exit code 1"},
		[]string{"DoesNotExist", "invoke: method DoesNotExist not found"},
	}
	for _, test := range testErrors {
		testError(t, cmd, append([]string{"invoke", name, test[0]}, test[2:]...), test[1])
	}
}
