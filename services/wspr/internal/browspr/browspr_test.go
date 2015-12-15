// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package browspr

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	vdltime "v.io/v23/vdlroot/time"
	"v.io/v23/vom"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/services/wspr/internal/app"
	"v.io/x/ref/services/wspr/internal/lib"
	"v.io/x/ref/services/wspr/internal/principal"
	"v.io/x/ref/services/xproxy/xproxy"
	"v.io/x/ref/test"
)

type mockServer struct{}

func (s mockServer) BasicCall(_ *context.T, _ rpc.StreamServerCall, txt string) (string, error) {
	return "[" + txt + "]", nil
}

func parseBrowsperResponse(data string, t *testing.T) (uint64, uint64, []byte) {
	receivedBytes, err := hex.DecodeString(data)
	if err != nil {
		t.Fatalf("Failed to hex decode outgoing message: %v", err)
	}

	id, bytesRead, err := lib.BinaryDecodeUint(receivedBytes)
	if err != nil {
		t.Fatalf("Failed to read mesage id: %v", err)
	}

	receivedBytes = receivedBytes[bytesRead:]
	messageType, bytesRead, err := lib.BinaryDecodeUint(receivedBytes)
	if err != nil {
		t.Fatalf("Failed to read message type: %v", err)
	}
	receivedBytes = receivedBytes[bytesRead:]
	return id, messageType, receivedBytes
}

func TestBrowspr(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	proxySpec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}},
	}
	ctx = v23.WithListenSpec(ctx, proxySpec)
	proxy, err := xproxy.New(ctx, "", security.AllowEveryone())
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	proxyEndpoint := proxy.ListeningEndpoints()[0]

	mt, err := mounttablelib.NewMountTableDispatcher(ctx, "", "", "mounttable")
	if err != nil {
		t.Fatalf("Failed to create mounttable: %v", err)
	}
	ctx, s, err := v23.WithNewDispatchingServer(ctx, "", mt, options.ServesMountTable(true))
	if err != nil {
		t.Fatalf("Failed to start mounttable server: %v", err)
	}
	mtEndpoint := s.Status().Endpoints[0]
	root := mtEndpoint.Name()

	if err := v23.GetNamespace(ctx).SetRoots(root); err != nil {
		t.Fatalf("Failed to set namespace roots: %v", err)
	}

	mockServerName := "mock/server"
	ctx, mockServer, err := v23.WithNewServer(ctx, mockServerName, mockServer{}, nil)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	mockServerEndpoint := mockServer.Status().Endpoints[0]

	then := time.Now()
found:
	for {
		status := mockServer.Status()
		for _, v := range status.Mounts {
			if v.Name == mockServerName && v.Server == mockServerEndpoint.String() && !v.LastMount.IsZero() {
				if v.LastMountErr != nil {
					t.Fatalf("Failed to mount %s: %v", v.Name, v.LastMountErr)
				}
				break found
			}
		}
		if time.Now().Sub(then) > time.Minute {
			t.Fatalf("Failed to find mounted server and endpoint: %v: %v", mockServerName, mtEndpoint)
		}
		time.Sleep(100 * time.Millisecond)
	}
	mountEntry, err := v23.GetNamespace(ctx).Resolve(ctx, mockServerName)
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

	spec := v23.GetListenSpec(ctx)
	spec.Addrs = nil
	spec.Proxy = proxyEndpoint.Name()

	receivedResponse := make(chan bool, 1)
	var receivedInstanceId int32
	var receivedType string
	var messageId uint64
	var messageType uint64
	var receivedBytes []byte
	typeReader := lib.NewTypeReader()

	var postMessageHandler = func(instanceId int32, ty, msg string) {
		id, mType, bin := parseBrowsperResponse(msg, t)
		if mType == lib.ResponseTypeMessage {
			var decodedBytes []byte
			if err := vom.Decode(bin, &decodedBytes); err != nil {
				t.Fatalf("Failed to decode type bytes: %v", err)
			}
			typeReader.Add(hex.EncodeToString(decodedBytes))
			return
		}
		receivedInstanceId = instanceId
		receivedType = ty
		messageType = mType
		messageId = id
		receivedBytes = bin
		receivedResponse <- true
	}

	v23.GetNamespace(ctx).SetRoots(root)
	browspr := NewBrowspr(ctx, postMessageHandler, &spec, "/mock:1234/identd", []string{root}, principal.NewInMemorySerializer())

	// browspr sets its namespace root to use the "ws" protocol, but we want to force "tcp" here.
	browspr.namespaceRoots = []string{root}

	browspr.accountManager.SetMockBlesser(newMockBlesserService(v23.GetPrincipal(ctx)))

	msgInstanceId := int32(11)
	msgOrigin := "http://test-origin.com"

	// Associate the origin with the root accounts' blessings, otherwise a
	// dummy account will be used and will be rejected by the authorizer.
	accountName := "test-account"
	bp := v23.GetPrincipal(browspr.ctx)
	if err := browspr.principalManager.AddAccount(accountName, bp.BlessingStore().Default()); err != nil {
		t.Fatalf("Failed to add account: %v", err)
	}
	if err := browspr.accountManager.AssociateAccount(ctx, msgOrigin, accountName, nil); err != nil {
		t.Fatalf("Failed to associate account: %v", err)
	}

	rpc := app.RpcRequest{
		Name:        mockServerName,
		Method:      "BasicCall",
		NumInArgs:   1,
		NumOutArgs:  1,
		IsStreaming: false,
		Deadline:    vdltime.Deadline{},
	}

	var typeBuf bytes.Buffer
	typeEncoder := vom.NewTypeEncoder(&typeBuf)
	var buf bytes.Buffer
	encoder := vom.NewEncoderWithTypeEncoder(&buf, typeEncoder)
	if err := encoder.Encode(rpc); err != nil {
		t.Fatalf("Failed to vom encode rpc message: %v", err)
	}
	if err := encoder.Encode("InputValue"); err != nil {
		t.Fatalf("Failed to vom encode rpc message: %v", err)
	}

	vomRPC := hex.EncodeToString(buf.Bytes())

	msg := app.Message{
		Id:   1,
		Data: vomRPC,
		Type: app.VeyronRequestMessage,
	}

	typeMessage := app.Message{
		Id:   0,
		Data: hex.EncodeToString(typeBuf.Bytes()),
		Type: app.TypeMessage,
	}
	createInstanceMessage := CreateInstanceMessage{
		InstanceId:     msgInstanceId,
		Origin:         msgOrigin,
		NamespaceRoots: nil,
		Proxy:          "",
	}
	_, err = browspr.HandleCreateInstanceRpc(vdl.ValueOf(createInstanceMessage))

	err = browspr.HandleMessage(msgInstanceId, msgOrigin, typeMessage)
	if err != nil {
		t.Fatalf("Error while handling type message: %v", err)
	}

	err = browspr.HandleMessage(msgInstanceId, msgOrigin, msg)
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

	if messageId != 1 {
		t.Errorf("Id was %v, expected %v", messageId, int32(1))
	}

	if lib.ResponseType(messageType) != lib.ResponseFinal {
		t.Errorf("Message type was %v, expected %v", messageType, lib.ResponseFinal)
	}

	var data string
	if err := vom.NewDecoder(bytes.NewBuffer(receivedBytes)).Decode(&data); err != nil {
		t.Fatalf("Failed to unmarshall outgoing response: %v", err)
	}

	var result app.RpcResponse
	dataBytes, err := hex.DecodeString(data)
	if err != nil {
		t.Errorf("Failed to hex decode from %v: %v", data, err)
	}
	td := vom.NewTypeDecoder(typeReader)
	td.Start()
	defer td.Stop()
	decoder := vom.NewDecoderWithTypeDecoder(bytes.NewBuffer(dataBytes), td)
	if err := decoder.Decode(&result); err != nil {
		t.Errorf("Failed to vom decode args from %v: %v", data, err)
	}
	if got, want := result.OutArgs[0], vdl.StringValue("[InputValue]"); !vdl.EqualValue(got, want) {
		t.Errorf("Result got %v, want %v", got, want)
	}
}
