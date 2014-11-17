package server

import (
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
)

// signatureInvoker acts as the signature() method and is used to handle calls
// to signature() on behalf of the service
//
// TODO(toddw): Replace this with the new Signature call.
type signatureInvoker struct {
	// signature of the service
	sig ipc.ServiceSignature
}

var _ ipc.Invoker = (*signatureInvoker)(nil)

func (i *signatureInvoker) signature() ipc.ServiceSignature {
	return i.sig
}

// newSignatureInvoker is an invoker factory
func newSignatureInvoker(sig ipc.ServiceSignature) ipc.Invoker {
	return &signatureInvoker{sig}
}

// Prepare implements the Invoker interface.
func (i *signatureInvoker) Prepare(methodName string, _ int) (argptrs, tags []interface{}, err error) {
	return []interface{}{}, []interface{}{security.ReadLabel}, nil
}

// Invoke implements the Invoker interface.
func (i *signatureInvoker) Invoke(methodName string, call ipc.ServerCall, argptrs []interface{}) ([]interface{}, error) {
	return []interface{}{i.signature(), nil}, nil
}

func (i *signatureInvoker) VGlob() *ipc.GlobState {
	return nil
}

func (i *signatureInvoker) Signature(ctx ipc.ServerContext) ([]ipc.InterfaceSig, error) {
	return nil, nil
}

func (i *signatureInvoker) MethodSignature(ctx ipc.ServerContext, method string) (ipc.MethodSig, error) {
	return ipc.MethodSig{}, nil
}
