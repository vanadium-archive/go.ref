// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"v.io/v23/context"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/internal/reflectutil"
	"v.io/x/ref/services/wspr/internal/lib"
	"v.io/x/ref/services/wspr/internal/rpc/server"
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
	finalResponse        *vom.RawBytes
	receivedResponse     *vom.RawBytes
	finalError           error
	hasCalledAuth        bool
	// Right now we keep track of the flow count by hand, but maybe we
	// should setup a different object to handle each flow, so we
	// can make sure that both sides are using the same flowId.  This
	// isn't a problem right now because the test doesn't do multiple flows
	// at the same time.
	flowCount int32
	rpcFlow   int32

	typeReader  *lib.TypeReader
	typeDecoder *vom.TypeDecoder

	typeEncoder *vom.TypeEncoder

	ctx *context.T
}

func (m *mockJSServer) Send(responseType lib.ResponseType, msg interface{}) error {
	switch responseType {
	case lib.ResponseDispatcherLookup:
		return m.handleDispatcherLookup(m.ctx, msg)
	case lib.ResponseAuthRequest:
		return m.handleAuthRequest(m.ctx, msg)
	case lib.ResponseServerRequest:
		return m.handleServerRequest(m.ctx, msg)
	case lib.ResponseValidate:
		return m.handleValidationRequest(m.ctx, msg)
	case lib.ResponseStream:
		return m.handleStream(m.ctx, msg)
	case lib.ResponseStreamClose:
		return m.handleStreamClose(m.ctx, msg)
	case lib.ResponseFinal:
		if m.receivedResponse != nil {
			return fmt.Errorf("Two responses received. First was: %#v. Second was: %#v", m.receivedResponse, msg)
		}
		m.receivedResponse = vom.RawBytesOf(msg)
		return nil
	case lib.ResponseLog, lib.ResponseBlessingsCacheMessage:
		m.flowCount += 2
		return nil
	case lib.ResponseTypeMessage:
		m.handleTypeMessage(msg)
		return nil
	}
	return fmt.Errorf("Unknown message type: %d", responseType)
}

func internalErr(args interface{}, typeEncoder *vom.TypeEncoder) string {
	err := verror.E{
		ID:        verror.ID("v.io/v23/verror.Internal"),
		Action:    verror.ActionCode(0),
		ParamList: []interface{}{args},
	}

	return lib.HexVomEncodeOrDie(server.LookupReply{
		Err: err,
	}, typeEncoder)
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

func (m *mockJSServer) handleTypeMessage(v interface{}) {
	m.typeReader.Add(hex.EncodeToString(v.([]byte)))
}
func (m *mockJSServer) handleDispatcherLookup(ctx *context.T, v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()
	m.controllerReady.RLock()
	defer m.controllerReady.RUnlock()

	msg, err := normalize(v)
	if err != nil {
		m.controller.HandleLookupResponse(ctx, m.flowCount, internalErr(err, m.typeEncoder))
		return nil
	}
	expected := map[string]interface{}{"serverId": 0.0, "suffix": "adder"}
	if !reflect.DeepEqual(msg, expected) {
		m.controller.HandleLookupResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("got: %v, want: %v", msg, expected), m.typeEncoder))
		return nil
	}
	lookupReply := lib.HexVomEncodeOrDie(server.LookupReply{
		Handle:        0,
		Signature:     m.serviceSignature,
		HasAuthorizer: m.hasAuthorizer,
	}, m.typeEncoder)
	m.controller.HandleLookupResponse(ctx, m.flowCount, lookupReply)
	return nil
}

func validateEndpoint(ep string) bool {
	return ep != ""
}

func (m *mockJSServer) handleAuthRequest(ctx *context.T, v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()

	m.hasCalledAuth = true
	if !m.hasAuthorizer {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr("unexpected auth request", m.typeEncoder))
		return nil
	}

	var msg server.AuthRequest
	if err := lib.HexVomDecode(v.(string), &msg, m.typeDecoder); err != nil {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("error decoding %v:", err), m.typeEncoder))
		return nil
	}

	if msg.Handle != 0 {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("unexpected handled: %v", msg.Handle), m.typeEncoder))
		return nil
	}

	call := msg.Call
	if field, got, want := "Method", call.Method, lib.LowercaseFirstCharacter(m.method); got != want {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want), m.typeEncoder))
		return nil
	}

	if field, got, want := "Suffix", call.Suffix, "adder"; got != want {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want), m.typeEncoder))
		return nil
	}

	// We expect localBlessings and remoteBlessings to be a non-zero id
	if call.LocalBlessings == 0 {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("bad local blessing: %v", call.LocalBlessings), m.typeEncoder))
		return nil
	}
	if call.RemoteBlessings == 0 {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("bad remote blessing: %v", call.RemoteBlessings), m.typeEncoder))
		return nil
	}

	// We expect endpoints to be set
	if !validateEndpoint(call.LocalEndpoint) {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("bad endpoint:%v", call.LocalEndpoint), m.typeEncoder))
		return nil
	}

	if !validateEndpoint(call.RemoteEndpoint) {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("bad endpoint:%v", call.RemoteEndpoint), m.typeEncoder))
		return nil
	}

	authReply := lib.HexVomEncodeOrDie(server.AuthReply{
		Err: m.authError,
	}, m.typeEncoder)

	m.controller.HandleAuthResponse(ctx, m.flowCount, authReply)
	return nil
}

func (m *mockJSServer) handleServerRequest(ctx *context.T, v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()

	if m.hasCalledAuth != m.hasAuthorizer {
		m.controller.HandleServerResponse(ctx, m.flowCount, internalErr("authorizer hasn't been called yet", m.typeEncoder))
		return nil
	}

	var msg server.ServerRpcRequest
	if err := lib.HexVomDecode(v.(string), &msg, m.typeDecoder); err != nil {
		m.controller.HandleServerResponse(ctx, m.flowCount, internalErr(err, m.typeEncoder))
		return nil
	}

	if field, got, want := "Method", msg.Method, lib.LowercaseFirstCharacter(m.method); got != want {
		m.controller.HandleServerResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want), m.typeEncoder))
		return nil
	}

	if field, got, want := "Handle", msg.Handle, int32(0); got != want {
		m.controller.HandleServerResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want), m.typeEncoder))
		return nil
	}

	vals := make([]interface{}, len(msg.Args))
	for i, vArg := range msg.Args {
		if err := vArg.ToValue(&vals[i]); err != nil {
			panic(err)
		}
	}
	if field, got, want := "Args", vals, m.inArgs; !reflectutil.DeepEqual(got, want, &reflectutil.DeepEqualOpts{SliceEqNilEmpty: true}) {
		m.controller.HandleServerResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want), m.typeEncoder))
		return nil
	}

	call := msg.Call.SecurityCall
	if field, got, want := "Suffix", call.Suffix, "adder"; got != want {
		m.controller.HandleServerResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("unexpected value for %s: got %v, want %v", field, got, want), m.typeEncoder))
		return nil
	}

	// We expect localBlessings and remoteBlessings to be a non-zero id
	if call.LocalBlessings == 0 {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("bad local blessing: %v", call.LocalBlessings), m.typeEncoder))
		return nil
	}
	if call.RemoteBlessings == 0 {
		m.controller.HandleAuthResponse(ctx, m.flowCount, internalErr(fmt.Sprintf("bad remote blessing: %v", call.RemoteBlessings), m.typeEncoder))
		return nil
	}

	m.rpcFlow = m.flowCount

	// We don't return the final response until the stream is closed.
	m.sender.Add(1)
	go m.sendServerStream(ctx)
	return nil
}

func (m *mockJSServer) handleValidationRequest(ctx *context.T, v interface{}) error {
	defer func() {
		m.flowCount += 2
	}()

	req := v.(server.CaveatValidationRequest)
	resp := server.CaveatValidationResponse{
		Results: make([]error, len(req.Cavs)),
	}

	res := lib.HexVomEncodeOrDie(resp, m.typeEncoder)

	m.controllerReady.RLock()
	m.controller.HandleCaveatValidationResponse(ctx, m.flowCount, res)
	m.controllerReady.RUnlock()
	return nil
}

func (m *mockJSServer) sendServerStream(ctx *context.T) {
	defer m.sender.Done()
	m.controllerReady.RLock()
	for _, v := range m.serverStream {
		m.controller.SendOnStream(ctx, m.rpcFlow, lib.HexVomEncodeOrDie(v, m.typeEncoder), m)
	}
	m.controllerReady.RUnlock()
}

func (m *mockJSServer) handleStream(ctx *context.T, msg interface{}) error {
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

func (m *mockJSServer) handleStreamClose(ctx *context.T, msg interface{}) error {
	m.sender.Wait()
	reply := lib.ServerRpcReply{
		Results: []*vom.RawBytes{m.finalResponse},
		Err:     m.finalError,
	}

	m.controllerReady.RLock()
	m.controller.HandleServerResponse(ctx, m.rpcFlow, lib.HexVomEncodeOrDie(reply, m.typeEncoder))
	m.controllerReady.RUnlock()
	return nil
}
