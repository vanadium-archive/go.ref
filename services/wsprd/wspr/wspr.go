// A simple WebSocket proxy (WSPR) that takes in a Veyron RPC message, encoded in JSON
// and stored in a WebSocket message, and sends it to the specified Veyron
// endpoint.
//
// Input arguments must be provided as a JSON message in the following format:
//
// {
//	 "Address" : String, //EndPoint Address
//   "Name" : String, //Service Name
//   "Method"   : String, //Method Name
//   "PrivateID" : "", //Identification
//   "InArgs"     : { "ArgName1" : ArgVal1, "ArgName2" : ArgVal2, ... },
//   "IsStreaming" : true/false
// }
//
package wspr

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	"veyron2/rt"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"
	vom_wiretype "veyron2/vom/wiretype"
	wiretype_build "veyron2/wiretype/build"

	"github.com/gorilla/websocket"
)

const (
	pingInterval = 50 * time.Second              // how often the server pings the client.
	pongTimeout  = pingInterval + 10*time.Second // maximum wait for pong.
)

type wsprConfig struct {
	MounttableRoot []string
}

type WSPR struct {
	tlsCert       *tls.Certificate
	clientCache   *lib.ClientCache
	rt            veyron2.Runtime
	logger        vlog.Logger
	port          int
	veyronProxyEP string
}

var logger vlog.Logger

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
	PrivateId   string // base64(vom(security.PrivateID))
	InArgs      []json.RawMessage
	NumOutArgs  int32
	IsStreaming bool
}

type veyronRPC struct {
	Name        string
	Method      string
	PrivateId   string // base64(vom(security.PrivateID))
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

func (ctx WSPR) newClient(privateId string) (ipc.Client, error) {
	id := decodeIdentity(ctx.logger, privateId)
	client := ctx.clientCache.Get(id)
	var err error
	if client == nil {
		// TODO(bjornick): Use the identity to create the client.
		client, err = ctx.rt.NewClient(veyron2.CallTimeout(ipc.NoTimeout))
		if err != nil {
			return nil, fmt.Errorf("error creating client: %v", err)
		}
		ctx.clientCache.Put(id, client)
	}

	return client, nil
}

func (ctx WSPR) startVeyronRequest(w lib.ClientWriter, msg *veyronRPC) (ipc.Call, error) {
	// Issue request to the endpoint.
	client, err := ctx.newClient(msg.PrivateId)
	if err != nil {
		return nil, err
	}
	methodName := lib.UppercaseFirstCharacter(msg.Method)
	clientCall, err := client.StartCall(ctx.rt.TODOContext(), msg.Name, methodName, msg.InArgs)

	if err != nil {
		return nil, fmt.Errorf("error starting call (name: %v, method: %v, args: %v): %v", msg.Name, methodName, msg.InArgs, err)
	}

	return clientCall, nil
}

func decodeIdentity(logger vlog.Logger, msg string) security.PrivateID {
	if len(msg) == 0 {
		return nil
	}
	// msg contains base64-encoded-vom-encoded identity.PrivateID.
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

func intToByteSlice(i int32) []byte {
	rw := new(bytes.Buffer)
	binary.Write(rw, binary.BigEndian, i)
	buf := make([]byte, 4)
	n, err := io.ReadFull(rw, buf)
	if n != 4 || err != nil {
		panic(fmt.Sprintf("Read less than 4 bytes: %d", n))
	}
	return buf[:n]
}

func (ctx WSPR) handleDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<html>
<head>
<title>/debug</title>
</head>
<body>
<ul>
<li><a href="/debug/pprof">/debug/pprof</a></li>
<li><b>Client cache stats:</b>
`))
	if ctx.clientCache == nil {
		w.Write([]byte("No ClientCache"))
	} else {
		w.Write([]byte(ctx.clientCache.Stats()))
	}
	w.Write([]byte("</li></ul></body></html>"))
}

func readFromRequest(r *http.Request) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	if readBytes, err := io.Copy(&buf, r.Body); err != nil {
		return nil, fmt.Errorf("error copying message out of request: %v", err)
	} else if wantBytes := r.ContentLength; readBytes != wantBytes {
		return nil, fmt.Errorf("read %d bytes, wanted %d", readBytes, wantBytes)
	}
	return &buf, nil
}

func setAccessControl(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

type outstandingStream struct {
	stream stream.Sender
	inType vom.Type
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

	if wsp.writerCreator == nil {
		wsp.writerCreator = func(id int64) lib.ClientWriter {
			return &websocketWriter{ws: wsp.ws, id: id, logger: wsp.ctx.logger}
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

	wc, err := wsp.ws.NextWriter(websocket.TextMessage)
	if err != nil {
		wsp.ctx.logger.Errorf("failed to create websocket writer: %s", err)
		return
	}
	if err := vom.ObjToJSON(wc, vom.ValueOf(msg)); err != nil {
		wsp.ctx.logger.Errorf("failed to convert wspr config to json: %s", err)
		return
	}
	wc.Close()
}

func (wsp *websocketPipe) pingLoop() {
	for {
		time.Sleep(pingInterval)
		wsp.ctx.logger.VI(2).Info("ws: ping")
		if err := wsp.ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
			wsp.ctx.logger.Error("ws: ping failed")
			return
		}
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
func (wsp *websocketPipe) handleVeyronRequest(id int64, data string, w *websocketWriter) {
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
			wsp.ws.WriteMessage(websocket.TextMessage, []byte(errMsg))
			continue
		}

		ww := &websocketWriter{ws: wsp.ws, id: msg.Id, logger: wsp.ctx.logger}

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
func (wsp *websocketPipe) handleServeRequest(data string, w *websocketWriter) {
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
func (wsp *websocketPipe) handleStopRequest(data string, w *websocketWriter) {

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

	client, err := wsp.ctx.newClient(tempMsg.PrivateId)
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
	msg.PrivateId = tempMsg.PrivateId
	msg.NumOutArgs = tempMsg.NumOutArgs
	msg.IsStreaming = tempMsg.IsStreaming

	inStreamType := vom_wiretype.Type{
		ID:   methSig.InStream,
		Defs: &td,
	}

	wsp.ctx.logger.VI(2).Infof("VeyronRPC: %s.%s(id=%v, ..., streaming=%v)", msg.Name, msg.Method, len(msg.PrivateId) > 0, msg.IsStreaming)
	return &msg, inStreamType, nil
}

type signatureRequest struct {
	Name      string
	PrivateId string
}

func (wsp *websocketPipe) getSignature(name string, privateId string) (signature.JSONServiceSignature, error) {
	client, err := wsp.ctx.newClient(privateId)
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
func (wsp *websocketPipe) handleSignatureRequest(data string, w *websocketWriter) {
	// Decode the request
	var request signatureRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&request); err != nil {
		w.Error(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}

	wsp.ctx.logger.VI(2).Infof("requesting Signature for %q", request.Name)
	wsp.ctx.logger.VI(2).Info("private id is", request.PrivateId)
	jsSig, err := wsp.getSignature(request.Name, request.PrivateId)
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

func (ctx *WSPR) setup() {
	// Cache up to 20 identity.PrivateID->ipc.Client mappings
	ctx.clientCache = lib.NewClientCache(20)
}

// Starts the proxy and listens for requests. This method is blocking.
func (ctx WSPR) Run() {
	ctx.setup()
	http.HandleFunc("/debug", ctx.handleDebug)
	http.Handle("/favicon.ico", http.NotFoundHandler())
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		pipe := &websocketPipe{ctx: &ctx}
		pipe.start(w, r)
	})
	ctx.logger.VI(1).Infof("Listening on port %d.", ctx.port)
	httpErr := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", ctx.port), nil)
	if httpErr != nil {
		log.Fatalf("Failed to HTTP serve: %s", httpErr)
	}
}

func (ctx WSPR) Shutdown() {
	ctx.rt.Cleanup()
}

// Creates a new WebSocket Proxy object.
func NewWSPR(port int, veyronProxyEP string, opts ...veyron2.ROpt) *WSPR {
	if veyronProxyEP == "" {
		log.Fatalf("a veyron proxy must be set")
	}

	newrt, err := rt.New(opts...)
	if err != nil {
		log.Fatalf("rt.New failed: %s", err)
	}

	return &WSPR{port: port, veyronProxyEP: veyronProxyEP, rt: newrt, logger: newrt.Logger()}
}
