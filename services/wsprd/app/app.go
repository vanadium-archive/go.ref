// The app package contains the struct that keeps per javascript app state and handles translating
// javascript requests to veyron requests and vice versa.
package app

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sync"
	"time"

	"veyron/services/wsprd/identity"
	"veyron/services/wsprd/ipc/client"
	"veyron/services/wsprd/ipc/server"
	"veyron/services/wsprd/ipc/stream"
	"veyron/services/wsprd/lib"
	"veyron/services/wsprd/signature"
	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"
	vom_wiretype "veyron2/vom/wiretype"
	wiretype_build "veyron2/wiretype/build"
)

// TODO(bjornick,nlacasse): Remove the retryTimeout flag once we able
// to pass it in from javascript. For now all RPCs have the same
// retryTimeout, set by command line flag.
var retryTimeoutOpt ipc.CallOpt

func init() {
	// TODO(bjornick,nlacasse): Remove the retryTimeout flag once we able
	// to pass it in from javascript. For now all RPCs have the same
	// retryTimeout, set by command line flag.
	retryTimeout := flag.Int("retry-timeout", 0, "Duration in seconds to retry starting an RPC call. 0 means never retry.")
	retryTimeoutOpt = veyron2.RetryTimeoutOpt(time.Duration(*retryTimeout) * time.Second)
}

// Temporary holder of RPC so that we can store the unprocessed args.
type veyronTempRPC struct {
	Name        string
	Method      string
	InArgs      []json.RawMessage
	NumOutArgs  int32
	IsStreaming bool
}

type veyronRPC struct {
	Name        string
	Method      string
	InArgs      []interface{}
	NumOutArgs  int32
	IsStreaming bool
}

// A request javascript to serve undern a particular name
type serveRequest struct {
	Name     string
	ServerId uint64
	Service  signature.JSONServiceSignature
}

type outstandingStream struct {
	stream stream.Sender
	inType vom.Type
}

type jsonCaveatValidator struct {
	Type string `json:"_type"`
	Data json.RawMessage
}

type blessingRequest struct {
	Handle     int64
	Caveats    []jsonCaveatValidator
	DurationMs int64
	Name       string
}

// PublicIDHandle is a handle given to Javascript that is linked
// to a PublicID in go.
type PublicIDHandle struct {
	Handle int64
	Names  []string
}

// Controller represents all the state of a Veyron Web App.  This is the struct
// that is in charge performing all the veyron options.
type Controller struct {
	// Protects outstandingStreams and outstandingServerRequests.
	sync.Mutex

	logger vlog.Logger

	// The runtime to use to create new clients.
	rt veyron2.Runtime

	// Used to generate unique ids for requests initiated by the proxy.
	// These ids will be even so they don't collide with the ids generated
	// by the client.
	lastGeneratedId int64

	// Streams for the outstanding requests.
	outstandingStreams map[int64]outstandingStream

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

	// There is only one client per Controller since there is only one identity per app.
	client ipc.Client

	veyronProxyEP string

	// Store for all the PublicIDs that javascript has a handle to.
	idStore *identity.JSPublicIDHandles
}

// NewController creates a new Controller.  writerCreator will be used to create a new flow for rpcs to
// javascript server. veyronProxyEP is an endpoint for the veyron proxy to serve through.  It can't be empty.
// opts are any options that should be passed to the rt.New(), such as the mounttable root.
func NewController(writerCreator func(id int64) lib.ClientWriter,
	veyronProxyEP string, opts ...veyron2.ROpt) (*Controller, error) {
	r, err := rt.New(opts...)
	if err != nil {
		return nil, err
	}
	client, err := r.NewClient()
	if err != nil {
		return nil, err
	}

	controller := &Controller{
		rt:            r,
		logger:        r.Logger(),
		client:        client,
		writerCreator: writerCreator,
		veyronProxyEP: veyronProxyEP,
		idStore:       identity.NewJSPublicIDHandles(),
	}
	controller.setup()
	return controller, nil
}

// finishCall waits for the call to finish and write out the response to w.
func (c *Controller) finishCall(w lib.ClientWriter, clientCall ipc.Call, msg *veyronRPC) {
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
			if err := w.Send(lib.ResponseStream, item); err != nil {
				w.Error(verror.Internalf("unable to marshal: %v", item))
			}
		}

		if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
			w.Error(verror.Internalf("unable to marshal close stream message"))
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
		w.Error(verror.Internalf("client call did not return any results"))
		return
	}

	if err, ok := results[len(results)-1].(error); ok {
		// return the call Application error as is
		w.Error(err)
		return
	}

	if err := w.Send(lib.ResponseFinal, results[0:len(results)-1]); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
	}
}

func (c *Controller) startCall(ctx context.T, w lib.ClientWriter, msg *veyronRPC) (ipc.Call, error) {
	c.Lock()
	defer c.Unlock()
	if c.client == nil {
		return nil, verror.BadArgf("no client created")
	}
	methodName := lib.UppercaseFirstCharacter(msg.Method)
	clientCall, err := c.client.StartCall(ctx, msg.Name, methodName, msg.InArgs, retryTimeoutOpt)
	if err != nil {
		return nil, fmt.Errorf("error starting call (name: %v, method: %v, args: %v): %v", msg.Name, methodName, msg.InArgs, err)
	}

	return clientCall, nil
}

// Implements the serverHelper interface

// CreateNewFlow creats a new server flow that will be used to write out
// streaming messages to Javascript.
func (c *Controller) CreateNewFlow(s *server.Server, stream stream.Sender) *server.Flow {
	c.Lock()
	defer c.Unlock()
	id := c.lastGeneratedId
	c.lastGeneratedId += 2
	c.flowMap[id] = s
	c.outstandingStreams[id] = outstandingStream{stream, vom_wiretype.Type{ID: 1}}
	return &server.Flow{ID: id, Writer: c.writerCreator(id)}
}

// CleanupFlow removes the bookkeping for a previously created flow.
func (c *Controller) CleanupFlow(id int64) {
	c.Lock()
	defer c.Unlock()
	delete(c.outstandingStreams, id)
	delete(c.flowMap, id)
}

// GetLogger returns a Veyron logger to use.
func (c *Controller) GetLogger() vlog.Logger {
	return c.logger
}

// RT returns the runtime of the app.
func (c *Controller) RT() veyron2.Runtime {
	return c.rt
}

// AddIdentity adds the PublicID to the local id store and returns
// the handle to it.  This function exists because JS only has
// a handle to the PublicID to avoid shipping the blessing forest
// to JS and back.
func (c *Controller) AddIdentity(id security.PublicID) int64 {
	return c.idStore.Add(id)
}

// Cleanup cleans up any outstanding rpcs.
func (c *Controller) Cleanup() {
	c.logger.VI(0).Info("Cleaning up websocket")
	c.Lock()
	defer c.Unlock()
	for _, stream := range c.outstandingStreams {
		_ = stream
		// TODO(bjornick): this is impossible type assertion and
		// will panic at run time.
		// if call, ok := stream.stream.(ipc.Call); ok {
		// 	call.Cancel()
		// }
	}

	for _, server := range c.servers {
		server.Stop()
	}
}

func (c *Controller) setup() {
	c.signatureManager = lib.NewSignatureManager()
	c.outstandingStreams = make(map[int64]outstandingStream)
	c.flowMap = make(map[int64]*server.Server)
	c.servers = make(map[uint64]*server.Server)
}

func (c *Controller) sendParsedMessageOnStream(id int64, msg interface{}, w lib.ClientWriter) {
	c.Lock()
	defer c.Unlock()
	stream := c.outstandingStreams[id].stream
	if stream == nil {
		w.Error(fmt.Errorf("unknown stream"))
		return
	}

	stream.Send(msg, w)

}

// SendOnStream writes data on id's stream.  Returns an error if the send failed.
func (c *Controller) SendOnStream(id int64, data string, w lib.ClientWriter) {
	c.Lock()
	typ := c.outstandingStreams[id].inType
	c.Unlock()
	if typ == nil {
		vlog.Errorf("no inType for stream %d (%q)", id, data)
		return
	}
	payload, err := vom.JSONToObject(data, typ)
	if err != nil {
		vlog.Errorf("error while converting json to InStreamType (%s): %v", data, err)
		return
	}
	c.sendParsedMessageOnStream(id, payload, w)
}

// SendVeyronRequest makes a veyron request for the given flowId.  If signal is non-nil, it will receive
// the call object after it has been constructed.
func (c *Controller) sendVeyronRequest(ctx context.T, id int64, veyronMsg *veyronRPC, w lib.ClientWriter, signal chan ipc.Stream) {
	// We have to make the start call synchronous so we can make sure that we populate
	// the call map before we can Handle a recieve call.
	call, err := c.startCall(ctx, w, veyronMsg)
	if err != nil {
		w.Error(verror.Internalf("can't start Veyron Request: %v", err))
		return
	}

	if signal != nil {
		signal <- call
	}

	c.finishCall(w, call, veyronMsg)
	if signal != nil {
		c.Lock()
		delete(c.outstandingStreams, id)
		c.Unlock()
	}
}

// HandleVeyronRequest starts a veyron rpc and returns before the rpc has been completed.
func (c *Controller) HandleVeyronRequest(ctx context.T, id int64, data string, w lib.ClientWriter) {
	veyronMsg, inStreamType, err := c.parseVeyronRequest(ctx, bytes.NewBufferString(data))
	if err != nil {
		w.Error(verror.Internalf("can't parse Veyron Request: %v", err))
		return
	}

	c.Lock()
	defer c.Unlock()
	// If this rpc is streaming, we would expect that the client would try to send
	// on this stream.  Since the initial handshake is done asynchronously, we have
	// to basically put a queueing stream in the map before we make the async call
	// so that the future sends on the stream can see the queuing stream, even if
	// the client call isn't actually ready yet.
	var signal chan ipc.Stream
	if veyronMsg.IsStreaming {
		signal = make(chan ipc.Stream)
		c.outstandingStreams[id] = outstandingStream{
			stream: client.StartQueueingStream(signal),
			inType: inStreamType,
		}
	}
	go c.sendVeyronRequest(ctx, id, veyronMsg, w, signal)
}

// CloseStream closes the stream for a given id.
func (c *Controller) CloseStream(id int64) {
	c.Lock()
	defer c.Unlock()
	stream := c.outstandingStreams[id].stream
	if stream == nil {
		c.logger.Errorf("close called on non-existent call: %v", id)
		return
	}

	var call client.QueueingStream
	var ok bool
	if call, ok = stream.(client.QueueingStream); !ok {
		c.logger.Errorf("can't close server stream: %v", id)
		return
	}

	if err := call.Close(); err != nil {
		c.logger.Errorf("client call close failed with: %v", err)
	}
}

func (c *Controller) maybeCreateServer(serverId uint64) (*server.Server, error) {
	c.Lock()
	defer c.Unlock()
	if server, ok := c.servers[serverId]; ok {
		return server, nil
	}
	server, err := server.NewServer(serverId, c.veyronProxyEP, c)
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
		w.Error(verror.Internalf("error creating server: %v", err))
	}

	c.logger.VI(2).Infof("serving under name: %q", serveRequest.Name)

	endpoint, err := server.Serve(serveRequest.Name, serveRequest.Service)
	if err != nil {
		w.Error(verror.Internalf("error serving service: %v", err))
		return
	}
	// Send the endpoint back
	if err := w.Send(lib.ResponseFinal, endpoint); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
		return
	}
}

// HandleServeRequest takes a request to serve a server, creates
// a server, registers the provided services and sends the endpoint back.
func (c *Controller) HandleServeRequest(data string, w lib.ClientWriter) {
	// Decode the serve request which includes IDL, registered services and name
	var serveRequest serveRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&serveRequest); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}
	c.serve(serveRequest, w)
}

// HandleStopRequest takes a request to stop a server.
func (c *Controller) HandleStopRequest(data string, w lib.ClientWriter) {
	var serverId uint64
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&serverId); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}

	c.removeServer(serverId)

	// Send true to indicate stop has finished
	if err := w.Send(lib.ResponseFinal, true); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
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
func (c *Controller) parseVeyronRequest(ctx context.T, r io.Reader) (*veyronRPC, vom.Type, error) {
	var tempMsg veyronTempRPC
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&tempMsg); err != nil {
		return nil, nil, fmt.Errorf("can't unmarshall JSONMessage: %v", err)
	}

	// Fetch and adapt signature from the SignatureManager
	sig, err := c.signatureManager.Signature(ctx, tempMsg.Name, c.client, retryTimeoutOpt)
	if err != nil {
		return nil, nil, verror.Internalf("error getting service signature for %s: %v", tempMsg.Name, err)
	}

	methName := lib.UppercaseFirstCharacter(tempMsg.Method)
	methSig, ok := sig.Methods[methName]
	if !ok {
		return nil, nil, fmt.Errorf("Method not found in signature: %v (full sig: %v)", methName, sig)
	}

	var msg veyronRPC
	if len(methSig.InArgs) != len(tempMsg.InArgs) {
		return nil, nil, fmt.Errorf("invalid number of arguments: %v vs. %v", methSig, tempMsg)
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
			return nil, nil, fmt.Errorf("error while converting json to object for arg %d (%s): %v", i, methSig.InArgs[i].Name, err)
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

	c.logger.VI(2).Infof("VeyronRPC: %s.%s(id=%v, ..., streaming=%v)", msg.Name, msg.Method, msg.IsStreaming)
	return &msg, inStreamType, nil
}

type signatureRequest struct {
	Name string
}

func (c *Controller) getSignature(ctx context.T, name string) (signature.JSONServiceSignature, error) {
	// Fetch and adapt signature from the SignatureManager
	sig, err := c.signatureManager.Signature(ctx, name, c.client)
	if err != nil {
		return nil, verror.Internalf("error getting service signature for %s: %v", name, err)
	}

	return signature.NewJSONServiceSignature(*sig), nil
}

// HandleSignatureRequest uses signature manager to get and cache signature of a remote server
func (c *Controller) HandleSignatureRequest(ctx context.T, data string, w lib.ClientWriter) {
	// Decode the request
	var request signatureRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&request); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}

	c.logger.VI(2).Infof("requesting Signature for %q", request.Name)
	jsSig, err := c.getSignature(ctx, request.Name)
	if err != nil {
		w.Error(err)
		return
	}

	// Send the signature back
	if err := w.Send(lib.ResponseFinal, jsSig); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
		return
	}
}

// HandleUnlinkJSIdentity removes an identity from the JS identity store.
// data should be JSON encoded number
func (c *Controller) HandleUnlinkJSIdentity(data string, w lib.ClientWriter) {
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	var handle int64
	if err := decoder.Decode(&handle); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}
	c.idStore.Remove(handle)
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
	case "PeerBlessingsCaveat":
		var patterns []security.BlessingPattern
		if err := json.Unmarshal(c.Data, &patterns); err != nil {
			return failed, err
		}
		if len(patterns) == 0 {
			return failed, fmt.Errorf("must provide at least one BlessingPattern")
		}
		return security.PeerBlessingsCaveat(patterns[0], patterns[1:]...)
	default:
		return failed, verror.BadArgf("unknown caveat type %s", c.Type)
	}
}

func (c *Controller) getPublicIDHandle(handle int64) (*PublicIDHandle, error) {
	id := c.idStore.Get(handle)
	if id == nil {
		return nil, verror.NotFoundf("uknown public id")
	}
	return &PublicIDHandle{Handle: handle, Names: id.Names()}, nil
}

func (c *Controller) bless(request blessingRequest) (*PublicIDHandle, error) {
	var caveats []security.Caveat
	for _, c := range request.Caveats {
		cav, err := decodeCaveat(c)
		if err != nil {
			return nil, verror.BadArgf("failed to create caveat: %v", err)
		}
		caveats = append(caveats, cav)
	}
	duration := time.Duration(request.DurationMs) * time.Millisecond

	blessee := c.idStore.Get(request.Handle)

	if blessee == nil {
		return nil, verror.NotFoundf("invalid PublicID handle")
	}
	blessor := c.rt.Identity()

	blessed, err := blessor.Bless(blessee, request.Name, duration, caveats)
	if err != nil {
		return nil, err
	}

	return &PublicIDHandle{Handle: c.idStore.Add(blessed), Names: blessed.Names()}, nil
}

// HandleBlessing handles a blessing request from JS.
func (c *Controller) HandleBlessing(data string, w lib.ClientWriter) {
	var request blessingRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&request); err != nil {
		w.Error(verror.Internalf("can't unmarshall message: %v", err))
		return
	}

	handle, err := c.bless(request)

	if err != nil {
		w.Error(err)
		return
	}

	// Send the id back.
	if err := w.Send(lib.ResponseFinal, handle); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
		return
	}
}

func (c *Controller) HandleCreateIdentity(data string, w lib.ClientWriter) {
	var name string
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&name); err != nil {
		w.Error(verror.Internalf("can't unmarshall message: %v", err))
		return
	}
	id, err := c.rt.NewIdentity(name)
	if err != nil {
		w.Error(err)
		return
	}

	publicID := id.PublicID()
	jsID := &PublicIDHandle{Handle: c.idStore.Add(publicID), Names: publicID.Names()}
	if err := w.Send(lib.ResponseFinal, jsID); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
		return
	}
}
