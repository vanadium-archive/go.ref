package wspr

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"sync"
	"time"

	"veyron/services/wsprd/ipc/client"
	"veyron/services/wsprd/ipc/server"
	"veyron/services/wsprd/ipc/stream"
	"veyron/services/wsprd/lib"
	"veyron/services/wsprd/signature"
	"veyron2"
	"veyron2/ipc"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"
	vom_wiretype "veyron2/vom/wiretype"
	wiretype_build "veyron2/wiretype/build"

	"github.com/gorilla/websocket"
)

// The type of message sent by the JS client to the wspr.
type websocketMessageType int

const (
	// Making a veyron client request, streaming or otherwise
	websocketVeyronRequest websocketMessageType = 0

	// Serving this websocket under an object name
	websocketServe = 1

	// A response from a service in javascript to a request
	// from the proxy.
	websocketServerResponse = 2

	// Sending streaming data, either from a JS client or JS service.
	websocketStreamingValue = 3

	// A response that means the stream is closed by the client.
	websocketStreamClose = 4

	// A request to get signature of a remote server
	websocketSignatureRequest = 5

	// A request to stop a server
	websocketStopServer = 6

	// A request to associate an identity with an origin
	websocketAssocIdentity = 7
)

type websocketMessage struct {
	Id int64
	// This contains the json encoded payload.
	Data string

	// Whether it is an rpc request or a serve request.
	Type websocketMessageType
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
type websocketPipe struct {
	// Protects outstandingStreams and outstandingServerRequests.
	sync.Mutex
	ws  *websocket.Conn
	ctx *WSPR
	// Used to generate unique ids for requests initiated by the proxy.
	// These ids will be even so they don't collide with the ids generated
	// by the client.
	lastGeneratedId int64

	// Streams for the outstanding requests.
	outstandingStreams map[int64]outstandingStream

	// Maps flowids to the server that owns them.
	flowMap map[int64]*server.Server

	// A manager that handles fetching and caching signature of remote services
	signatureManager lib.SignatureManager

	// We maintain multiple Veyron server per websocket pipe for serving JavaScript
	// services.
	servers map[uint64]*server.Server

	// Creates a client writer for a given flow.  This is a member so that tests can override
	// the default implementation.
	writerCreator func(id int64) lib.ClientWriter

	writeQueue chan wsMessage

	// privateId associated with the pipe
	privateId security.PrivateID
}

// finishCall waits for the call to finish and write out the response to w.
func (wsp *websocketPipe) finishCall(w lib.ClientWriter, clientCall ipc.Call, msg *veyronRPC) {
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
		// return the call application error as is
		w.Error(err)
		return
	}

	if err := w.Send(lib.ResponseFinal, results[0:len(results)-1]); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
	}
}

// Implements the serverHelper interface
func (wsp *websocketPipe) CreateNewFlow(s *server.Server, stream stream.Sender) *server.Flow {
	wsp.Lock()
	defer wsp.Unlock()
	id := wsp.lastGeneratedId
	wsp.lastGeneratedId += 2
	wsp.flowMap[id] = s
	wsp.outstandingStreams[id] = outstandingStream{stream, vom_wiretype.Type{ID: 1}}
	return &server.Flow{ID: id, Writer: wsp.writerCreator(id)}
}

func (wsp *websocketPipe) CleanupFlow(id int64) {
	wsp.Lock()
	defer wsp.Unlock()
	delete(wsp.outstandingStreams, id)
	delete(wsp.flowMap, id)
}

func (wsp *websocketPipe) GetLogger() vlog.Logger {
	return wsp.ctx.logger
}

func (wsp *websocketPipe) RT() veyron2.Runtime {
	return wsp.ctx.rt
}

// cleans up any outstanding rpcs.
func (wsp *websocketPipe) cleanup() {
	wsp.ctx.logger.VI(0).Info("Cleaning up websocket")
	wsp.Lock()
	defer wsp.Unlock()
	for _, stream := range wsp.outstandingStreams {
		if call, ok := stream.stream.(ipc.Call); ok {
			call.Cancel()
		}
	}

	for _, server := range wsp.servers {
		server.Stop()
	}
}

func (wsp *websocketPipe) setup() {
	wsp.signatureManager = lib.NewSignatureManager()
	wsp.outstandingStreams = make(map[int64]outstandingStream)
	wsp.flowMap = make(map[int64]*server.Server)
	wsp.servers = make(map[uint64]*server.Server)
	wsp.writeQueue = make(chan wsMessage, 50)
	go wsp.writeLoop()

	if wsp.writerCreator == nil {
		wsp.writerCreator = func(id int64) lib.ClientWriter {
			return &websocketWriter{wsp: wsp, id: id, logger: wsp.ctx.logger}
		}
	}
}

func (wsp *websocketPipe) writeLoop() {
	for {
		msg, ok := <-wsp.writeQueue
		if !ok {
			wsp.ctx.logger.Errorf("write queue was closed")
			return
		}

		if msg.messageType == websocket.PingMessage {
			wsp.ctx.logger.Infof("sending ping")
		}
		if err := wsp.ws.WriteMessage(msg.messageType, msg.buf); err != nil {
			wsp.ctx.logger.Errorf("failed to write bytes: %s", err)
		}
	}
}

func (wsp *websocketPipe) start(w http.ResponseWriter, req *http.Request) {
	ws, err := websocket.Upgrade(w, req, nil, 1024, 1024)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Not a websocket handshake", 400)
		return
	} else if err != nil {
		http.Error(w, "Internal Error", 500)
		wsp.ctx.logger.Errorf("websocket upgrade failed: %s", err)
		return
	}

	wsp.setup()
	wsp.ws = ws
	wsp.ws.SetPongHandler(wsp.pongHandler)
	wsp.sendInitialMessage()
	go wsp.readLoop()
	go wsp.pingLoop()
}

// Upon first connect, we send a message with the wsprConfig.
func (wsp *websocketPipe) sendInitialMessage() {
	mounttableRoots := strings.Split(os.Getenv("NAMESPACE_ROOT"), ",")
	if len(mounttableRoots) == 1 && mounttableRoots[0] == "" {
		mounttableRoots = []string{}
	}
	msg := wsprConfig{
		MounttableRoot: mounttableRoots,
	}

	var buf bytes.Buffer
	if err := vom.ObjToJSON(&buf, vom.ValueOf(msg)); err != nil {
		wsp.ctx.logger.Errorf("failed to convert wspr config to json: %s", err)
		return
	}
	wsp.writeQueue <- wsMessage{messageType: websocket.TextMessage, buf: buf.Bytes()}
}

func (wsp *websocketPipe) pingLoop() {
	for {
		time.Sleep(pingInterval)
		wsp.ctx.logger.VI(2).Info("ws: ping")
		wsp.writeQueue <- wsMessage{messageType: websocket.PingMessage, buf: []byte{}}
	}
}

func (wsp *websocketPipe) pongHandler(msg string) error {
	wsp.ctx.logger.VI(2).Infof("ws: pong")
	wsp.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	return nil
}

func (wsp *websocketPipe) sendParsedMessageOnStream(id int64, msg interface{}, w lib.ClientWriter) {
	wsp.Lock()
	defer wsp.Unlock()
	stream := wsp.outstandingStreams[id].stream
	if stream == nil {
		w.Error(fmt.Errorf("unknown stream"))
		return
	}

	stream.Send(msg, w)

}

// sendOnStream writes data on id's stream.  Returns an error if the send failed.
func (wsp *websocketPipe) sendOnStream(id int64, data string, w lib.ClientWriter) {
	wsp.Lock()
	typ := wsp.outstandingStreams[id].inType
	wsp.Unlock()
	if typ == nil {
		vlog.Errorf("no inType for stream %d (%q)", id, data)
		return
	}
	payload, err := vom.JSONToObject(data, typ)
	if err != nil {
		vlog.Errorf("error while converting json to InStreamType (%s): %v", data, err)
		return
	}
	wsp.sendParsedMessageOnStream(id, payload, w)
}

func (wsp *websocketPipe) sendVeyronRequest(id int64, veyronMsg *veyronRPC, w lib.ClientWriter, signal chan ipc.Stream) {
	// We have to make the start call synchronous so we can make sure that we populate
	// the call map before we can handle a recieve call.
	call, err := wsp.ctx.startVeyronRequest(w, veyronMsg)
	if err != nil {
		w.Error(verror.Internalf("can't start Veyron Request: %v", err))
		return
	}

	if signal != nil {
		signal <- call
	}

	wsp.finishCall(w, call, veyronMsg)
	if signal != nil {
		wsp.Lock()
		delete(wsp.outstandingStreams, id)
		wsp.Unlock()
	}
}

// handleVeyronRequest starts a veyron rpc and returns before the rpc has been completed.
func (wsp *websocketPipe) handleVeyronRequest(id int64, data string, w lib.ClientWriter) {
	veyronMsg, inStreamType, err := wsp.parseVeyronRequest(bytes.NewBufferString(data))
	if err != nil {
		w.Error(verror.Internalf("can't parse Veyron Request: %v", err))
		return
	}

	wsp.Lock()
	defer wsp.Unlock()
	// If this rpc is streaming, we would expect that the client would try to send
	// on this stream.  Since the initial handshake is done asynchronously, we have
	// to basically put a queueing stream in the map before we make the async call
	// so that the future sends on the stream can see the queuing stream, even if
	// the client call isn't actually ready yet.
	var signal chan ipc.Stream
	if veyronMsg.IsStreaming {
		signal = make(chan ipc.Stream)
		wsp.outstandingStreams[id] = outstandingStream{
			stream: client.StartQueueingStream(signal),
			inType: inStreamType,
		}
	}
	go wsp.sendVeyronRequest(id, veyronMsg, w, signal)
}

func (wsp *websocketPipe) closeStream(id int64) {
	wsp.Lock()
	defer wsp.Unlock()
	stream := wsp.outstandingStreams[id].stream
	if stream == nil {
		wsp.ctx.logger.Errorf("close called on non-existent call: %v", id)
		return
	}

	var call client.QueueingStream
	var ok bool
	if call, ok = stream.(client.QueueingStream); !ok {
		wsp.ctx.logger.Errorf("can't close server stream: %v", id)
		return
	}

	if err := call.Close(); err != nil {
		wsp.ctx.logger.Errorf("client call close failed with: %v", err)
	}
}

func (wsp *websocketPipe) readLoop() {
	wsp.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	for {
		op, r, err := wsp.ws.NextReader()
		if err == io.ErrUnexpectedEOF { // websocket disconnected
			break
		}
		if err != nil {
			wsp.ctx.logger.VI(1).Infof("websocket receive: %s", err)
			break
		}

		if op != websocket.TextMessage {
			wsp.ctx.logger.Errorf("unexpected websocket op: %v", op)
		}

		var msg websocketMessage
		decoder := json.NewDecoder(r)
		if err := decoder.Decode(&msg); err != nil {
			errMsg := fmt.Sprintf("can't unmarshall JSONMessage: %v", err)
			wsp.ctx.logger.Error(errMsg)
			wsp.writeQueue <- wsMessage{messageType: websocket.TextMessage, buf: []byte(errMsg)}
			continue
		}

		ww := wsp.writerCreator(msg.Id)

		switch msg.Type {
		case websocketVeyronRequest:
			wsp.handleVeyronRequest(msg.Id, msg.Data, ww)
		case websocketStreamingValue:
			// This will asynchronous for a client rpc, but synchronous for a
			// server rpc.  This could be potentially bad if the server is sending
			// back large packets.  Making it asynchronous for the server, would make
			// it difficult to guarantee that all stream messages make it to the client
			// before the finish call.
			// TODO(bjornick): Make the server send also asynchronous.
			wsp.sendOnStream(msg.Id, msg.Data, ww)
		case websocketStreamClose:
			wsp.closeStream(msg.Id)
		case websocketServe:
			go wsp.handleServeRequest(msg.Data, ww)
		case websocketStopServer:
			go wsp.handleStopRequest(msg.Data, ww)
		case websocketServerResponse:
			go wsp.handleServerResponse(msg.Id, msg.Data)
		case websocketSignatureRequest:
			go wsp.handleSignatureRequest(msg.Data, ww)
		case websocketAssocIdentity:
			wsp.handleAssocIdentity(msg.Data, ww)
		default:
			ww.Error(verror.Unknownf("unknown message type: %v", msg.Type))
		}
	}
	wsp.cleanup()
}

func (wsp *websocketPipe) maybeCreateServer(serverId uint64) (*server.Server, error) {
	wsp.Lock()
	defer wsp.Unlock()
	if server, ok := wsp.servers[serverId]; ok {
		return server, nil
	}
	server, err := server.NewServer(serverId, wsp.ctx.veyronProxyEP, wsp)
	if err != nil {
		return nil, err
	}
	wsp.servers[serverId] = server
	return server, nil
}

func (wsp *websocketPipe) removeServer(serverId uint64) {
	wsp.Lock()
	server := wsp.servers[serverId]
	if server == nil {
		wsp.Unlock()
		return
	}
	delete(wsp.servers, serverId)
	wsp.Unlock()

	server.Stop()
}

func (wsp *websocketPipe) serve(serveRequest serveRequest, w lib.ClientWriter) {
	// Create a server for the websocket pipe, if it does not exist already
	server, err := wsp.maybeCreateServer(serveRequest.ServerId)
	if err != nil {
		w.Error(verror.Internalf("error creating server: %v", err))
	}

	wsp.ctx.logger.VI(2).Infof("serving under name: %q", serveRequest.Name)

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

// handleServeRequest takes a request to serve a server, creates
// a server, registers the provided services and sends the endpoint back.
func (wsp *websocketPipe) handleServeRequest(data string, w lib.ClientWriter) {
	// Decode the serve request which includes IDL, registered services and name
	var serveRequest serveRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&serveRequest); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}
	wsp.serve(serveRequest, w)
}

// handleStopRequest takes a request to stop a server.
func (wsp *websocketPipe) handleStopRequest(data string, w lib.ClientWriter) {

	var serverId uint64
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&serverId); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}

	wsp.removeServer(serverId)

	// Send true to indicate stop has finished
	if err := w.Send(lib.ResponseFinal, true); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
		return
	}
}

// handleServerResponse handles the completion of outstanding calls to JavaScript services
// by filling the corresponding channel with the result from JavaScript.
func (wsp *websocketPipe) handleServerResponse(id int64, data string) {
	wsp.Lock()
	server := wsp.flowMap[id]
	wsp.Unlock()
	if server == nil {
		wsp.ctx.logger.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	server.HandleServerResponse(id, data)
}

// parseVeyronRequest parses a json rpc request into a veyronRPC object.
func (wsp *websocketPipe) parseVeyronRequest(r io.Reader) (*veyronRPC, vom.Type, error) {
	var tempMsg veyronTempRPC
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&tempMsg); err != nil {
		return nil, nil, fmt.Errorf("can't unmarshall JSONMessage: %v", err)
	}

	client, err := wsp.ctx.newClient()
	if err != nil {
		return nil, nil, verror.Internalf("error creating client: %v", err)
	}

	// Fetch and adapt signature from the SignatureManager
	ctx := wsp.ctx.rt.TODOContext()
	sig, err := wsp.signatureManager.Signature(ctx, tempMsg.Name, client)
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

	wsp.ctx.logger.VI(2).Infof("VeyronRPC: %s.%s(id=%v, ..., streaming=%v)", msg.Name, msg.Method, msg.IsStreaming)
	return &msg, inStreamType, nil
}

type signatureRequest struct {
	Name string
}

func (wsp *websocketPipe) getSignature(name string) (signature.JSONServiceSignature, error) {
	client, err := wsp.ctx.newClient()
	if err != nil {
		return nil, verror.Internalf("error creating client: %v", err)
	}

	// Fetch and adapt signature from the SignatureManager
	ctx := wsp.ctx.rt.TODOContext()
	sig, err := wsp.signatureManager.Signature(ctx, name, client)
	if err != nil {
		return nil, verror.Internalf("error getting service signature for %s: %v", name, err)
	}

	return signature.NewJSONServiceSignature(*sig), nil
}

// handleSignatureRequest uses signature manager to get and cache signature of a remote server
func (wsp *websocketPipe) handleSignatureRequest(data string, w lib.ClientWriter) {
	// Decode the request
	var request signatureRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&request); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}

	wsp.ctx.logger.VI(2).Infof("requesting Signature for %q", request.Name)
	jsSig, err := wsp.getSignature(request.Name)
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

type assocIdentityData struct {
	Account  string
	Identity string // base64(vom(security.PrivateID))
	Origin   string
}

// handleAssocIdentityRequest associates the identity with the origin
func (wsp *websocketPipe) handleAssocIdentity(data string, w lib.ClientWriter) {
	// Decode the request
	var parsedData assocIdentityData
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&parsedData); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}

	wsp.ctx.logger.VI(2).Info("associating name %v and private id %v to origin %v",
		parsedData.Account,
		parsedData.Identity,
		parsedData.Origin)

	idManager := wsp.ctx.idManager

	wsp.privateId = decodeIdentity(wsp.ctx.logger, parsedData.Identity)

	if err := idManager.AddAccount(parsedData.Account, wsp.privateId); err != nil {
		w.Error(verror.Internalf("identity.AddAccount(%v, %v) failed: %v", parsedData.Account, wsp.privateId, err))
	}

	if err := idManager.AddOrigin(parsedData.Origin, parsedData.Account, []security.ServiceCaveat{}); err != nil {
		w.Error(verror.Internalf("identity.AddOrigin(%v, %v, %v) failed: %v", parsedData.Origin, parsedData.Account, []security.ServiceCaveat{}, err))
	}

	if err := w.Send(lib.ResponseFinal, nil); err != nil {
		w.Error(verror.Internalf("error marshalling results: %v", err))
		return
	}
}

func decodeIdentity(logger vlog.Logger, msg string) security.PrivateID {
	if len(msg) == 0 {
		return nil
	}
	// PrivateIds are sent as base64-encoded-vom-encoded identity.PrivateID.
	// Pure JSON or pure VOM could not have been used.
	// - JSON cannot be used because identity.PrivateID contains an
	//   ecdsa.PrivateKey (which encoding/json cannot decode).
	// - Regular VOM cannot be used because it only has a binary,
	//   Go-specific implementation at this time.
	// The "portable" encoding is base64-encoded VOM (see
	// veyron/daemon/cmd/identity/responder/responder.go).
	// When toddw@ has the text-based VOM encoding going, that can probably
	// be used instead.
	var id security.PrivateID
	if err := vom.NewDecoder(base64.NewDecoder(base64.URLEncoding, strings.NewReader(msg))).Decode(&id); err != nil {
		logger.Error("Could not decode identity:", err)
		return nil
	}
	return id
}

func encodeIdentity(logger vlog.Logger, identity security.PrivateID) string {
	var vomEncoded bytes.Buffer
	if err := vom.NewEncoder(&vomEncoded).Encode(identity); err != nil {
		logger.Error("Could not encode identity: %v", err)
	}
	var base64Encoded bytes.Buffer
	encoder := base64.NewEncoder(base64.URLEncoding, &base64Encoded)
	encoder.Write(vomEncoded.Bytes())
	encoder.Close()
	return base64Encoded.String()
}
