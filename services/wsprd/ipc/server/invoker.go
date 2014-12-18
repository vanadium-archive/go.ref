package server

import (
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vdl"
	"veyron.io/veyron/veyron2/vdl/vdlroot/src/signature"
	verror "veyron.io/veyron/veyron2/verror2"
)

var typedNil []int

const pkgPath = "veyron.io/wspr/veyron/services/wsprd/ipc/server"

// Errors.
var (
	errWrongNumberOfArgs         = verror.Register(pkgPath+".errWrongNumberOfArgs", verror.NoRetry, "{1:}{2:} Method {3} got {4} args, want {5}{:_}")
	errMethodNotFoundInSignature = verror.Register(pkgPath+".errMethodNotFoundInSignature", verror.NoRetry, "{1:}{2:} Method {3} not found in signature{:_}")
)

// invoker holds a delegate function to call on invoke and a list of methods that
// are available for be called.
type invoker struct {
	// delegate function to call when an invoke request comes in
	invokeFunc remoteInvokeFunc

	signature []signature.Interface
}

var _ ipc.Invoker = (*invoker)(nil)

// newInvoker is an invoker factory
func newInvoker(signature []signature.Interface, invokeFunc remoteInvokeFunc) ipc.Invoker {
	i := &invoker{invokeFunc, signature}
	return i
}

// Prepare implements the Invoker interface.
func (i *invoker) Prepare(methodName string, numArgs int) ([]interface{}, []interface{}, error) {
	method, err := i.MethodSignature(nil, methodName)
	if err != nil {
		return nil, nil, err
	}
	if got, want := numArgs, len(method.InArgs); got != want {
		return nil, nil, verror.Make(errWrongNumberOfArgs, nil, methodName, got, want)
	}
	argptrs := make([]interface{}, len(method.InArgs))
	for ix, arg := range method.InArgs {
		argptrs[ix] = vdl.ZeroValue(arg.Type)
	}

	tags := make([]interface{}, len(method.Tags))
	for ix, tag := range method.Tags {
		tags[ix] = (interface{})(tag)
	}

	return argptrs, tags, nil
}

// Invoke implements the Invoker interface.
func (i *invoker) Invoke(methodName string, call ipc.ServerCall, argptrs []interface{}) ([]interface{}, error) {
	replychan := i.invokeFunc(methodName, argptrs, call)

	// Wait for the result
	reply := <-replychan

	var err error
	if reply.Err != nil {
		err = reply.Err
	}

	// We always assume JavaScript services might return error.
	// JavaScript returns non-error results in reply.Results & error in reply.Err
	// We add the error as the last result of the ipc invoke call since last
	// out arg is where application error is expected to be.
	results := make([]interface{}, len(reply.Results))
	results = append(reply.Results, err)

	return results, nil
}

// TODO(bjornick,rthellend): Find a reasonable way to implement this for JS.
func (i *invoker) Globber() *ipc.GlobState {
	return nil
}

func (i *invoker) Signature(ctx ipc.ServerContext) ([]signature.Interface, error) {
	return i.signature, nil
}

func (i *invoker) MethodSignature(ctx ipc.ServerContext, method string) (signature.Method, error) {
	if methodSig, ok := signature.FirstMethod(i.signature, method); ok {
		return methodSig, nil
	}
	return signature.Method{}, verror.Make(errMethodNotFoundInSignature, ctx, method)
}
