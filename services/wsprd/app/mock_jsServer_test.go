package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/signature"
)

type mockJSServer struct {
	controller           *Controller
	t                    *testing.T
	method               string
	serviceSignature     signature.JSONServiceSignature
	sender               sync.WaitGroup
	expectedClientStream []string
	serverStream         []string
	hasAuthorizer        bool
	authError            error
	inArgs               string
	finalResponse        interface{}
	finalError           error
	hasCalledAuth        bool
	// Right now we keep track of the flow count by hand, but maybe we
	// should setup a different object to handle each flow, so we
	// can make sure that both sides are using the same flowId.  This
	// isn't a problem right now because the test doesn't do multiple flows
	// at the same time.
	flowCount int64
	rpcFlow   int64
}

func (m *mockJSServer) Send(responseType lib.ResponseType, msg interface{}) error {
	switch responseType {
	case lib.ResponseDispatcherLookup:
		return m.handleDispatcherLookup(msg)
	case lib.ResponseAuthRequest:
		return m.handleAuthRequest(msg)
	case lib.ResponseServerRequest:
		return m.handleServerRequest(msg)
	case lib.ResponseStream:
		return m.handleStream(msg)
	case lib.ResponseStreamClose:
		return m.handleStreamClose(msg)

	}
	return fmt.Errorf("Unknown message type: %d", responseType)
}

func internalErrJSON(args interface{}) string {
	return fmt.Sprintf(`{"err": {
			"idAction": {
				"id": "veyron.io/veyron/veyron2/verror.Internal",
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
		"signature":     m.serviceSignature,
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
func validateBlessing(v interface{}) bool {
	blessings, ok := v.(map[string]interface{})
	return ok && blessings["Handle"] != nil && blessings["PublicKey"] != nil
}

func validateEndpoint(v interface{}) bool {
	if v == nil {
		return false
	}
	ep, ok := v.(string)
	return ok && ep != ""
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

	msg, err := normalize(v)
	if err != nil {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(err))
		return nil

	}
	if msg["handle"] != 0.0 {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected handled: %f", msg["handle"])))
		return nil
	}

	// Do an exact match on keys in this map for the context structure.
	// For keys not in this map, we can't do an exact equality check and
	// we'll verify them later.
	expectedContextValues := map[string]interface{}{
		"method": lib.LowercaseFirstCharacter(m.method),
		"name":   "adder",
		"suffix": "adder",
	}

	context := msg["context"].(map[string]interface{})
	for key, value := range expectedContextValues {
		if !reflect.DeepEqual(context[key], value) {
			m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", key, context[key], value)))
			return nil

		}
	}
	// We expect localBlessings and remoteBlessings to be set and the publicKey be a string
	if !validateBlessing(context["localBlessings"]) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad localblessing:%v", context["localBlessing"])))
		return nil
	}
	if !validateBlessing(context["remoteBlessings"]) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad remoteblessing:%v", context["remoteBlessings"])))
		return nil
	}

	// We expect endpoints to be set
	if !validateEndpoint(context["localEndpoint"]) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad endpoint:%v", context["localEndpoint"])))
		return nil

	}

	if !validateEndpoint(context["remoteEndpoint"]) {
		m.controller.HandleAuthResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad endpoint:%v", context["remoteEndpoint"])))
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

	msg, err := normalize(v)
	if err != nil {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(err))
		return nil

	}

	// Do an exact match on keys in this map for the request structure.
	// For keys not in this map, we can't do an exact equality check and
	// we'll verify them later.
	expectedRequestValues := map[string]interface{}{
		"Method": lib.LowercaseFirstCharacter(m.method),
		"Handle": 0.0,
		"Args":   m.inArgs,
	}

	for key, value := range expectedRequestValues {
		if !reflect.DeepEqual(msg[key], value) {
			m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", key, msg[key], value)))
			return nil

		}
	}

	context := msg["Context"].(map[string]interface{})
	expectedContextValues := map[string]interface{}{
		"Name":   "adder",
		"Suffix": "adder",
	}

	for key, value := range expectedContextValues {
		if !reflect.DeepEqual(context[key], value) {
			m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("unexpected value for %s: got %v, want %v", key, context[key], value)))
			return nil

		}
	}

	if !validateBlessing(context["RemoteBlessings"]) {
		m.controller.HandleServerResponse(m.flowCount, internalErrJSON(fmt.Sprintf("bad Remoteblessing:%v", context["RemoteBlessings"])))
		return nil
	}

	m.rpcFlow = m.flowCount

	// We don't return the final response until the stream is closed.
	m.sender.Add(1)
	go m.sendServerStream()
	return nil
}

func (m *mockJSServer) sendServerStream() {
	defer m.sender.Done()
	for _, msg := range m.serverStream {
		m.controller.SendOnStream(m.rpcFlow, msg, m)
	}
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
	serverReply := map[string]interface{}{
		"Results": []interface{}{m.finalResponse},
		"Err":     m.finalError,
	}

	bytes, err := json.Marshal(serverReply)
	if err != nil {
		m.t.Fatalf("Failed to serialize the reply: %v", err)
	}
	m.controller.HandleServerResponse(m.rpcFlow, string(bytes))
	return nil
}
