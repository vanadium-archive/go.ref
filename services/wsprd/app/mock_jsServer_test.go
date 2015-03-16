package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/vom"
	"v.io/x/ref/services/wsprd/ipc/server"
	"v.io/x/ref/services/wsprd/lib"
	"v.io/x/ref/services/wsprd/principal"
)

type mockJSServer struct {
	controller           *Controller
	t                    *testing.T
	method               string
	serviceSignature     []signature.Interface
	sender               sync.WaitGroup
	expectedClientStream []string
	serverStream         []interface{}
	hasAuthorizer        bool
	authError            error
	inArgs               []interface{}
	controllerReady      sync.RWMutex
	finalResponse        *vdl.Value
	receivedResponse     *vdl.Value
	finalError           error
	hasCalledAuth        bool
	// Right now we keep track of the flow count by hand, but maybe we
	// should setup a different object to handle each flow, so we
	// can make sure that both sides are using the same flowId.  This
	// isn't a problem right now because the test doesn't do multiple flows
	// at the same time.
	flowCount int32
	rpcFlow   int32
}

func (m *mockJSServer) Send(responseType lib.ResponseType, msg interface{}) error {
	switch responseType {
	case lib.ResponseDispatcherLookup:
		return m.handleDispatcherLookup(msg)
	case lib.ResponseAuthRequest:
		return m.handleAuthRequest(msg)
	case lib.ResponseServerRequest:
		return m.handleServerRequest(msg)
	case lib.ResponseValidate:
		return m.handleValidationRequest(msg)
	case lib.ResponseStream:
		return m.handleStream(msg)
	case lib.ResponseStreamClose:
		return m.handleStreamClose(msg)
	case lib.ResponseFinal:
		if m.receivedResponse != nil {
			return fmt.Errorf("Two responses received. First was: %#v. Second was: %#v", m.receivedResponse, msg)
		}
		m.receivedResponse = vdl.ValueOf(msg)
		return nil
	}
	return fmt.Errorf("Unknown message type: %d", responseType)
}

func internalErrJSON(args interface{}) string {
	return fmt.Sprintf(`{"err": {
			"idAction": {
				"id": "v.io/v23/verror.Internal",
				"action": 0
			},
			"paramList": ["%v"]}, "results":[null]}`, args)
}

func (m *mockJSServer) Error(err error) {
	panic(err)
}

func normalize(msg interface{}) (map[string]interface{}, error) {
	// We serialize and deserialize the reponse so that we can do deep equal with
	// messages that contain non-exported structs.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, err
	}

	var r interface{}

	if err := json.NewDecoder(&buf).Decode(&r); err != nil {
		return nil, err
	}
	return r.(map[string]interface{}), nil
}

func (m *mockJSServer) handleDispatcherLookup(v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()
	m.controllerReady.RLock()
	defer m.controllerReady.RUnlock()
	msg, err := normalize(v)
	if err != nil {
		m.controller.HandleLookupResponse(m.flowCount, internalErrJSON(err))
		return nil
	}
	expected := map[string]interface{}{"serverId": 0.0, "suffix": "adder"}
	if !reflect.DeepEqual(msg, expected) {
		m.controller.HandleLookupResponse(m.flowCount, internalErrJSON(fmt.Sprintf("got: %v, want: %v", msg, expected)))
		return nil
	}
	bytes, err := json.Marshal(map[string]interface{}{
		"handle":        0,
		"signature":     lib.VomEncodeOrDie(m.serviceSignature),
		"hasAuthorizer": m.hasAuthorizer,
	})
	if err != nil {
		m.controller.HandleLookupResponse(m.flowCount, internalErrJSON(fmt.Sprintf("failed to serialize %v", err)))
		return nil
	}
	m.controller.HandleLookupResponse(m.flowCount, string(bytes))
	return nil
}

// Returns false if the blessing is malformed
func validateBlessing(blessings principal.BlessingsHandle) bool {
	return blessings.Handle != 0 && blessings.PublicKey != ""
}

func validateEndpoint(ep string) bool {
	return ep != ""
}

func (m *mockJSServer) handleAuthRequest(v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()

	m.hasCalledAuth = true
	if !m.hasAuthorizer {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON("unexpected auth request"))
		return nil
	}

	var msg server.AuthRequest
	if err := lib.VomDecode(v.(string), &msg); err != nil {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("error decoding %v:", err)))
		return nil
	}

	if msg.Handle != 0 {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected handled: %f", msg.Handle)))
		return nil
	}

	call := msg.Call
	if field, got, want := "Method", call.Method, lib.LowercaseFirstCharacter(m.method); got != want {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want)))
		return nil
	}

	if field, got, want := "Suffix", call.Suffix, "adder"; got != want {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want)))
		return nil
	}

	// We expect localBlessings and remoteBlessings to be set and the publicKey be a string
	if !validateBlessing(call.LocalBlessings) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad localblessing:%v", call.LocalBlessings)))
		return nil
	}
	if !validateBlessing(call.RemoteBlessings) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad remoteblessing:%v", call.RemoteBlessings)))
		return nil
	}

	// We expect endpoints to be set
	if !validateEndpoint(call.LocalEndpoint) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad endpoint:%v", call.LocalEndpoint)))
		return nil
	}

	if !validateEndpoint(call.RemoteEndpoint) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad endpoint:%v", call.RemoteEndpoint)))
		return nil
	}

	bytes, err := json.Marshal(map[string]interface{}{
		"err": m.authError,
	})
	if err != nil {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("failed to serialize %v", err)))
		return nil
	}

	m.controller.HandleAuthResponse(m.flowCount, string(bytes))
	return nil
}

func (m *mockJSServer) handleServerRequest(v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()

	if m.hasCalledAuth != m.hasAuthorizer {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON("authorizer hasn't been called yet"))
		return nil
	}

	var msg server.ServerRPCRequest
	if err := lib.VomDecode(v.(string), &msg); err != nil {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(err))
		return nil
	}

	if field, got, want := "Method", msg.Method, lib.LowercaseFirstCharacter(m.method); got != want {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want)))
		return nil
	}

	if field, got, want := "Handle", msg.Handle, int32(0); got != want {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want)))
		return nil
	}

	if field, got, want := "Args", msg.Args, m.inArgs; !reflect.DeepEqual(got, want) {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want)))
		return nil
	}

	call := msg.Call.SecurityCall
	if field, got, want := "Suffix", call.Suffix, "adder"; got != want {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want)))
		return nil
	}

	if !validateBlessing(call.RemoteBlessings) {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad Remoteblessing:%v", call.RemoteBlessings)))
		return nil
	}

	m.rpcFlow = m.flowCount

	// We don't return the final response until the stream is closed.
	m.sender.Add(1)
	go m.sendServerStream()
	return nil
}

func (m *mockJSServer) handleValidationRequest(v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()

	req := v.(server.CaveatValidationRequest)
	resp := server.CaveatValidationResponse{
		Results: make([]error, len(req.Cavs)),
	}

	var b bytes.Buffer
	enc, err := vom.NewEncoder(&b)
	if err != nil {
		panic(err)
	}
	if err := enc.Encode(resp); err != nil {
		panic(err)
	}

	m.controllerReady.RLock()
	m.controller.HandleCaveatValidationResponse(m.flowCount, fmt.Sprintf("%x", b.Bytes()))
	m.controllerReady.RUnlock()
	return nil
}

func (m *mockJSServer) sendServerStream() {
	defer m.sender.Done()
	m.controllerReady.RLock()
	for _, v := range m.serverStream {
		m.controller.SendOnStream(m.rpcFlow, lib.VomEncodeOrDie(v), m)
	}
	m.controllerReady.RUnlock()
}

func (m *mockJSServer) handleStream(msg interface{}) error {
	smsg, ok := msg.(string)
	if !ok || len(m.expectedClientStream) == 0 {
		m.t.Errorf("unexpected stream message: %v", msg)
		return nil
	}

	curr := m.expectedClientStream[0]
	m.expectedClientStream = m.expectedClientStream[1:]
	if smsg != curr {
		m.t.Errorf("unexpected stream message, got %s, want: %s", smsg, curr)
	}
	return nil
}

func (m *mockJSServer) handleStreamClose(msg interface{}) error {
	m.sender.Wait()
	reply := lib.ServerRpcReply{
		Results: []*vdl.Value{m.finalResponse},
		Err:     m.finalError,
	}
	vomReply, err := lib.VomEncode(reply)
	if err != nil {
		m.t.Fatalf("Failed to serialize the reply: %v", err)
	}
	m.controllerReady.RLock()
	m.controller.HandleServerResponse(m.rpcFlow, vomReply)
	m.controllerReady.RUnlock()
	return nil
}
