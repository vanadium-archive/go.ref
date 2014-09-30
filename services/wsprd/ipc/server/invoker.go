package server

import (
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
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
}

// newInvoker is an invoker factory
func newInvoker(sig ipc.ServiceSignature, invokeFunc remoteInvokeFunc) ipc.Invoker {
	predefinedInvokers := make(map[string]ipc.Invoker)

	// Special handling for predefined "signature" method
	predefinedInvokers["Signature"] = newSignatureInvoker(sig)

	i := &invoker{sig, invokeFunc, predefinedInvokers}
	return i
}

// Prepare implements the Invoker interface.
func (i *invoker) Prepare(methodName string, numArgs int) ([]interface{}, security.Label, error) {

	if pi := i.predefinedInvokers[methodName]; pi != nil {
		return pi.Prepare(methodName, numArgs)
	}

	method, ok := i.sig.Methods[methodName]
	if !ok {
		return nil, security.AdminLabel, verror.NoExistf("method name not found in IDL: %s", methodName)
	}

	argptrs := make([]interface{}, len(method.InArgs))

	for ix := range method.InArgs {
		var x interface{}
		argptrs[ix] = &x // Accept AnyData
	}

	securityLabel := methodSecurityLabel(method)

	return argptrs, securityLabel, nil
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

// methodSecurityLabel returns the security label for a given method.
func methodSecurityLabel(methodSig ipc.MethodSignature) security.Label {
	// TODO(bprosnitz) Get the security label and return it here.
	return security.AdminLabel
}
