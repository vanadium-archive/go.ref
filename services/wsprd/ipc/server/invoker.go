package server

import (
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/verror"

	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/signature"
)

var typedNil []int

// invoker holds a delegate function to call on invoke and a list of methods that
// are available for be called.
type invoker struct {
	// signature of the service this invoker belogs to
	sig ipc.ServiceSignature
	// delegate function to call when an invoke request comes in
	invokeFunc remoteInvokeFunc
	// map of special methods like "Signature" which invoker handles on behalf of the actual service
	predefinedInvokers map[string]ipc.Invoker

	// This is to get the method tags.  TODO(bjornick): Remove this when vom2 signatures
	// has tags.
	jsonSig signature.JSONServiceSignature
}

var _ ipc.Invoker = (*invoker)(nil)

// newInvoker is an invoker factory
func newInvoker(sig ipc.ServiceSignature, jsonSig signature.JSONServiceSignature, invokeFunc remoteInvokeFunc) ipc.Invoker {
	predefinedInvokers := make(map[string]ipc.Invoker)

	// Special handling for predefined "signature" method
	predefinedInvokers["Signature"] = newSignatureInvoker(sig)

	i := &invoker{sig, invokeFunc, predefinedInvokers, jsonSig}
	return i
}

// Prepare implements the Invoker interface.
func (i *invoker) Prepare(methodName string, numArgs int) ([]interface{}, []interface{}, error) {
	if pi := i.predefinedInvokers[methodName]; pi != nil {
		return pi.Prepare(methodName, numArgs)
	}

	method, ok := i.jsonSig[lib.LowercaseFirstCharacter(methodName)]
	if !ok {
		return nil, nil, verror.NoExistf("method name not found in IDL: %s", methodName)
	}

	argptrs := make([]interface{}, len(method.InArgs))

	for ix := range method.InArgs {
		var x interface{}
		argptrs[ix] = &x // Accept AnyData
	}

	return argptrs, method.Tags, nil
}

// Invoke implements the Invoker interface.
func (i *invoker) Invoke(methodName string, call ipc.ServerCall, argptrs []interface{}) ([]interface{}, error) {

	if pi := i.predefinedInvokers[methodName]; pi != nil {
		return pi.Invoke(methodName, call, argptrs)
	}

	if _, ok := i.sig.Methods[methodName]; !ok {
		return nil, verror.NoExistf("method name not found in IDL: %s", methodName)
	}

	replychan := i.invokeFunc(methodName, argptrs, call)

	// Wait for the result
	reply := <-replychan

	var err error = nil
	if reply.Err != nil {
		err = reply.Err
	}

	for i, v := range reply.Results {
		if v == nil {
			reply.Results[i] = typedNil
		}
	}

	// We always assume JavaScript services might return error.
	// JavaScript returns non-error results in reply.Results & error in reply.Err
	// We add the error as the last result of the ipc invoke call since last
	// out arg is where application error is expected to be.
	results := make([]interface{}, len(reply.Results)+1)
	results = append(reply.Results, err)

	return results, nil
}

// TODO(bjornick,rthellend): Find a reasonable way to implement this for JS.
func (i *invoker) Globber() *ipc.GlobState {
	return nil
}

// TODO(bjornick,toddw): Implement this for JS.
func (i *invoker) Signature(ctx ipc.ServerContext) ([]ipc.InterfaceSig, error) {
	return nil, nil
}

// TODO(bjornick,toddw): Implement this for JS.
func (i *invoker) MethodSignature(ctx ipc.ServerContext, method string) (ipc.MethodSig, error) {
	return ipc.MethodSig{}, nil
}
