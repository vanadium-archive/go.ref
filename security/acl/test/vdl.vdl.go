// This file was auto-generated by the veyron vdl tool.
// Source: vdl.vdl

// Package test provides a VDL specification for a service used in the unittest of the acl package.
package test

import (
	// The non-user imports are prefixed with "_gen_" to prevent collisions.
	_gen_veyron2 "veyron.io/veyron/veyron2"
	_gen_context "veyron.io/veyron/veyron2/context"
	_gen_ipc "veyron.io/veyron/veyron2/ipc"
	_gen_naming "veyron.io/veyron/veyron2/naming"
	_gen_vdlutil "veyron.io/veyron/veyron2/vdl/vdlutil"
	_gen_wiretype "veyron.io/veyron/veyron2/wiretype"
)

// Any package can define tags (of arbitrary types) to be attached to methods.
// This type can be used to index into a TaggedACLMap.
type MyTag string

// For this example/unittest, there are three possible values of MyTag,
// each represented by a single-character string.
const Read = MyTag("R")

const Write = MyTag("W")

const Execute = MyTag("X")

// TODO(toddw): Remove this line once the new signature support is done.
// It corrects a bug where _gen_wiretype is unused in VDL pacakges where only
// bootstrap types are used on interfaces.
const _ = _gen_wiretype.TypeIDInvalid

// MyObject demonstrates how tags are attached to methods.
// MyObject is the interface the client binds and uses.
// MyObject_ExcludingUniversal is the interface without internal framework-added methods
// to enable embedding without method collisions.  Not to be used directly by clients.
type MyObject_ExcludingUniversal interface {
	Get(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error)
	Put(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error)
	Resolve(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error)
	NoTags(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error) // No tags attached to this.
	AllTags(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error)
}
type MyObject interface {
	_gen_ipc.UniversalServiceMethods
	MyObject_ExcludingUniversal
}

// MyObjectService is the interface the server implements.
type MyObjectService interface {
	Get(context _gen_ipc.ServerContext) (err error)
	Put(context _gen_ipc.ServerContext) (err error)
	Resolve(context _gen_ipc.ServerContext) (err error)
	NoTags(context _gen_ipc.ServerContext) (err error) // No tags attached to this.
	AllTags(context _gen_ipc.ServerContext) (err error)
}

// BindMyObject returns the client stub implementing the MyObject
// interface.
//
// If no _gen_ipc.Client is specified, the default _gen_ipc.Client in the
// global Runtime is used.
func BindMyObject(name string, opts ..._gen_ipc.BindOpt) (MyObject, error) {
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
	stub := &clientStubMyObject{defaultClient: client, name: name}

	return stub, nil
}

// NewServerMyObject creates a new server stub.
//
// It takes a regular server implementing the MyObjectService
// interface, and returns a new server stub.
func NewServerMyObject(server MyObjectService) interface{} {
	return &ServerStubMyObject{
		service: server,
	}
}

// clientStubMyObject implements MyObject.
type clientStubMyObject struct {
	defaultClient _gen_ipc.Client
	name          string
}

func (__gen_c *clientStubMyObject) client(ctx _gen_context.T) _gen_ipc.Client {
	if __gen_c.defaultClient != nil {
		return __gen_c.defaultClient
	}
	return _gen_veyron2.RuntimeFromContext(ctx).Client()
}

func (__gen_c *clientStubMyObject) Get(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "Get", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubMyObject) Put(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "Put", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubMyObject) Resolve(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "Resolve", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubMyObject) NoTags(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "NoTags", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubMyObject) AllTags(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "AllTags", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubMyObject) UnresolveStep(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply []string, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "UnresolveStep", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubMyObject) Signature(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply _gen_ipc.ServiceSignature, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubMyObject) GetMethodTags(ctx _gen_context.T, method string, opts ..._gen_ipc.CallOpt) (reply []interface{}, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client(ctx).StartCall(ctx, __gen_c.name, "GetMethodTags", []interface{}{method}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

// ServerStubMyObject wraps a server that implements
// MyObjectService and provides an object that satisfies
// the requirements of veyron2/ipc.ReflectInvoker.
type ServerStubMyObject struct {
	service MyObjectService
}

func (__gen_s *ServerStubMyObject) GetMethodTags(call _gen_ipc.ServerCall, method string) ([]interface{}, error) {
	// TODO(bprosnitz) GetMethodTags() will be replaces with Signature().
	// Note: This exhibits some weird behavior like returning a nil error if the method isn't found.
	// This will change when it is replaced with Signature().
	switch method {
	case "Get":
		return []interface{}{MyTag("R")}, nil
	case "Put":
		return []interface{}{MyTag("W")}, nil
	case "Resolve":
		return []interface{}{MyTag("X")}, nil
	case "NoTags":
		return []interface{}{}, nil
	case "AllTags":
		return []interface{}{MyTag("R"), MyTag("W"), MyTag("X")}, nil
	default:
		return nil, nil
	}
}

func (__gen_s *ServerStubMyObject) Signature(call _gen_ipc.ServerCall) (_gen_ipc.ServiceSignature, error) {
	result := _gen_ipc.ServiceSignature{Methods: make(map[string]_gen_ipc.MethodSignature)}
	result.Methods["AllTags"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 65},
		},
	}
	result.Methods["Get"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 65},
		},
	}
	result.Methods["NoTags"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 65},
		},
	}
	result.Methods["Put"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 65},
		},
	}
	result.Methods["Resolve"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 65},
		},
	}

	result.TypeDefs = []_gen_vdlutil.Any{
		_gen_wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

func (__gen_s *ServerStubMyObject) UnresolveStep(call _gen_ipc.ServerCall) (reply []string, err error) {
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

func (__gen_s *ServerStubMyObject) Get(call _gen_ipc.ServerCall) (err error) {
	err = __gen_s.service.Get(call)
	return
}

func (__gen_s *ServerStubMyObject) Put(call _gen_ipc.ServerCall) (err error) {
	err = __gen_s.service.Put(call)
	return
}

func (__gen_s *ServerStubMyObject) Resolve(call _gen_ipc.ServerCall) (err error) {
	err = __gen_s.service.Resolve(call)
	return
}

func (__gen_s *ServerStubMyObject) NoTags(call _gen_ipc.ServerCall) (err error) {
	err = __gen_s.service.NoTags(call)
	return
}

func (__gen_s *ServerStubMyObject) AllTags(call _gen_ipc.ServerCall) (err error) {
	err = __gen_s.service.AllTags(call)
	return
}
