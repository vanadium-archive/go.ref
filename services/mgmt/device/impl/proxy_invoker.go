package impl

import (
	"fmt"
	"io"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vdl/vdlroot/src/signature"
)

// proxyInvoker is an ipc.Invoker implementation that proxies all requests
// to a remote object, i.e. requests to <suffix> are forwarded to
// <remote> transparently.
//
// remote is the name of the remote object.
// access is the access tag require to access the object.
// desc is used to determine the number of results for a given method.
func newProxyInvoker(remote string, access access.Tag, desc []ipc.InterfaceDesc) *proxyInvoker {
	methodNumResults := make(map[string]int)
	for _, iface := range desc {
		for _, method := range iface.Methods {
			methodNumResults[method.Name] = len(method.OutArgs)
		}
	}
	return &proxyInvoker{remote, access, methodNumResults}
}

type proxyInvoker struct {
	remote           string
	access           access.Tag
	methodNumResults map[string]int
}

var _ ipc.Invoker = (*proxyInvoker)(nil)

func (p *proxyInvoker) Prepare(method string, numArgs int) (argptrs []interface{}, tags []*vdl.Value, _ error) {
	// TODO(toddw): Change argptrs to be filled in with *vdl.Value, to avoid
	// unnecessary type lookups.
	argptrs = make([]interface{}, numArgs)
	for i, _ := range argptrs {
		var x interface{}
		argptrs[i] = &x
	}
	tags = []*vdl.Value{vdl.ValueOf(p.access)}
	return
}

func (p *proxyInvoker) Invoke(method string, inCall ipc.ServerCall, argptrs []interface{}) (results []interface{}, err error) {
	// We accept any values as argument and pass them through to the remote
	// server.
	args := make([]interface{}, len(argptrs))
	for i, ap := range argptrs {
		args[i] = ap
	}
	ctx := inCall.Context()
	client := veyron2.GetClient(ctx)

	outCall, err := client.StartCall(ctx, p.remote, method, args)
	if err != nil {
		return nil, err
	}

	// Each RPC has a bi-directional stream, and there is no way to know in
	// advance how much data will be sent in either direction, if any.
	//
	// This method (Invoke) must return when the remote server is done with
	// the RPC, which is when outCall.Recv() returns EOF. When that happens,
	// we need to call outCall.Finish() to get the return values, and then
	// return these values to the client.
	//
	// While we are forwarding data from the server to the client, we must
	// also forward data from the client to the server. This happens in a
	// separate goroutine. This goroutine may return after Invoke has
	// returned if the client doesn't call CloseSend() explicitly.
	//
	// Any error, other than EOF, will be returned to the client, if
	// possible. The only situation where it is not possible to send an
	// error to the client is when the error comes from forwarding data from
	// the client to the server and Invoke has already returned or is about
	// to return. In this case, the error is lost. So, it is possible that
	// the client could successfully Send() data that the server doesn't
	// actually receive if the server terminates the RPC while the data is
	// in the proxy.
	fwd := func(src, dst ipc.Stream, errors chan<- error) {
		for {
			var obj interface{}
			switch err := src.Recv(&obj); err {
			case io.EOF:
				if call, ok := src.(ipc.Call); ok {
					if err := call.CloseSend(); err != nil {
						errors <- err
					}
				}
				return
			case nil:
				break
			default:
				errors <- err
				return
			}
			if err := dst.Send(obj); err != nil {
				errors <- err
				return
			}
		}
	}
	errors := make(chan error, 2)
	go fwd(inCall, outCall, errors)
	fwd(outCall, inCall, errors)
	select {
	case err := <-errors:
		return nil, err
	default:
	}

	nResults, err := p.numResults(method)
	if err != nil {
		return nil, err
	}

	// We accept any return values, without type checking, and return them
	// to the client.
	res := make([]interface{}, nResults)
	for i := 0; i < len(res); i++ {
		var foo interface{}
		res[i] = &foo
	}
	err = outCall.Finish(res...)
	results = make([]interface{}, len(res))
	for i, r := range res {
		results[i] = *r.(*interface{})
	}
	return results, err
}

// TODO(toddw): Expose a helper function that performs all error checking based
// on reflection, to simplify the repeated logic processing results.
func (p *proxyInvoker) Signature(ctx ipc.ServerContext) ([]signature.Interface, error) {
	call, ok := ctx.(ipc.ServerCall)
	if !ok {
		return nil, fmt.Errorf("couldn't upgrade ipc.ServerContext to ipc.ServerCall")
	}
	results, err := p.Invoke(ipc.ReservedSignature, call, nil)
	if err != nil {
		return nil, err
	}
	if len(results) != 2 {
		return nil, fmt.Errorf("unexpected number of result values. Got %d, want 2.", len(results))
	}
	if results[1] != nil {
		err, ok := results[1].(error)
		if !ok {
			return nil, fmt.Errorf("unexpected error type. Got %T, want error.", err)
		}
		return nil, err
	}
	var res []signature.Interface
	if results[0] != nil {
		sig, ok := results[0].([]signature.Interface)
		if !ok {
			return nil, fmt.Errorf("unexpected result value type. Got %T, want []signature.Interface.", sig)
		}
	}
	return res, nil
}

func (p *proxyInvoker) MethodSignature(ctx ipc.ServerContext, method string) (signature.Method, error) {
	empty := signature.Method{}
	call, ok := ctx.(ipc.ServerCall)
	if !ok {
		return empty, fmt.Errorf("couldn't upgrade ipc.ServerContext to ipc.ServerCall")
	}
	results, err := p.Invoke(ipc.ReservedMethodSignature, call, []interface{}{&method})
	if err != nil {
		return empty, err
	}
	if len(results) != 2 {
		return empty, fmt.Errorf("unexpected number of result values. Got %d, want 2.", len(results))
	}
	if results[1] != nil {
		err, ok := results[1].(error)
		if !ok {
			return empty, fmt.Errorf("unexpected error type. Got %T, want error.", err)
		}
		return empty, err
	}
	var res signature.Method
	if results[0] != nil {
		sig, ok := results[0].(signature.Method)
		if !ok {
			return empty, fmt.Errorf("unexpected result value type. Got %T, want signature.Method.", sig)
		}
	}
	return res, nil
}

func (p *proxyInvoker) Globber() *ipc.GlobState {
	return &ipc.GlobState{AllGlobber: p}
}

type call struct {
	ipc.ServerContext
	ch chan<- naming.VDLGlobReply
}

func (c *call) Recv(v interface{}) error {
	return io.EOF
}

func (c *call) Send(v interface{}) error {
	c.ch <- v.(naming.VDLGlobReply)
	return nil
}

func (p *proxyInvoker) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLGlobReply, error) {
	ch := make(chan naming.VDLGlobReply)
	go func() {
		p.Invoke(ipc.GlobMethod, &call{ctx, ch}, []interface{}{&pattern})
		close(ch)
	}()
	return ch, nil
}

// numResults returns the number of result values for the given method.
func (p *proxyInvoker) numResults(method string) (int, error) {
	switch method {
	case ipc.GlobMethod:
		return 1, nil
	case ipc.ReservedSignature, ipc.ReservedMethodSignature:
		return 2, nil
	}
	num, ok := p.methodNumResults[method]
	if !ok {
		return 0, fmt.Errorf("unknown method %q", method)
	}
	return num, nil
}
