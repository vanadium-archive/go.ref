// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/rpc"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
)

var typedNil []int

const pkgPath = "v.io/x/ref/services/wspr/internal/rpc/server"

// Errors.
var (
	ErrWrongNumberOfArgs         = verror.Register(pkgPath+".ErrWrongNumberOfArgs", verror.NoRetry, "{1:}{2:} Method {3} got {4} args, want {5}{:_}")
	ErrMethodNotFoundInSignature = verror.Register(pkgPath+".ErrMethodNotFoundInSignature", verror.NoRetry, "{1:}{2:} Method {3} not found in signature{:_}")
)

// invoker holds a delegate function to call on invoke and a list of methods that
// are available for be called.
type invoker struct {
	// delegate function to call when an invoke request comes in
	invokeFunc remoteInvokeFunc

	signature []signature.Interface

	globFunc remoteGlobFunc
}

var _ rpc.Invoker = (*invoker)(nil)

// newInvoker is an invoker factory
func newInvoker(signature []signature.Interface, invokeFunc remoteInvokeFunc, globFunc remoteGlobFunc) rpc.Invoker {
	i := &invoker{invokeFunc, signature, globFunc}
	return i
}

// Prepare implements the Invoker interface.
func (i *invoker) Prepare(_ *context.T, methodName string, numArgs int) ([]interface{}, []*vdl.Value, error) {
	method, err := i.MethodSignature(nil, nil, methodName)
	if err != nil {
		return nil, nil, err
	}
	if got, want := numArgs, len(method.InArgs); got != want {
		return nil, nil, verror.New(ErrWrongNumberOfArgs, nil, methodName, got, want)
	}
	argptrs := make([]interface{}, len(method.InArgs))
	for ix, arg := range method.InArgs {
		argptrs[ix] = vdl.ZeroValue(arg.Type)
	}
	return argptrs, method.Tags, nil
}

// Invoke implements the Invoker interface.
func (i *invoker) Invoke(ctx *context.T, call rpc.StreamServerCall, methodName string, argptrs []interface{}) ([]interface{}, error) {
	replychan := i.invokeFunc(ctx, call, methodName, argptrs)

	// Wait for the result
	reply := <-replychan

	if reply.Err != nil {
		return nil, reply.Err
	}

	vtrace.GetStore(ctx).Merge(reply.TraceResponse)

	// Convert the reply.Results from []*vom.RawBytes to []interface{}
	results := make([]interface{}, len(reply.Results))
	for i, r := range reply.Results {
		results[i] = r
	}
	return results, nil
}

func (i *invoker) Globber() *rpc.GlobState {
	if i.globFunc == nil {
		return nil
	}
	return &rpc.GlobState{AllGlobber: i}
}

func (i *invoker) Glob__(ctx *context.T, call rpc.GlobServerCall, g *glob.Glob) error {
	// TODO(rthellend,bjornick): Should we convert globFunc to match the
	// new Glob__ interface?
	ch, err := i.globFunc(ctx, call, g.String())
	if ch != nil {
		for reply := range ch {
			call.SendStream().Send(reply)
		}
	}
	return err
}

func (i *invoker) Signature(ctx *context.T, call rpc.ServerCall) ([]signature.Interface, error) {
	return i.signature, nil
}

func (i *invoker) MethodSignature(ctx *context.T, call rpc.ServerCall, method string) (signature.Method, error) {
	if methodSig, ok := signature.FirstMethod(i.signature, method); ok {
		return methodSig, nil
	}
	return signature.Method{}, verror.New(ErrMethodNotFoundInSignature, ctx, method)
}
