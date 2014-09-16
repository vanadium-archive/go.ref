// This file was auto-generated by the veyron vdl tool.
// Source: wire.vdl

package main

import (
	// The non-user imports are prefixed with "_gen_" to prevent collisions.
	_gen_veyron2 "veyron2"
	_gen_context "veyron2/context"
	_gen_ipc "veyron2/ipc"
	_gen_naming "veyron2/naming"
	_gen_vdlutil "veyron2/vdl/vdlutil"
	_gen_wiretype "veyron2/wiretype"
)

// TODO(bprosnitz) Remove this line once signatures are updated to use typevals.
// It corrects a bug where _gen_wiretype is unused in VDL pacakges where only bootstrap types are used on interfaces.
const _ = _gen_wiretype.TypeIDInvalid

// Simple service used in the agent tests.
// PingPong is the interface the client binds and uses.
// PingPong_ExcludingUniversal is the interface without internal framework-added methods
// to enable embedding without method collisions.  Not to be used directly by clients.
type PingPong_ExcludingUniversal interface {
	Ping(ctx _gen_context.T, message string, opts ..._gen_ipc.CallOpt) (reply string, err error)
}
type PingPong interface {
	_gen_ipc.UniversalServiceMethods
	PingPong_ExcludingUniversal
}

// PingPongService is the interface the server implements.
type PingPongService interface {
	Ping(context _gen_ipc.ServerContext, message string) (reply string, err error)
}

// BindPingPong returns the client stub implementing the PingPong
// interface.
//
// If no _gen_ipc.Client is specified, the default _gen_ipc.Client in the
// global Runtime is used.
func BindPingPong(name string, opts ..._gen_ipc.BindOpt) (PingPong, error) {
	var client _gen_ipc.Client
	switch len(opts) {
	case 0:
		// Do nothing.
	case 1:
		if clientOpt, ok := opts[0].(_gen_ipc.Client); opts[0] == nil || ok {
			client = clientOpt
		} else {
			return nil, _gen_vdlutil.ErrUnrecognizedOption
		}
	default:
		return nil, _gen_vdlutil.ErrTooManyOptionsToBind
	}
	stub := &clientStubPingPong{defaultClient: client, name: name}

	return stub, nil
}

// NewServerPingPong creates a new server stub.
//
// It takes a regular server implementing the PingPongService
// interface, and returns a new server stub.
func NewServerPingPong(server PingPongService) interface{} {
	return &ServerStubPingPong{
		service: server,
	}
}

// clientStubPingPong implements PingPong.
type clientStubPingPong struct {
	defaultClient _gen_ipc.Client
	name          string
}

func (__gen_c *clientStubPingPong) client(ctx _gen_context.T) _gen_ipc.Client {
	if __gen_c.defaultClient != nil {
		return __gen_c.defaultClient
	}
	return _gen_veyron2.RuntimeFromContext(ctx).Client()
}

func (__gen_c *clientStubPingPong) Ping(ctx _gen_context.T, message string, opts ..._gen_ipc.CallOpt) (reply string, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "Ping", []interface{}{message}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubPingPong) UnresolveStep(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply []string, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "UnresolveStep", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubPingPong) Signature(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply _gen_ipc.ServiceSignature, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubPingPong) GetMethodTags(ctx _gen_context.T, method string, opts ..._gen_ipc.CallOpt) (reply []interface{}, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "GetMethodTags", []interface{}{method}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

// ServerStubPingPong wraps a server that implements
// PingPongService and provides an object that satisfies
// the requirements of veyron2/ipc.ReflectInvoker.
type ServerStubPingPong struct {
	service PingPongService
}

func (__gen_s *ServerStubPingPong) GetMethodTags(call _gen_ipc.ServerCall, method string) ([]interface{}, error) {
	// TODO(bprosnitz) GetMethodTags() will be replaces with Signature().
	// Note: This exhibits some weird behavior like returning a nil error if the method isn't found.
	// This will change when it is replaced with Signature().
	switch method {
	case "Ping":
		return []interface{}{}, nil
	default:
		return nil, nil
	}
}

func (__gen_s *ServerStubPingPong) Signature(call _gen_ipc.ServerCall) (_gen_ipc.ServiceSignature, error) {
	result := _gen_ipc.ServiceSignature{Methods: make(map[string]_gen_ipc.MethodSignature)}
	result.Methods["Ping"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{
			{Name: "message", Type: 3},
		},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 3},
			{Name: "", Type: 65},
		},
	}

	result.TypeDefs = []_gen_vdlutil.Any{
		_gen_wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

func (__gen_s *ServerStubPingPong) UnresolveStep(call _gen_ipc.ServerCall) (reply []string, err error) {
	if unresolver, ok := __gen_s.service.(_gen_ipc.Unresolver); ok {
		return unresolver.UnresolveStep(call)
	}
	if call.Server() == nil {
		return
	}
	var published []string
	if published, err = call.Server().Published(); err != nil || published == nil {
		return
	}
	reply = make([]string, len(published))
	for i, p := range published {
		reply[i] = _gen_naming.Join(p, call.Name())
	}
	return
}

func (__gen_s *ServerStubPingPong) Ping(call _gen_ipc.ServerCall, message string) (reply string, err error) {
	reply, err = __gen_s.service.Ping(call, message)
	return
}
