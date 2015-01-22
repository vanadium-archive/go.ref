package browspr

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"v.io/core/veyron/profiles"
	"v.io/core/veyron/runtimes/google/ipc/stream/proxy"
	mounttable "v.io/core/veyron/services/mounttable/lib"
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/wspr/veyron/services/wsprd/app"
	"v.io/wspr/veyron/services/wsprd/lib"
)

func startProxy() (*proxy.Proxy, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	return proxy.New(rid, nil, "tcp", "127.0.0.1:0", "")
}

func startMounttable(ctx *context.T) (ipc.Server, naming.Endpoint, error) {
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		return nil, nil, err
	}

	s, err := veyron2.NewServer(ctx, options.ServesMountTable(true))
	if err != nil {
		return nil, nil, err
	}

	endpoints, err := s.Listen(profiles.LocalListenSpec)
	if err != nil {
		return nil, nil, err
	}

	if err := s.ServeDispatcher("", mt); err != nil {
		return nil, nil, err
	}

	return s, endpoints[0], nil
}

type mockServer struct{}

func (s mockServer) BasicCall(_ ipc.ServerCall, txt string) (string, error) {
	return "[" + txt + "]", nil
}

func startMockServer(ctx *context.T, desiredName string) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := veyron2.NewServer(ctx)
	if err != nil {
		return nil, nil, err
	}

	endpoints, err := s.Listen(profiles.LocalListenSpec)
	if err != nil {
		return nil, nil, err
	}

	if err := s.Serve(desiredName, mockServer{}, nil); err != nil {
		return nil, nil, err
	}

	return s, endpoints[0], nil
}

func TestBrowspr(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	proxy, err := startProxy()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Shutdown()

	mtServer, mtEndpoint, err := startMounttable(ctx)
	if err != nil {
		t.Fatalf("Failed to start mounttable server: %v", err)
	}
	defer mtServer.Stop()
	root := mtEndpoint.Name()
	if err := veyron2.GetNamespace(ctx).SetRoots(root); err != nil {
		t.Fatalf("Failed to set namespace roots: %v", err)
	}

	mockServerName := "mock/server"
	mockServer, mockServerEndpoint, err := startMockServer(ctx, mockServerName)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer mockServer.Stop()

	names, err := mockServer.Published()
	if err != nil {
		t.Fatalf("Error fetching published names: %v", err)
	}
	if len(names) != 1 || names[0] != naming.JoinAddressName(root, mockServerName) {
		t.Fatalf("Incorrectly mounted server. Names: %v", names)
	}
	mountEntry, err := veyron2.GetNamespace(ctx).Resolve(ctx, mockServerName)
	if err != nil {
		t.Fatalf("Error fetching published names from mounttable: %v", err)
	}

	servers := []string{}
	for _, s := range mountEntry.Servers {
		if strings.Index(s.Server, "@tcp") != -1 {
			servers = append(servers, s.Server)
		}
	}
	if len(servers) != 1 || servers[0] != mockServerEndpoint.String() {
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

	veyron2.GetNamespace(ctx).SetRoots(root)
	browspr := NewBrowspr(ctx, postMessageHandler, &spec, "/mock:1234/identd", []string{root})

	// browspr sets its namespace root to use the "ws" protocol, but we want to force "tcp" here.
	browspr.namespaceRoots = []string{root}

	browspr.accountManager.SetMockBlesser(newMockBlesserService(veyron2.GetPrincipal(ctx)))

	msgInstanceId := int32(11)
	msgOrigin := "http://test-origin.com"

	// Associate the origin with the root accounts' blessings, otherwise a
	// dummy account will be used and will be rejected by the authorizer.
	accountName := "test-account"
	bp := veyron2.GetPrincipal(browspr.ctx)
	if err := browspr.principalManager.AddAccount(accountName, bp.BlessingStore().Default()); err != nil {
		t.Fatalf("Failed to add account: %v")
	}
	if err := browspr.accountManager.AssociateAccount(msgOrigin, accountName, nil); err != nil {
		t.Fatalf("Failed to associate account: %v")
	}

	rpc := app.VeyronRPC{
		Name:        mockServerName,
		Method:      "BasicCall",
		InArgs:      []interface{}{"InputValue"},
		NumOutArgs:  1,
		IsStreaming: false,
		Timeout:     (1 << 31) - 1,
	}

	vomRPC, err := lib.VomEncode(rpc)
	if err != nil {
		t.Fatalf("Failed to vom encode rpc message: %v", err)
	}

	msg, err := json.Marshal(app.Message{
		Id:   1,
		Data: vomRPC,
		Type: app.VeyronRequestMessage,
	})
	if err != nil {
		t.Fatalf("Failed to marshall app message to json: %v", err)
	}

	err = browspr.HandleMessage(msgInstanceId, msgOrigin, string(msg))
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
	if err := lib.VomDecode(receivedMsg, &outMsg); err != nil {
		t.Fatalf("Failed to unmarshall outgoing message: %v", err)
	}
	if outMsg.Id != int32(1) {
		t.Errorf("Id was %v, expected %v", outMsg.Id, int32(1))
	}
	if outMsg.Type != app.VeyronRequestMessage {
		t.Errorf("Message type was %v, expected %v", outMsg.Type, app.MessageType(0))
	}

	var responseMsg lib.Response
	if err := lib.VomDecode(outMsg.Data, &responseMsg); err != nil {
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
	if err := lib.VomDecode(outArg, &result); err != nil {
		t.Errorf("Failed to vom decode args from %v: %v", outArg, err)
	}
	if got, want := result, []string{"[InputValue]"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Result got %v, want %v", got, want)
	}
}
