package browspr

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/proxy"
	mounttable "veyron.io/veyron/veyron/services/mounttable/lib"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/vom2"
	"veyron.io/veyron/veyron2/wiretype"
	"veyron.io/wspr/veyron/services/wsprd/app"
	"veyron.io/wspr/veyron/services/wsprd/lib"
)

var r veyron2.Runtime

func init() {
	var err error
	if r, err = rt.New(); err != nil {
		panic(err)
	}
}

func startProxy() (*proxy.Proxy, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	return proxy.New(rid, nil, "tcp", "127.0.0.1:0", "")
}

func startMounttable() (ipc.Server, naming.Endpoint, error) {
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		return nil, nil, err
	}

	s, err := r.NewServer(options.ServesMountTable(true))
	if err != nil {
		return nil, nil, err
	}

	endpoint, err := s.Listen(profiles.LocalListenSpec)
	if err != nil {
		return nil, nil, err
	}

	if err := s.ServeDispatcher("", mt); err != nil {
		return nil, nil, err
	}

	return s, endpoint, nil
}

type mockServer struct{}

func (s mockServer) BasicCall(_ ipc.ServerCall, txt string) (string, error) {
	return "[" + txt + "]", nil
}

func (s mockServer) Signature(call ipc.ServerCall) (ipc.ServiceSignature, error) {
	result := ipc.ServiceSignature{Methods: make(map[string]ipc.MethodSignature)}
	result.Methods["BasicCall"] = ipc.MethodSignature{
		InArgs: []ipc.MethodArgument{
			{Name: "Txt", Type: 3},
		},
		OutArgs: []ipc.MethodArgument{
			{Name: "Value", Type: 3},
			{Name: "Err", Type: 65},
		},
	}
	result.TypeDefs = []vdlutil.Any{
		wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

func startMockServer(desiredName string) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		return nil, nil, err
	}

	endpoint, err := s.Listen(profiles.LocalListenSpec)
	if err != nil {
		return nil, nil, err
	}

	if err := s.ServeDispatcher(desiredName, ipc.LeafDispatcher(mockServer{}, nil)); err != nil {
		return nil, nil, err
	}

	return s, endpoint, nil
}

type veyronTempRPC struct {
	Name        string
	Method      string
	InArgs      []json.RawMessage
	NumOutArgs  int32
	IsStreaming bool
	Timeout     int64
}

func TestBrowspr(t *testing.T) {
	proxy, err := startProxy()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Shutdown()

	mtServer, mtEndpoint, err := startMounttable()
	if err != nil {
		t.Fatalf("Failed to start mounttable server: %v", err)
	}
	defer mtServer.Stop()
	tcpNamespaceRoot := "/" + mtEndpoint.String()
	if err := r.Namespace().SetRoots(tcpNamespaceRoot); err != nil {
		t.Fatalf("Failed to set namespace roots: %v", err)
	}

	mockServerName := "mock/server"
	mockServer, mockServerEndpoint, err := startMockServer(mockServerName)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer mockServer.Stop()

	names, err := mockServer.Published()
	if err != nil {
		t.Fatalf("Error fetching published names: %v", err)
	}
	if len(names) != 1 || names[0] != tcpNamespaceRoot+"/"+mockServerName {
		t.Fatalf("Incorrectly mounted server. Names: %v", names)
	}
	mountEntry, err := r.Namespace().ResolveX(nil, mockServerName)
	if err != nil {
		t.Fatalf("Error fetching published names from mounttable: %v", err)
	}

	servers := []string{}
	for _, s := range mountEntry.Servers {
		if strings.Index(s.Server, "@tcp") != -1 {
			servers = append(servers, s.Server)
		}
	}
	if len(servers) != 1 || servers[0] != "/"+mockServerEndpoint.String() {
		t.Fatalf("Incorrect names retrieved from mounttable: %v", mountEntry)
	}

	spec := profiles.LocalListenSpec
	spec.Proxy = proxy.Endpoint().String()

	receivedResponse := make(chan bool, 1)
	var receivedInstanceId int32
	var receivedType string
	var receivedMsg string

	var postMessageHandler = func(instanceId int32, ty, msg string) {
		receivedInstanceId = instanceId
		receivedType = ty
		receivedMsg = msg
		receivedResponse <- true
	}

	browspr := NewBrowspr(postMessageHandler, nil, spec, "/mock/identd", []string{tcpNamespaceRoot}, options.RuntimePrincipal{r.Principal()})

	// browspr sets its namespace root to use the "ws" protocol, but we want to force "tcp" here.
	browspr.namespaceRoots = []string{tcpNamespaceRoot}

	principal := browspr.rt.Principal()
	browspr.accountManager.SetMockBlesser(newMockBlesserService(principal))

	msgInstanceId := int32(11)

	rpcMessage := veyronTempRPC{
		Name:   mockServerName,
		Method: "BasicCall",
		InArgs: []json.RawMessage{
			json.RawMessage([]byte("\"InputValue\"")),
		},
		NumOutArgs:  2,
		IsStreaming: false,
		Timeout:     (1 << 31) - 1,
	}

	jsonRpcMessage, err := json.Marshal(rpcMessage)
	if err != nil {
		t.Fatalf("Failed to marshall rpc message to json: %v", err)
	}

	msg, err := json.Marshal(app.Message{
		Id:   1,
		Data: string(jsonRpcMessage),
		Type: app.VeyronRequestMessage,
	})
	if err != nil {
		t.Fatalf("Failed to marshall app message to json: %v", err)
	}

	err = browspr.HandleMessage(msgInstanceId, string(msg))
	if err != nil {
		t.Fatalf("Error while handling message: %v", err)
	}

	<-receivedResponse

	if receivedInstanceId != msgInstanceId {
		t.Errorf("Received unexpected instance id: %d. Expected: %d", receivedInstanceId, msgInstanceId)
	}
	if receivedType != "browsprMsg" {
		t.Errorf("Received unexpected response type. Expected: %q, but got %q", "browsprMsg", receivedType)
	}

	var outMsg app.Message
	if err := json.Unmarshal([]byte(receivedMsg), &outMsg); err != nil {
		t.Fatalf("Failed to unmarshall outgoing message: %v", err)
	}
	if outMsg.Id != int64(1) {
		t.Errorf("Id was %v, expected %v", outMsg.Id, int64(1))
	}
	if outMsg.Type != app.VeyronRequestMessage {
		t.Errorf("Message type was %v, expected %v", outMsg.Type, app.MessageType(0))
	}

	var responseMsg app.Response
	if err := json.Unmarshal([]byte(outMsg.Data), &responseMsg); err != nil {
		t.Fatalf("Failed to unmarshall outgoing response: %v", err)
	}
	if responseMsg.Type != lib.ResponseFinal {
		t.Errorf("Data was %q, expected %q", outMsg.Data, `["[InputValue]"]`)
	}
	var outArg string
	var ok bool
	if outArg, ok = responseMsg.Message.(string); !ok {
		t.Errorf("Got unexpected response message body of type %T, expected type string", responseMsg.Message)
	}
	var result []string
	arg, err := hex.DecodeString(outArg)
	if err != nil {
		t.Errorf("failed to hex decode string: %v", err)
	}
	decoder, err := vom2.NewDecoder(bytes.NewBuffer(arg))
	if err != nil {
		t.Fatalf("failed to construct new decoder: %v", err)
	}
	if err := decoder.Decode(&result); err != nil || result[0] != "[InputValue]" {
		t.Errorf("got %s with err: %v, expected %s", result[0], err, "[InputValue]")
	}
}
