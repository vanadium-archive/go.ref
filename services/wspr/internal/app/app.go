// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The app package contains the struct that keeps per javascript app state and handles translating
// javascript requests to vanadium requests and vice versa.
package app

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/ref/services/wspr/internal/lib"
	"v.io/x/ref/services/wspr/internal/namespace"
	"v.io/x/ref/services/wspr/internal/principal"
	"v.io/x/ref/services/wspr/internal/rpc/server"
)

const (
	// pkgPath is the prefix os errors in this package.
	pkgPath = "v.io/x/ref/services/wspr/internal/app"

	typeFlow = 0
)

// Errors
var (
	marshallingError = verror.Register(pkgPath+".marshallingError", verror.NoRetry, "{1} {2} marshalling error {_}")
	noResults        = verror.Register(pkgPath+".noResults", verror.NoRetry, "{1} {2} no results from call {_}")
	badCaveatType    = verror.Register(pkgPath+".badCaveatType", verror.NoRetry, "{1} {2} bad caveat type {_}")
	unknownBlessings = verror.Register(pkgPath+".unknownBlessings", verror.NoRetry, "{1} {2} unknown public id {_}")
)

type outstandingRequest struct {
	stream *outstandingStream
	cancel context.CancelFunc
}

// Controller represents all the state of a Vanadium Web App.  This is the struct
// that is in charge performing all the vanadium options.
type Controller struct {
	// Protects everything.
	// TODO(bjornick): We need to split this up.
	sync.Mutex

	// The context of this controller.
	ctx *context.T

	// The cleanup function for this controller.
	cancel context.CancelFunc

	// The rpc.ListenSpec to use with server.Listen
	listenSpec *rpc.ListenSpec

	// Used to generate unique ids for requests initiated by the proxy.
	// These ids will be even so they don't collide with the ids generated
	// by the client.
	lastGeneratedId int32

	// Used to keep track of data (streams and cancellation functions) for
	// outstanding requests.
	outstandingRequests map[int32]*outstandingRequest

	// Maps flowids to the server that owns them.
	flowMap map[int32]interface{}

	// A manager that Handles fetching and caching signature of remote services
	signatureManager lib.SignatureManager

	// We maintain multiple Vanadium server per pipe for serving JavaScript
	// services.
	servers map[uint32]*server.Server

	// Creates a client writer for a given flow.  This is a member so that tests can override
	// the default implementation.
	writerCreator func(id int32) lib.ClientWriter

	// Cache of Blessings that were sent to Javascript.
	blessingsCache *principal.BlessingsCache

	// reservedServices contains a map of reserved service names.  These
	// are objects that serve requests in wspr without actually making
	// an outgoing rpc call.
	reservedServices map[string]rpc.Invoker

	typeEncoder *vom.TypeEncoder

	typeDecoder *vom.TypeDecoder
	typeReader  *lib.TypeReader
}

var _ ControllerServerMethods = (*Controller)(nil)

// NewController creates a new Controller.  writerCreator will be used to create a new flow for rpcs to
// javascript server.
func NewController(ctx *context.T, writerCreator func(id int32) lib.ClientWriter, listenSpec *rpc.ListenSpec, namespaceRoots []string, p security.Principal) (*Controller, error) {
	ctx, cancel := context.WithCancel(ctx)

	if namespaceRoots != nil {
		var err error
		ctx, _, err = v23.WithNewNamespace(ctx, namespaceRoots...)
		if err != nil {
			return nil, err
		}
	}

	ctx, _ = vtrace.WithNewTrace(ctx)

	ctx, err := v23.WithPrincipal(ctx, p)
	if err != nil {
		return nil, err
	}

	controller := &Controller{
		ctx:           ctx,
		cancel:        cancel,
		writerCreator: writerCreator,
		listenSpec:    listenSpec,
	}

	controller.blessingsCache = principal.NewBlessingsCache(controller.SendBlessingsCacheMessages, principal.PeriodicGcPolicy(1*time.Minute))

	controllerInvoker, err := rpc.ReflectInvoker(ControllerServer(controller))
	if err != nil {
		return nil, err
	}
	namespaceInvoker, err := rpc.ReflectInvoker(namespace.New(ctx))
	if err != nil {
		return nil, err
	}
	controller.reservedServices = map[string]rpc.Invoker{
		"__controller": controllerInvoker,
		"__namespace":  namespaceInvoker,
	}

	controller.setup()
	return controller, nil
}

// finishCall waits for the call to finish and write out the response to w.
func (c *Controller) finishCall(ctx *context.T, w lib.ClientWriter, clientCall rpc.ClientCall, msg *RpcRequest, span vtrace.Span) {
	if msg.IsStreaming {
		for {
			var item interface{}
			if err := clientCall.Recv(&item); err != nil {
				if err == io.EOF {
					break
				}
				w.Error(err) // Send streaming error as is
				return
			}
			vomItem, err := lib.HexVomEncode(item, c.typeEncoder)
			if err != nil {
				w.Error(verror.New(marshallingError, ctx, item, err))
				continue
			}
			if err := w.Send(lib.ResponseStream, vomItem); err != nil {
				w.Error(verror.New(marshallingError, ctx, item))
			}
		}
		if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
			w.Error(verror.New(marshallingError, ctx, "ResponseStreamClose"))
		}
	}
	results := make([]*vdl.Value, msg.NumOutArgs)
	wireBlessingsType := vdl.TypeOf(security.WireBlessings{})
	// This array will have pointers to the values in results.
	resultptrs := make([]interface{}, msg.NumOutArgs)
	for i := range results {
		resultptrs[i] = &results[i]
	}
	if err := clientCall.Finish(resultptrs...); err != nil {
		// return the call system error as is
		w.Error(err)
		return
	}
	for i, val := range results {
		if val.Type() == wireBlessingsType {
			var blessings security.Blessings
			if err := vdl.Convert(&blessings, val); err != nil {
				w.Error(err)
				return
			}
			results[i] = vdl.ValueOf(c.blessingsCache.Put(blessings))
		}
	}
	c.sendRPCResponse(ctx, w, span, results)
}

func (c *Controller) sendRPCResponse(ctx *context.T, w lib.ClientWriter, span vtrace.Span, results []*vdl.Value) {
	span.Finish()
	response := RpcResponse{
		OutArgs:       results,
		TraceResponse: vtrace.GetResponse(ctx),
	}
	var buf bytes.Buffer
	encoder := vom.NewEncoderWithTypeEncoder(&buf, c.typeEncoder)
	if err := encoder.Encode(response); err != nil {
		w.Error(err)
		return
	}
	encoded := hex.EncodeToString(buf.Bytes())
	if err := w.Send(lib.ResponseFinal, encoded); err != nil {
		w.Error(verror.Convert(marshallingError, ctx, err))
	}
}

// callOpts turns a slice of type []RpcCallOption object into an array of rpc.CallOpt.
func (c *Controller) callOpts(opts []RpcCallOption) ([]rpc.CallOpt, error) {
	var callOpts []rpc.CallOpt

	for _, opt := range opts {
		switch v := opt.(type) {
		case RpcCallOptionAllowedServersPolicy:
			callOpts = append(callOpts, options.ServerAuthorizer{access.AccessList{In: v.Value}})
		case RpcCallOptionRetryTimeout:
			callOpts = append(callOpts, options.RetryTimeout(v.Value))
		case RpcCallOptionGranter:
			callOpts = append(callOpts, &jsGranter{c, v.Value})
		default:
			return nil, fmt.Errorf("Unknown RpcCallOption type %T", v)
		}
	}

	return callOpts, nil
}

// serverOpts turns a slice of type []RpcServerOptions object into an array of rpc.ServerOpt.
func (c *Controller) serverOpts(opts []RpcServerOption) ([]rpc.ServerOpt, error) {
	var serverOpts []rpc.ServerOpt

	for _, opt := range opts {
		switch v := opt.(type) {
		case RpcServerOptionIsLeaf:
			serverOpts = append(serverOpts, options.IsLeaf(v.Value))
		case RpcServerOptionServesMountTable:
			serverOpts = append(serverOpts, options.ServesMountTable(v.Value))
		default:
			return nil, fmt.Errorf("Unknown RpcServerOption type %T", v)
		}
	}

	return serverOpts, nil
}

func (c *Controller) startCall(ctx *context.T, w lib.ClientWriter, msg *RpcRequest, inArgs []interface{}) (rpc.ClientCall, error) {
	methodName := lib.UppercaseFirstCharacter(msg.Method)
	callOpts, err := c.callOpts(msg.CallOptions)
	if err != nil {
		return nil, err
	}
	clientCall, err := v23.GetClient(ctx).StartCall(ctx, msg.Name, methodName, inArgs, callOpts...)
	if err != nil {
		return nil, fmt.Errorf("error starting call (name: %v, method: %v, args: %v): %v", msg.Name, methodName, inArgs, err)
	}

	return clientCall, nil
}

// Implements the serverHelper interface

// CreateNewFlow creats a new server flow that will be used to write out
// streaming messages to Javascript.
func (c *Controller) CreateNewFlow(s interface{}, stream rpc.Stream) *server.Flow {
	c.Lock()
	defer c.Unlock()
	id := c.lastGeneratedId
	c.lastGeneratedId += 2
	c.flowMap[id] = s
	os := newStream(c.typeDecoder)
	os.init(stream)
	c.outstandingRequests[id] = &outstandingRequest{
		stream: os,
	}
	return &server.Flow{ID: id, Writer: c.writerCreator(id)}
}

// CleanupFlow removes the bookkeeping for a previously created flow.
func (c *Controller) CleanupFlow(id int32) {
	c.Lock()
	request := c.outstandingRequests[id]
	delete(c.outstandingRequests, id)
	delete(c.flowMap, id)
	c.Unlock()
	if request != nil && request.stream != nil {
		request.stream.end()
		request.stream.waitUntilDone()
	}
}

// BlessingsCache gets the blessings cache used by the controller.
func (c *Controller) BlessingsCache() *principal.BlessingsCache {
	return c.blessingsCache
}

// RT returns the runtime of the app.
func (c *Controller) Context() *context.T {
	return c.ctx
}

// Cleanup cleans up any outstanding rpcs.
func (c *Controller) Cleanup(ctx *context.T) {
	ctx.VI(0).Info("Cleaning up controller")
	c.Lock()

	for _, request := range c.outstandingRequests {
		if request.cancel != nil {
			request.cancel()
		}
		if request.stream != nil {
			request.stream.end()
		}
	}

	servers := []*server.Server{}
	for _, server := range c.servers {
		servers = append(servers, server)
	}

	c.Unlock()

	// We must unlock before calling server.Stop otherwise it can deadlock.
	for _, server := range servers {
		server.Stop()
	}

	c.typeReader.Close()
	c.cancel()

	c.typeDecoder.Stop()
}

func (c *Controller) setup() {
	c.signatureManager = lib.NewSignatureManager()
	c.outstandingRequests = make(map[int32]*outstandingRequest)
	c.flowMap = make(map[int32]interface{})
	c.servers = make(map[uint32]*server.Server)
	c.typeReader = lib.NewTypeReader()
	c.typeDecoder = vom.NewTypeDecoder(c.typeReader)
	c.typeDecoder.Start()
	c.typeEncoder = vom.NewTypeEncoder(lib.NewTypeWriter(c.writerCreator(typeFlow)))
	c.lastGeneratedId += 2
}

// SendOnStream writes data on id's stream.  The actual network write will be
// done asynchronously.  If there is an error, it will be sent to w.
func (c *Controller) SendOnStream(ctx *context.T, id int32, data string, w lib.ClientWriter) {
	c.Lock()
	request := c.outstandingRequests[id]
	if request == nil || request.stream == nil {
		ctx.Errorf("unknown stream: %d", id)
		c.Unlock()
		return
	}
	stream := request.stream
	c.Unlock()
	stream.send(data, w)
}

// SendVeyronRequest makes a vanadium request for the given flowId.  If signal is non-nil, it will receive
// the call object after it has been constructed.
func (c *Controller) sendVeyronRequest(ctx *context.T, id int32, msg *RpcRequest, inArgs []interface{}, w lib.ClientWriter, stream *outstandingStream, span vtrace.Span) {
	// We have to make the start call synchronous so we can make sure that we populate
	// the call map before we can Handle a recieve call.
	call, err := c.startCall(ctx, w, msg, inArgs)
	if err != nil {
		w.Error(verror.Convert(verror.ErrInternal, ctx, err))
		return
	}

	if stream != nil {
		stream.init(call)
	}

	c.finishCall(ctx, w, call, msg, span)
	c.Lock()
	if request, ok := c.outstandingRequests[id]; ok {
		delete(c.outstandingRequests, id)
		if request.cancel != nil {
			request.cancel()
		}
	}
	c.Unlock()
}

// TODO(mattr): This is a very limited implementation of ServerCall,
// but currently none of the methods the controller exports require
// any of this context information.
type localCall struct {
	ctx         *context.T
	vrpc        *RpcRequest
	tags        []*vdl.Value
	w           lib.ClientWriter
	typeEncoder *vom.TypeEncoder
}

var (
	_ rpc.StreamServerCall = (*localCall)(nil)
	_ security.Call        = (*localCall)(nil)
)

func (l *localCall) Send(item interface{}) error {
	vomItem, err := lib.HexVomEncode(item, l.typeEncoder)
	if err != nil {
		err = verror.New(marshallingError, l.ctx, item, err)
		l.w.Error(err)
		return err
	}
	if err := l.w.Send(lib.ResponseStream, vomItem); err != nil {
		err = verror.New(marshallingError, l.ctx, item)
		l.w.Error(err)
		return err
	}
	return nil
}
func (l *localCall) Recv(interface{}) error                          { return nil }
func (l *localCall) GrantedBlessings() security.Blessings            { return security.Blessings{} }
func (l *localCall) Server() rpc.Server                              { return nil }
func (l *localCall) Timestamp() (t time.Time)                        { return }
func (l *localCall) Method() string                                  { return l.vrpc.Method }
func (l *localCall) MethodTags() []*vdl.Value                        { return l.tags }
func (l *localCall) Suffix() string                                  { return l.vrpc.Name }
func (l *localCall) LocalDischarges() map[string]security.Discharge  { return nil }
func (l *localCall) RemoteDischarges() map[string]security.Discharge { return nil }
func (l *localCall) LocalPrincipal() security.Principal              { return nil }
func (l *localCall) LocalBlessings() security.Blessings              { return security.Blessings{} }
func (l *localCall) RemoteBlessings() security.Blessings             { return security.Blessings{} }
func (l *localCall) LocalEndpoint() naming.Endpoint                  { return nil }
func (l *localCall) RemoteEndpoint() naming.Endpoint                 { return nil }
func (l *localCall) Security() security.Call                         { return l }

func (c *Controller) handleInternalCall(ctx *context.T, invoker rpc.Invoker, msg *RpcRequest, w lib.ClientWriter, span vtrace.Span, decoder *vom.Decoder) {
	argptrs, tags, err := invoker.Prepare(ctx, msg.Method, int(msg.NumInArgs))
	if err != nil {
		w.Error(verror.Convert(verror.ErrInternal, ctx, err))
		return
	}
	for _, argptr := range argptrs {
		if err := decoder.Decode(argptr); err != nil {
			w.Error(verror.Convert(verror.ErrInternal, ctx, err))
			return
		}
	}
	results, err := invoker.Invoke(ctx, &localCall{ctx, msg, tags, w, c.typeEncoder}, msg.Method, argptrs)
	if err != nil {
		w.Error(verror.Convert(verror.ErrInternal, ctx, err))
		return
	}
	if msg.IsStreaming {
		if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
			w.Error(verror.New(marshallingError, ctx, "ResponseStreamClose"))
		}
	}

	// Convert results from []interface{} to []*vdl.Value.
	vresults := make([]*vdl.Value, len(results))
	for i, res := range results {
		vv, err := vdl.ValueFromReflect(reflect.ValueOf(res))
		if err != nil {
			w.Error(verror.Convert(verror.ErrInternal, ctx, err))
			return
		}
		vresults[i] = vv
	}
	c.sendRPCResponse(ctx, w, span, vresults)
}

// HandleCaveatValidationResponse handles the response to caveat validation
// requests.
func (c *Controller) HandleCaveatValidationResponse(ctx *context.T, id int32, data string) {
	c.Lock()
	server, ok := c.flowMap[id].(*server.Server)
	c.Unlock()
	if !ok {
		ctx.Errorf("unexpected result from JavaScript. No server found matching id %d.", id)
		return // ignore unknown server
	}
	server.HandleCaveatValidationResponse(ctx, id, data)
}

// HandleVeyronRequest starts a vanadium rpc and returns before the rpc has been completed.
func (c *Controller) HandleVeyronRequest(ctx *context.T, id int32, data string, w lib.ClientWriter) {
	binBytes, err := hex.DecodeString(data)
	if err != nil {
		w.Error(verror.Convert(verror.ErrInternal, ctx, fmt.Errorf("Error decoding hex string %q: %v", data, err)))
		return
	}
	decoder := vom.NewDecoderWithTypeDecoder(bytes.NewReader(binBytes), c.typeDecoder)
	if err != nil {
		w.Error(verror.Convert(verror.ErrInternal, ctx, fmt.Errorf("Error decoding hex string %q: %v", data, err)))
		return
	}
	var msg RpcRequest
	if err := decoder.Decode(&msg); err != nil {
		w.Error(verror.Convert(verror.ErrInternal, ctx, err))
		return
	}
	ctx.VI(2).Infof("Rpc: %s.%s(..., streaming=%v)", msg.Name, msg.Method, msg.IsStreaming)
	spanName := fmt.Sprintf("<wspr>%q.%s", msg.Name, msg.Method)
	ctx, span := vtrace.WithContinuedTrace(ctx, spanName, msg.TraceRequest)
	ctx = i18n.WithLangID(ctx, i18n.LangID(msg.Context.Language))

	var cctx *context.T
	var cancel context.CancelFunc

	// TODO(mattr): To be consistent with go, we should not ignore 0 timeouts.
	// However as a rollout strategy we must, otherwise there is a circular
	// dependency between the WSPR change and the JS change that will follow.
	if msg.Deadline.IsZero() {
		cctx, cancel = context.WithCancel(ctx)
	} else {
		cctx, cancel = context.WithDeadline(ctx, msg.Deadline.Time)
	}

	// If this message is for an internal service, do a short-circuit dispatch here.
	if invoker, ok := c.reservedServices[msg.Name]; ok {
		go c.handleInternalCall(ctx, invoker, &msg, w, span, decoder)
		return
	}

	inArgs := make([]interface{}, msg.NumInArgs)
	for i := range inArgs {
		var v *vdl.Value
		if err := decoder.Decode(&v); err != nil {
			w.Error(err)
			return
		}
		inArgs[i] = v
	}

	request := &outstandingRequest{
		cancel: cancel,
	}
	if msg.IsStreaming {
		// If this rpc is streaming, we would expect that the client would try to send
		// on this stream.  Since the initial handshake is done asynchronously, we have
		// to put the outstanding stream in the map before we make the async call so that
		// the future send know which queue to write to, even if the client call isn't
		// actually ready yet.
		request.stream = newStream(c.typeDecoder)
	}
	c.Lock()
	c.outstandingRequests[id] = request
	go c.sendVeyronRequest(cctx, id, &msg, inArgs, w, request.stream, span)
	c.Unlock()
}

// HandleVeyronCancellation cancels the request corresponding to the
// given id if it is still outstanding.
func (c *Controller) HandleVeyronCancellation(ctx *context.T, id int32) {
	c.Lock()
	defer c.Unlock()
	if request, ok := c.outstandingRequests[id]; ok && request.cancel != nil {
		request.cancel()
	}
}

// CloseStream closes the stream for a given id.
func (c *Controller) CloseStream(ctx *context.T, id int32) {
	c.Lock()
	defer c.Unlock()
	if request, ok := c.outstandingRequests[id]; ok && request.stream != nil {
		request.stream.end()
		return
	}
	ctx.Errorf("close called on non-existent call: %v", id)
}

// HandleLookupResponse handles the result of a Dispatcher.Lookup call that was
// run by the Javascript server.
func (c *Controller) HandleLookupResponse(ctx *context.T, id int32, data string) {
	c.Lock()
	server, ok := c.flowMap[id].(*server.Server)
	c.Unlock()
	if !ok {
		ctx.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	server.HandleLookupResponse(ctx, id, data)
}

// HandleAuthResponse handles the result of a Authorizer.Authorize call that was
// run by the Javascript server.
func (c *Controller) HandleAuthResponse(ctx *context.T, id int32, data string) {
	c.Lock()
	server, ok := c.flowMap[id].(*server.Server)
	c.Unlock()
	if !ok {
		ctx.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	server.HandleAuthResponse(ctx, id, data)
}

// NewServer instructs WSPR to create a server and start listening for calls on
// behalf of a JavaScript server.
func (c *Controller) NewServer(ctx *context.T, _ rpc.ServerCall, name string, serverId uint32, rpcServerOpts []RpcServerOption) error {
	opts, err := c.serverOpts(rpcServerOpts)
	if err != nil {
		return verror.Convert(verror.ErrInternal, nil, err)
	}

	s, err := server.NewServer(serverId, name, c.listenSpec, c, opts...)
	if err != nil {
		return verror.Convert(verror.ErrInternal, nil, err)
	}

	c.Lock()
	c.servers[serverId] = s
	c.Unlock()

	ctx.VI(2).Infof("server created under name: %q", name)

	return nil
}

// Stop instructs WSPR to stop listening for calls for the
// given javascript server.
func (c *Controller) Stop(_ *context.T, _ rpc.ServerCall, serverId uint32) error {
	c.Lock()
	server, ok := c.servers[serverId]
	if !ok {
		c.Unlock()
		return nil
	}
	delete(c.servers, serverId)
	c.Unlock()

	server.Stop()
	return nil
}

// AddName adds a published name to an existing server.
func (c *Controller) AddName(_ *context.T, _ rpc.ServerCall, serverId uint32, name string) error {
	server, err := c.getServerById(serverId)
	if err != nil {
		return err
	}

	// Add name
	if err := server.AddName(name); err != nil {
		return verror.Convert(verror.ErrInternal, nil, err)
	}
	return nil
}

// RemoveName removes a published name from an existing server.
func (c *Controller) RemoveName(_ *context.T, _ rpc.ServerCall, serverId uint32, name string) error {
	server, err := c.getServerById(serverId)
	if err != nil {
		return err
	}

	// Remove name
	server.RemoveName(name)
	// Remove name from signature cache as well
	c.signatureManager.FlushCacheEntry(name)
	return nil
}

// getServerById return a server for the given id or an error if id does not match any servers.
func (c *Controller) getServerById(serverId uint32) (*server.Server, error) {
	c.Lock()
	defer c.Unlock()
	server, ok := c.servers[serverId]
	if !ok {
		return nil, verror.Convert(verror.ErrInternal, nil, fmt.Errorf("Trying to getServerById, but id %d does not match any servers.", serverId))
	}

	return server, nil
}

// HandleServerResponse handles the completion of outstanding calls to JavaScript services
// by filling the corresponding channel with the result from JavaScript.
func (c *Controller) HandleServerResponse(ctx *context.T, id int32, data string) {
	c.Lock()
	server, ok := c.flowMap[id].(*server.Server)
	c.Unlock()
	if !ok {
		ctx.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	server.HandleServerResponse(ctx, id, data)
}

// getSignature uses the signature manager to get and cache the signature of a remote server.
func (c *Controller) getSignature(ctx *context.T, name string) ([]signature.Interface, error) {
	return c.signatureManager.Signature(ctx, name)
}

// Signature uses the signature manager to get and cache the signature of a remote server.
func (c *Controller) Signature(ctx *context.T, _ rpc.ServerCall, name string) ([]signature.Interface, error) {
	return c.getSignature(ctx, name)
}

// Bless binds extensions of blessings held by this principal to
// another principal (represented by its public key).
func (c *Controller) Bless(_ *context.T, _ rpc.ServerCall, publicKey []byte, inputBlessings security.Blessings, extension string, caveats []security.Caveat) (principal.BlessingsId, error) {
	key, err := security.UnmarshalPublicKey(publicKey)
	if err != nil {
		return 0, err
	}

	if len(caveats) == 0 {
		caveats = append(caveats, security.UnconstrainedUse())
	}

	p := v23.GetPrincipal(c.ctx)
	blessings, err := p.Bless(key, inputBlessings, extension, caveats[0], caveats[1:]...)
	if err != nil {
		return 0, err
	}
	return c.blessingsCache.Put(blessings), nil
}

// BlessSelf creates a blessing with the provided name for this principal.
func (c *Controller) BlessSelf(_ *context.T, _ rpc.ServerCall, extension string, caveats []security.Caveat) (principal.BlessingsId, error) {
	p := v23.GetPrincipal(c.ctx)
	blessings, err := p.BlessSelf(extension, caveats...)
	if err != nil {
		return 0, verror.Convert(verror.ErrInternal, nil, err)
	}

	return c.blessingsCache.Put(blessings), err
}

// BlessingStoreSet puts the specified blessing in the blessing store under the
// provided pattern.
func (c *Controller) BlessingStoreSet(_ *context.T, _ rpc.ServerCall, inputBlessings security.Blessings, pattern security.BlessingPattern) (principal.BlessingsId, error) {
	p := v23.GetPrincipal(c.ctx)
	outBlessings, err := p.BlessingStore().Set(inputBlessings, security.BlessingPattern(pattern))
	if err != nil {
		return 0, err
	}

	if outBlessings.IsZero() {
		return 0, nil
	}

	return c.blessingsCache.Put(outBlessings), nil
}

// BlessingStoreForPeer puts the specified blessing in the blessing store under the
// provided pattern.
func (c *Controller) BlessingStoreForPeer(_ *context.T, _ rpc.ServerCall, peerBlessings []string) (principal.BlessingsId, error) {
	p := v23.GetPrincipal(c.ctx)
	outBlessings := p.BlessingStore().ForPeer(peerBlessings...)

	if outBlessings.IsZero() {
		return 0, nil
	}

	return c.blessingsCache.Put(outBlessings), nil
}

// BlessingStoreSetDefault sets the default blessings in the blessing store.
func (c *Controller) BlessingStoreSetDefault(_ *context.T, _ rpc.ServerCall, inputBlessings security.Blessings) error {
	p := v23.GetPrincipal(c.ctx)
	return p.BlessingStore().SetDefault(inputBlessings)
}

// BlessingStoreDefault fetches the default blessings for the principal of the controller.
func (c *Controller) BlessingStoreDefault(*context.T, rpc.ServerCall) (principal.BlessingsId, error) {
	p := v23.GetPrincipal(c.ctx)
	outBlessings := p.BlessingStore().Default()

	if outBlessings.IsZero() {
		return 0, nil
	}

	return c.blessingsCache.Put(outBlessings), nil
}

// BlessingStorePublicKey fetches the public key used by the principal associated with the blessing store.
func (c *Controller) BlessingStorePublicKey(*context.T, rpc.ServerCall) ([]byte, error) {
	p := v23.GetPrincipal(c.ctx)
	return p.BlessingStore().PublicKey().MarshalBinary()
}

// BlessingStorePeerBlessings returns all the blessings that the BlessingStore holds.
func (c *Controller) BlessingStorePeerBlessings(*context.T, rpc.ServerCall) (map[security.BlessingPattern]principal.BlessingsId, error) {
	p := v23.GetPrincipal(c.ctx)
	outBlessingsMap := map[security.BlessingPattern]principal.BlessingsId{}
	for pattern, blessings := range p.BlessingStore().PeerBlessings() {
		outBlessingsMap[pattern] = c.blessingsCache.Put(blessings)
	}
	return outBlessingsMap, nil
}

// BlessingStoreDebugString retrieves a debug string describing the state of the blessing store
func (c *Controller) BlessingStoreDebugString(*context.T, rpc.ServerCall) (string, error) {
	p := v23.GetPrincipal(c.ctx)
	ds := p.BlessingStore().DebugString()
	return ds, nil
}

// AddToRoots adds the provided blessing as a root.
func (c *Controller) AddToRoots(_ *context.T, _ rpc.ServerCall, inputBlessings security.Blessings) error {
	return security.AddToRoots(v23.GetPrincipal(c.ctx), inputBlessings)
}

// HandleGranterResponse handles the result of a Granter request.
func (c *Controller) HandleGranterResponse(ctx *context.T, id int32, data string) {
	c.Lock()
	granterStr, ok := c.flowMap[id].(*granterStream)
	c.Unlock()
	if !ok {
		ctx.Errorf("unexpected result from JavaScript. Flow was not a granter "+
			"stream for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	granterStr.Send(data)
}

func (c *Controller) HandleTypeMessage(ctx *context.T, data string) {
	c.typeReader.Add(data)
}

func (c *Controller) RemoteBlessings(ctx *context.T, _ rpc.ServerCall, name, method string) ([]string, error) {
	ctx.VI(2).Infof("requesting remote blessings for %q", name)

	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	clientCall, err := v23.GetClient(cctx).StartCall(cctx, name, method, nil)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, cctx, err)
	}

	blessings, _ := clientCall.RemoteBlessings()
	return blessings, nil
}

func (c *Controller) SendLogMessage(level lib.LogLevel, msg string) error {
	c.Lock()
	defer c.Unlock()
	id := c.lastGeneratedId
	c.lastGeneratedId += 2
	return c.writerCreator(id).Send(lib.ResponseLog, lib.LogMessage{
		Level:   level,
		Message: msg,
	})
}

func (c *Controller) SendBlessingsCacheMessages(messages []principal.BlessingsCacheMessage) {
	c.Lock()
	defer c.Unlock()
	id := c.lastGeneratedId
	c.lastGeneratedId += 2
	if err := c.writerCreator(id).Send(lib.ResponseBlessingsCacheMessage, messages); err != nil {
		c.ctx.Errorf("unexpected error sending blessings cache message: %v", err)
	}
}

func (c *Controller) TypeEncoder() *vom.TypeEncoder {
	return c.typeEncoder
}

func (c *Controller) TypeDecoder() *vom.TypeDecoder {
	return c.typeDecoder
}
