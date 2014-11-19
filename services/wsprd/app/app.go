// The app package contains the struct that keeps per javascript app state and handles translating
// javascript requests to veyron requests and vice versa.
package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sync"
	"time"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
	vom_wiretype "veyron.io/veyron/veyron2/vom/wiretype"
	"veyron.io/veyron/veyron2/vom2"
	wiretype_build "veyron.io/veyron/veyron2/wiretype/build"
	"veyron.io/wspr/veyron/services/wsprd/ipc/server"
	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/namespace"
	"veyron.io/wspr/veyron/services/wsprd/principal"
	"veyron.io/wspr/veyron/services/wsprd/signature"
)

// pkgPath is the prefix os errors in this package.
const pkgPath = "veyron.io/veyron/veyron/services/wsprd/app"

// Errors
var (
	marshallingError       = verror2.Register(pkgPath+".marshallingError", verror2.NoRetry, "{1} {2} marshalling error {_}")
	noResults              = verror2.Register(pkgPath+".noResults", verror2.NoRetry, "{1} {2} no results from call {_}")
	signatureError         = verror2.Register(pkgPath+".signatureError", verror2.NoRetry, "{1} {2} signature error {_}")
	badCaveatType          = verror2.Register(pkgPath+".badCaveatType", verror2.NoRetry, "{1} {2} bad caveat type {_}")
	unknownBlessings       = verror2.Register(pkgPath+".unknownBlessings", verror2.NoRetry, "{1} {2} unknown public id {_}")
	invalidBlessingsHandle = verror2.Register(pkgPath+".invalidBlessingsHandle", verror2.NoRetry, "{1} {2} invalid blessings handle {_}")
)

// TODO(bjornick,nlacasse): Remove the retryTimeout flag once we able
// to pass it in from javascript. For now all RPCs have the same
// retryTimeout, set by command line flag.
var retryTimeout *int

func init() {
	// TODO(bjornick,nlacasse): Remove the retryTimeout flag once we able
	// to pass it in from javascript. For now all RPCs have the same
	// retryTimeout, set by command line flag.
	retryTimeout = flag.Int("retry-timeout", 0, "Duration in seconds to retry starting an RPC call. 0 means never retry.")
}

// Temporary holder of RPC so that we can store the unprocessed args.
type veyronTempRPC struct {
	Name        string
	Method      string
	InArgs      []json.RawMessage
	NumOutArgs  int32
	IsStreaming bool
	Timeout     int64
}

type veyronRPC struct {
	Name        string
	Method      string
	InArgs      []interface{}
	NumOutArgs  int32
	IsStreaming bool
}

type serveRequest struct {
	Name     string
	ServerId uint64
}

type addRemoveNameRequest struct {
	Name     string
	ServerId uint64
}

type jsonCaveatValidator struct {
	Type string `json:"_type"`
	Data json.RawMessage
}

type blessingRequest struct {
	Handle     int64
	Caveats    []jsonCaveatValidator
	DurationMs int64
	Extension  string
}

type outstandingRequest struct {
	stream *outstandingStream
	cancel context.CancelFunc
}

// Controller represents all the state of a Veyron Web App.  This is the struct
// that is in charge performing all the veyron options.
type Controller struct {
	// Protects everything.
	// TODO(bjornick): We need to split this up.
	sync.Mutex

	logger vlog.Logger

	// The runtime to use to create new clients.
	rt veyron2.Runtime

	// The ipc.ListenSpec to use with server.Listen
	listenSpec *ipc.ListenSpec

	// Used to generate unique ids for requests initiated by the proxy.
	// These ids will be even so they don't collide with the ids generated
	// by the client.
	lastGeneratedId int64

	// Used to keep track of data (streams and cancellation functions) for
	// outstanding requests.
	outstandingRequests map[int64]*outstandingRequest

	// Maps flowids to the server that owns them.
	flowMap map[int64]*server.Server

	// A manager that Handles fetching and caching signature of remote services
	signatureManager lib.SignatureManager

	// We maintain multiple Veyron server per websocket pipe for serving JavaScript
	// services.
	servers map[uint64]*server.Server

	// Creates a client writer for a given flow.  This is a member so that tests can override
	// the default implementation.
	writerCreator func(id int64) lib.ClientWriter

	// There is only one client per Controller since there is a single principal per app.
	client ipc.Client

	veyronProxyEP string

	// Store for all the Blessings that javascript has a handle to.
	blessingsStore *principal.JSBlessingsHandles
}

// NewController creates a new Controller.  writerCreator will be used to create a new flow for rpcs to
// javascript server. veyronProxyEP is an endpoint for the veyron proxy to serve through.  It can't be empty.
// opts are any options that should be passed to the rt.New().
func NewController(writerCreator func(id int64) lib.ClientWriter, profile veyron2.Profile, listenSpec *ipc.ListenSpec, namespaceRoots []string, opts ...veyron2.ROpt) (*Controller, error) {
	if profile != nil {
		opts = append(opts, options.Profile{profile})
	}
	r, err := rt.New(opts...)
	if err != nil {
		return nil, err
	}
	if namespaceRoots != nil {
		r.Namespace().SetRoots(namespaceRoots...)
	}
	client, err := r.NewClient()
	if err != nil {
		return nil, err
	}

	controller := &Controller{
		rt:             r,
		logger:         r.Logger(),
		client:         client,
		writerCreator:  writerCreator,
		listenSpec:     listenSpec,
		blessingsStore: principal.NewJSBlessingsHandles(),
	}

	controller.setup()
	return controller, nil
}

// finishCall waits for the call to finish and write out the response to w.
func (c *Controller) finishCall(ctx context.T, w lib.ClientWriter, clientCall ipc.Call, msg *veyronRPC) {
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

			var buf bytes.Buffer
			encoder, err := vom2.NewBinaryEncoder(&buf)
			if err != nil {
				w.Error(verror2.Make(marshallingError, ctx, item))
				continue
			}

			if err := encoder.Encode(item); err != nil {
				w.Error(verror2.Make(marshallingError, ctx, item))
				continue
			}

			if err := w.Send(lib.ResponseStream, hex.EncodeToString(buf.Bytes())); err != nil {
				w.Error(verror2.Make(marshallingError, ctx, item))
			}
		}

		if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
			w.Error(verror2.Make(marshallingError, ctx, "ResponseStreamClose"))
		}
	}

	results := make([]interface{}, msg.NumOutArgs)
	// This array will have pointers to the values in result.
	resultptrs := make([]interface{}, msg.NumOutArgs)
	for ax := range results {
		resultptrs[ax] = &results[ax]
	}
	if err := clientCall.Finish(resultptrs...); err != nil {
		// return the call system error as is
		w.Error(err)
		return
	}
	// for now we assume last out argument is always error
	if len(results) < 1 {
		w.Error(verror2.Make(noResults, ctx))
		return
	}

	if err, ok := results[len(results)-1].(error); ok {
		// return the call Application error as is
		w.Error(err)
		return
	}

	var buf bytes.Buffer
	encoder, err := vom2.NewBinaryEncoder(&buf)
	if err != nil {
		w.Error(err)
		return
	}

	if err := encoder.Encode(results[:len(results)-1]); err != nil {
		w.Error(err)
		return
	}

	if err := w.Send(lib.ResponseFinal, hex.EncodeToString(buf.Bytes())); err != nil {
		w.Error(verror2.Convert(marshallingError, ctx, err))
	}
}

func (c *Controller) startCall(ctx context.T, w lib.ClientWriter, msg *veyronRPC) (ipc.Call, error) {
	if c.client == nil {
		return nil, verror2.Make(verror2.BadArg, ctx, "app.Controller.client")
	}
	methodName := lib.UppercaseFirstCharacter(msg.Method)
	retryTimeoutOpt := options.RetryTimeout(time.Duration(*retryTimeout) * time.Second)
	clientCall, err := c.client.StartCall(ctx, msg.Name, methodName, msg.InArgs, retryTimeoutOpt)
	if err != nil {
		return nil, fmt.Errorf("error starting call (name: %v, method: %v, args: %v): %v", msg.Name, methodName, msg.InArgs, err)
	}

	return clientCall, nil
}

// Implements the serverHelper interface

// CreateNewFlow creats a new server flow that will be used to write out
// streaming messages to Javascript.
func (c *Controller) CreateNewFlow(s *server.Server, stream ipc.Stream) *server.Flow {
	c.Lock()
	defer c.Unlock()
	id := c.lastGeneratedId
	c.lastGeneratedId += 2
	c.flowMap[id] = s
	os := newStream()
	os.init(stream, vom_wiretype.Type{ID: 1})
	c.outstandingRequests[id] = &outstandingRequest{
		stream: os,
	}
	return &server.Flow{ID: id, Writer: c.writerCreator(id)}
}

// CleanupFlow removes the bookkeping for a previously created flow.
func (c *Controller) CleanupFlow(id int64) {
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

// GetLogger returns a Veyron logger to use.
func (c *Controller) GetLogger() vlog.Logger {
	return c.logger
}

// RT returns the runtime of the app.
func (c *Controller) RT() veyron2.Runtime {
	return c.rt
}

// AddBlessings adds the Blessings to the local blessings store and returns
// the handle to it.  This function exists because JS only has
// a handle to the blessings to avoid shipping the certificate forest
// to JS and back.
func (c *Controller) AddBlessings(blessings security.Blessings) int64 {
	return c.blessingsStore.Add(blessings)
}

// Cleanup cleans up any outstanding rpcs.
func (c *Controller) Cleanup() {
	c.logger.VI(0).Info("Cleaning up websocket")
	c.Lock()
	defer c.Unlock()

	for _, request := range c.outstandingRequests {
		if request.cancel != nil {
			request.cancel()
		}
		if request.stream != nil {
			request.stream.end()
		}
	}

	for _, server := range c.servers {
		server.Stop()
	}

	c.RT().Cleanup()
}

func (c *Controller) setup() {
	c.signatureManager = lib.NewSignatureManager()
	c.outstandingRequests = make(map[int64]*outstandingRequest)
	c.flowMap = make(map[int64]*server.Server)
	c.servers = make(map[uint64]*server.Server)
}

// SendOnStream writes data on id's stream.  The actual network write will be
// done asynchronously.  If there is an error, it will be sent to w.
func (c *Controller) SendOnStream(id int64, data string, w lib.ClientWriter) {
	c.Lock()
	request := c.outstandingRequests[id]
	if request == nil || request.stream == nil {
		vlog.Errorf("unknown stream: %d", id)
		return
	}
	stream := request.stream
	c.Unlock()
	stream.send(data, w)
}

// SendVeyronRequest makes a veyron request for the given flowId.  If signal is non-nil, it will receive
// the call object after it has been constructed.
func (c *Controller) sendVeyronRequest(ctx context.T, id int64, tempMsg *veyronTempRPC, w lib.ClientWriter, stream *outstandingStream) {
	// Fetch and adapt signature from the SignatureManager
	retryTimeoutOpt := options.RetryTimeout(time.Duration(*retryTimeout) * time.Second)
	sig, err := c.signatureManager.Signature(ctx, tempMsg.Name, c.client, retryTimeoutOpt)
	if err != nil {
		w.Error(verror2.Make(signatureError, ctx, tempMsg.Name, err))
		return
	}
	methName := lib.UppercaseFirstCharacter(tempMsg.Method)
	methSig, ok := sig.Methods[methName]
	if !ok {
		w.Error(fmt.Errorf("method not found in signature: %v (full sig: %v)", methName, sig))
		return
	}

	var msg veyronRPC
	if len(methSig.InArgs) != len(tempMsg.InArgs) {
		w.Error(fmt.Errorf("invalid number of arguments, expected: %v, got:%v", methSig, tempMsg))
		return
	}
	msg.InArgs = make([]interface{}, len(tempMsg.InArgs))
	td := wiretype_build.TypeDefs(sig.TypeDefs)

	for i := 0; i < len(tempMsg.InArgs); i++ {
		argTypeId := methSig.InArgs[i].Type
		argType := vom_wiretype.Type{
			ID:   argTypeId,
			Defs: &td,
		}

		val, err := vom.JSONToObject(string(tempMsg.InArgs[i]), argType)
		if err != nil {
			w.Error(fmt.Errorf("error while converting json to object for arg %d (%s): %v", i, methSig.InArgs[i].Name, err))
			return
		}
		msg.InArgs[i] = val
	}

	msg.Name = tempMsg.Name
	msg.Method = tempMsg.Method
	msg.NumOutArgs = tempMsg.NumOutArgs
	msg.IsStreaming = tempMsg.IsStreaming

	inStreamType := vom_wiretype.Type{
		ID:   methSig.InStream,
		Defs: &td,
	}

	// We have to make the start call synchronous so we can make sure that we populate
	// the call map before we can Handle a recieve call.
	call, err := c.startCall(ctx, w, &msg)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	if stream != nil {
		stream.init(call, inStreamType)
	}

	c.finishCall(ctx, w, call, &msg)
	c.Lock()
	if request, ok := c.outstandingRequests[id]; ok {
		delete(c.outstandingRequests, id)
		if request.cancel != nil {
			request.cancel()
		}
	}
	c.Unlock()
}

// HandleVeyronRequest starts a veyron rpc and returns before the rpc has been completed.
func (c *Controller) HandleVeyronRequest(ctx context.T, id int64, data string, w lib.ClientWriter) {
	veyronTempMsg, err := c.parseVeyronRequest(ctx, bytes.NewBufferString(data))
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	var cctx context.T
	var cancel context.CancelFunc

	// TODO(mattr): To be consistent with go, we should not ignore 0 timeouts.
	// However as a rollout strategy we must, otherwise there is a circular
	// dependency between the WSPR change and the JS change that will follow.
	if veyronTempMsg.Timeout == lib.JSIPCNoTimeout || veyronTempMsg.Timeout == 0 {
		cctx, cancel = ctx.WithCancel()
	} else {
		cctx, cancel = ctx.WithTimeout(lib.JSToGoDuration(veyronTempMsg.Timeout))
	}

	request := &outstandingRequest{
		cancel: cancel,
	}
	if veyronTempMsg.IsStreaming {
		// If this rpc is streaming, we would expect that the client would try to send
		// on this stream.  Since the initial handshake is done asynchronously, we have
		// to put the outstanding stream in the map before we make the async call so that
		// the future send know which queue to write to, even if the client call isn't
		// actually ready yet.
		request.stream = newStream()
	}
	c.Lock()
	c.outstandingRequests[id] = request
	go c.sendVeyronRequest(cctx, id, veyronTempMsg, w, request.stream)
	c.Unlock()
}

// HandleVeyronCancellation cancels the request corresponding to the
// given id if it is still outstanding.
func (c *Controller) HandleVeyronCancellation(id int64) {
	c.Lock()
	defer c.Unlock()
	if request, ok := c.outstandingRequests[id]; ok && request.cancel != nil {
		request.cancel()
	}
}

// CloseStream closes the stream for a given id.
func (c *Controller) CloseStream(id int64) {
	c.Lock()
	defer c.Unlock()
	if request, ok := c.outstandingRequests[id]; ok && request.stream != nil {
		request.stream.end()
		return
	}
	c.logger.Errorf("close called on non-existent call: %v", id)
}

func (c *Controller) maybeCreateServer(serverId uint64) (*server.Server, error) {
	c.Lock()
	defer c.Unlock()
	if server, ok := c.servers[serverId]; ok {
		return server, nil
	}
	server, err := server.NewServer(serverId, c.listenSpec, c)
	if err != nil {
		return nil, err
	}
	c.servers[serverId] = server
	return server, nil
}

func (c *Controller) removeServer(serverId uint64) {
	c.Lock()
	server := c.servers[serverId]
	if server == nil {
		c.Unlock()
		return
	}
	delete(c.servers, serverId)
	c.Unlock()

	server.Stop()
}

func (c *Controller) serve(serveRequest serveRequest, w lib.ClientWriter) {
	// Create a server for the websocket pipe, if it does not exist already
	server, err := c.maybeCreateServer(serveRequest.ServerId)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
	}

	c.logger.VI(2).Infof("serving under name: %q", serveRequest.Name)

	endpoint, err := server.Serve(serveRequest.Name)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
	// Send the endpoint back
	if err := w.Send(lib.ResponseFinal, endpoint); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
}

// HandleServeRequest takes a request to serve a server, creates
// a server, registers the provided services and sends the endpoint back.
func (c *Controller) HandleServeRequest(data string, w lib.ClientWriter) {
	// Decode the serve request which includes IDL, registered services and name
	var serveRequest serveRequest
	if err := json.Unmarshal([]byte(data), &serveRequest); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
	c.serve(serveRequest, w)
}

// HandleLookupResponse handles the result of a Dispatcher.Lookup call that was
// run by the Javascript server.
func (c *Controller) HandleLookupResponse(id int64, data string) {
	c.Lock()
	server := c.flowMap[id]
	c.Unlock()
	if server == nil {
		c.logger.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	server.HandleLookupResponse(id, data)
}

// HandleAuthResponse handles the result of a Authorizer.Authorize call that was
// run by the Javascript server.
func (c *Controller) HandleAuthResponse(id int64, data string) {
	c.Lock()
	server := c.flowMap[id]
	c.Unlock()
	if server == nil {
		c.logger.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	server.HandleAuthResponse(id, data)
}

// HandleStopRequest takes a request to stop a server.
func (c *Controller) HandleStopRequest(data string, w lib.ClientWriter) {
	var serverId uint64
	if err := json.Unmarshal([]byte(data), &serverId); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	c.removeServer(serverId)

	// Send true to indicate stop has finished
	if err := w.Send(lib.ResponseFinal, true); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
}

// HandleAddNameRequest takes a request to add a new name to a server
func (c *Controller) HandleAddNameRequest(data string, w lib.ClientWriter) {
	var request addRemoveNameRequest
	if err := json.Unmarshal([]byte(data), &request); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	// Create a server for the websocket pipe, if it does not exist already
	server, err := c.maybeCreateServer(request.ServerId)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	// Add name
	if err := server.AddName(request.Name); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	// Send true to indicate request has finished without error
	if err := w.Send(lib.ResponseFinal, true); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
}

// HandleRemoveNameRequest takes a request to remove a name from a server
func (c *Controller) HandleRemoveNameRequest(data string, w lib.ClientWriter) {
	var request addRemoveNameRequest
	if err := json.Unmarshal([]byte(data), &request); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	// Create a server for the websocket pipe, if it does not exist already
	server, err := c.maybeCreateServer(request.ServerId)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	// Remove name
	if err := server.RemoveName(request.Name); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	// Remove name from signature cache as well
	c.signatureManager.FlushCacheEntry(request.Name)

	// Send true to indicate request has finished without error
	if err := w.Send(lib.ResponseFinal, true); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
}

// HandleServerResponse handles the completion of outstanding calls to JavaScript services
// by filling the corresponding channel with the result from JavaScript.
func (c *Controller) HandleServerResponse(id int64, data string) {
	c.Lock()
	server := c.flowMap[id]
	c.Unlock()
	if server == nil {
		c.logger.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	server.HandleServerResponse(id, data)
}

// parseVeyronRequest parses a json rpc request into a veyronRPC object.
func (c *Controller) parseVeyronRequest(ctx context.T, r io.Reader) (*veyronTempRPC, error) {
	var tempMsg veyronTempRPC
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&tempMsg); err != nil {
		return nil, fmt.Errorf("can't unmarshall JSONMessage: %v", err)
	}
	c.logger.VI(2).Infof("VeyronRPC: %s.%s(..., streaming=%v)", tempMsg.Name, tempMsg.Method, tempMsg.IsStreaming)
	return &tempMsg, nil
}

type signatureRequest struct {
	Name string
}

func (c *Controller) getSignature(ctx context.T, name string) (signature.JSONServiceSignature, error) {
	// Fetch and adapt signature from the SignatureManager
	retryTimeoutOpt := options.RetryTimeout(time.Duration(*retryTimeout) * time.Second)
	sig, err := c.signatureManager.Signature(ctx, name, c.client, retryTimeoutOpt)
	if err != nil {
		return nil, verror2.Convert(verror2.Internal, ctx, err)
	}

	return signature.NewJSONServiceSignature(*sig), nil
}

// HandleSignatureRequest uses signature manager to get and cache signature of a remote server
func (c *Controller) HandleSignatureRequest(ctx context.T, data string, w lib.ClientWriter) {
	// Decode the request
	var request signatureRequest
	if err := json.Unmarshal([]byte(data), &request); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	c.logger.VI(2).Infof("requesting Signature for %q", request.Name)
	jsSig, err := c.getSignature(ctx, request.Name)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	var buf bytes.Buffer
	encoder, err := vom2.NewBinaryEncoder(&buf)
	if err != nil {
		w.Error(err)
		return
	}

	if err := encoder.Encode(jsSig); err != nil {
		w.Error(err)
		return
	}

	// Send the signature back
	if err := w.Send(lib.ResponseFinal, hex.EncodeToString(buf.Bytes())); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}
}

// HandleUnlinkJSBlessings removes the specified blessings from the JS blessings
// store.  'data' should be a JSON encoded number (representing the blessings handle).
func (c *Controller) HandleUnlinkJSBlessings(data string, w lib.ClientWriter) {
	var handle int64
	if err := json.Unmarshal([]byte(data), &handle); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
	c.blessingsStore.Remove(handle)
}

// Convert the json wire format of a caveat into the right go object
func decodeCaveat(c jsonCaveatValidator) (security.Caveat, error) {
	var failed security.Caveat
	switch c.Type {
	case "MethodCaveat":
		var methods []string
		if err := json.Unmarshal(c.Data, &methods); err != nil {
			return failed, err
		}
		if len(methods) == 0 {
			return failed, fmt.Errorf("must provide at least one method")
		}
		return security.MethodCaveat(methods[0], methods[1:]...)
	default:
		return failed, verror2.Make(badCaveatType, nil, c.Type)
	}
}

func (c *Controller) getBlessingsHandle(handle int64) (*principal.BlessingsHandle, error) {
	id := c.blessingsStore.Get(handle)
	if id == nil {
		return nil, verror2.Make(unknownBlessings, nil)
	}
	return principal.ConvertBlessingsToHandle(id, handle), nil
}

func (c *Controller) blessPublicKey(request blessingRequest) (*principal.BlessingsHandle, error) {
	var blessee security.Blessings
	if blessee = c.blessingsStore.Get(request.Handle); blessee == nil {
		return nil, verror2.Make(invalidBlessingsHandle, nil)
	}

	expiryCav, err := security.ExpiryCaveat(time.Now().Add(time.Duration(request.DurationMs) * time.Millisecond))
	if err != nil {
		return nil, err
	}
	caveats := []security.Caveat{expiryCav}
	for _, c := range request.Caveats {
		cav, err := decodeCaveat(c)
		if err != nil {
			return nil, verror2.Convert(verror2.BadArg, nil, err)
		}
		caveats = append(caveats, cav)
	}

	// TODO(ataly, ashankar, bjornick): Currently the Bless operation is carried
	// out using the Default blessing in this principal's blessings store. We
	// should change this so that the JS blessing request can also specify the
	// blessing to be used for the Bless operation.
	blessings, err := c.rt.Principal().Bless(blessee.PublicKey(), c.rt.Principal().BlessingStore().Default(), request.Extension, caveats[0], caveats[1:]...)
	if err != nil {
		return nil, err
	}

	return principal.ConvertBlessingsToHandle(blessings, c.blessingsStore.Add(blessings)), nil
}

// HandleBlessPublicKey handles a blessing request from JS.
func (c *Controller) HandleBlessPublicKey(data string, w lib.ClientWriter) {
	var request blessingRequest
	if err := json.Unmarshal([]byte(data), &request); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	handle, err := c.blessPublicKey(request)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	// Send the id back.
	if err := w.Send(lib.ResponseFinal, handle); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
}

func (c *Controller) HandleCreateBlessings(data string, w lib.ClientWriter) {
	var extension string
	if err := json.Unmarshal([]byte(data), &extension); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}

	blessings, err := p.BlessSelf(extension)
	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
	handle := principal.ConvertBlessingsToHandle(blessings, c.blessingsStore.Add(blessings))
	if err := w.Send(lib.ResponseFinal, handle); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
}

// HandleNamespaceRequest uses the namespace client to respond to namespace specific requests such as glob
func (c *Controller) HandleNamespaceRequest(ctx context.T, data string, w lib.ClientWriter) {
	namespace.HandleRequest(ctx, c.rt, data, w)
}
