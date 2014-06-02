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
package lib

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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"

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
	clientCache   *ClientCache
	rt            veyron2.Runtime
	logger        vlog.Logger
	port          int
	veyronProxyEP string
}

type responseType int

const (
	responseFinal       responseType = 0
	responseStream                   = 1
	responseError                    = 2
	serverRequest                    = 3
	responseStreamClose              = 4
)

// Wraps a response to the proxy client and adds a message type.
type response struct {
	Type    responseType
	Message interface{}
}

var logger vlog.Logger

// The type of message sent by the JS client to the wspr.
type websocketMessageType int

const (
	// Making a veyron client request, streaming or otherwise
	websocketVeyronRequest websocketMessageType = 0

	// Publishing this websocket under a veyron name
	websocketPublish = 1

	// A response from a service in javascript to a request
	// from the proxy.
	websocketServerResponse = 2

	// Sending streaming data, either from a JS client or JS service.
	websocketStreamingValue = 3

	// A response that means the stream is closed by the client.
	websocketStreamClose = 4

	// A request to get signature of a remote server
	websocketSignatureRequest = 5
)

type websocketMessage struct {
	Id int64
	// This contains the json encoded payload.
	Data string

	// Whether it is an rpc request or a publish request.
	Type websocketMessageType
}

type veyronRPC struct {
	Name        string
	Method      string
	PrivateId   string // base64(vom(security.PrivateID))
	InArgs      []interface{}
	NumOutArgs  int32
	IsStreaming bool
}

// A request javascript to publish on a particular name
type publishRequest struct {
	Name     string
	ServerId uint64
	Services map[string]JSONServiceSignature
}

// A request from javascript to register a particular prefix
type registerRequest struct {
	Prefix string
	// TODO(bjornick): Do we care about the methods?
}

// A request from the proxy to javascript to handle an RPC
type serverRPCRequest struct {
	ServerId    uint64
	ServiceName string
	Method      string
	Args        []interface{}
	Context     serverRPCRequestContext
}

// call context for a serverRPCRequest
type serverRPCRequestContext struct {
	Suffix string
	Name   string
}

// The response from the javascript server to the proxy.
type serverRPCReply struct {
	Results []interface{}
	Err     *verror.Standard
}

// This is basically an io.Writer interface, that allows passing error message
// strings.  This is how the proxy will talk to the javascript/java clients.
type clientWriter interface {
	Write(p []byte) (int, error)

	getLogger() vlog.Logger

	sendError(err error)

	FinishMessage() error
}

type vomMessage struct {
	Format string // 'binary' or 'json'
	Data   string // base64 encoded bytes if binary, or the VOM JSON string
}

type vomToJSONRequest struct {
	Message vomMessage
}

type vomToJSONResponse struct {
	Message string
}

type jsonToVOMRequest struct {
	RequestedFormat string // 'binary' or 'json'
	Message         string
}

type jsonToVOMResponse struct {
	Message vomMessage
}

// Implements clientWriter interface for sending messages over websockets.
type websocketWriter struct {
	ws     *websocket.Conn
	buf    bytes.Buffer
	logger vlog.Logger // TODO(bprosnitz) Remove this -- it has nothing to do with websockets!
	id     int64
}

func (w *websocketWriter) getLogger() vlog.Logger {
	return w.logger
}

func (w *websocketWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	return len(p), nil
}

func (w *websocketWriter) sendError(err error) {
	verr := verror.ToStandard(err)

	// Also log the error but write internal errors at a more severe log level
	var logLevel vlog.Level = 2
	logErr := fmt.Sprintf("%v", verr)
	// We want to look at the stack three frames up to find where the error actually
	// occurred.  (caller -> websocketErrorResponse/sendError -> generateErrorMessage).
	if _, file, line, ok := runtime.Caller(3); ok {
		logErr = fmt.Sprintf("%s:%d: %s", filepath.Base(file), line, logErr)
	}
	if verror.Is(verr, verror.Internal) {
		logLevel = 2
	}
	w.logger.VI(logLevel).Info(logErr)

	var errMsg = verror.Standard{
		ID:  verr.ErrorID(),
		Msg: verr.Error(),
	}

	w.buf.Reset()
	if err := vom.ObjToJSON(&w.buf, vom.ValueOf(response{Type: responseError, Message: errMsg})); err != nil {
		w.logger.VI(2).Info("Failed to marshal with", err)
		return
	}
	if err := w.FinishMessage(); err != nil {
		w.logger.VI(2).Info("WSPR: error finishing message: ", err)
		return
	}
}

func (w *websocketWriter) FinishMessage() error {
	wc, err := w.ws.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	if err := vom.ObjToJSON(wc, vom.ValueOf(websocketMessage{Id: w.id, Data: w.buf.String()})); err != nil {
		return err
	}
	wc.Close()
	w.buf.Reset()
	return nil
}

// finishCall waits for the call to finish and write out the response to w.
func finishCall(w clientWriter, clientCall ipc.Call, msg *veyronRPC) {
	if msg.IsStreaming {
		for {
			var item interface{}
			if err := clientCall.Recv(&item); err != nil {
				if err == io.EOF {
					break
				}
				w.sendError(err) // Send streaming error as is
				return
			}
			data := &response{Type: responseStream, Message: item}
			if err := vom.ObjToJSON(w, vom.ValueOf(data)); err != nil {
				w.sendError(verror.Internalf("unable to marshal: %v", item))
				continue
			}
			if err := w.FinishMessage(); err != nil {
				w.getLogger().VI(2).Info("WSPR: error finishing message: ", err)
			}
		}

		if err := vom.ObjToJSON(w, vom.ValueOf(response{Type: responseStreamClose})); err != nil {
			w.sendError(verror.Internalf("unable to marshal close stream message"))
		}
		if err := w.FinishMessage(); err != nil {
			w.getLogger().VI(2).Info("WSPR: error finishing message: ", err)
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
		w.sendError(err)
		return
	}
	// for now we assume last out argument is always error
	if len(results) < 1 {
		w.sendError(verror.Internalf("client call did not return any results"))
		return
	}

	if err, ok := results[len(results)-1].(error); ok {
		// return the call application error as is
		w.sendError(err)
		return
	}

	data := response{Type: responseFinal, Message: results[0 : len(results)-1]}
	if err := vom.ObjToJSON(w, vom.ValueOf(data)); err != nil {
		w.sendError(verror.Internalf("error marshalling results: %v", err))
		return
	}

	if err := w.FinishMessage(); err != nil {
		w.getLogger().VI(2).Info("WSPR: error finishing message: ", err)
		return
	}
}

func (ctx WSPR) parseVeyronRequest(r io.Reader) (*veyronRPC, error) {
	var msg veyronRPC
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&msg); err != nil {
		return nil, fmt.Errorf("can't unmarshall JSONMessage: %v", err)
	}

	ctx.logger.VI(2).Infof("VeyronRPC: %s.%s(id=%v, ..., streaming=%v)", msg.Name, msg.Method, len(msg.PrivateId) > 0, msg.IsStreaming)
	return &msg, nil
}

func (ctx WSPR) newClient(privateId string) (ipc.Client, error) {
	id := decodeIdentity(ctx.logger, privateId)
	client := ctx.clientCache.Get(id)
	var err error
	if client == nil {
		// TODO(bjornick): Use the identity to create the client.
		client, err = ctx.rt.NewClient()
		if err != nil {
			return nil, fmt.Errorf("error creating client: %v", err)
		}
		ctx.clientCache.Put(id, client)
	}

	return client, nil
}

func (ctx WSPR) startVeyronRequest(w clientWriter, msg *veyronRPC) (ipc.Call, error) {
	// Issue request to the endpoint.
	client, err := ctx.newClient(msg.PrivateId)
	if err != nil {
		return nil, err
	}
	clientCall, err := client.StartCall(ctx.rt.TODOContext(), msg.Name, uppercaseFirstCharacter(msg.Method), msg.InArgs)

	if err != nil {
		return nil, fmt.Errorf("error starting call: %v", err)
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
		logger.VI(0).Info("Could not decode identity:", err)
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
	outstandingStreams map[int64]sender

	// Channels that are used to communicate the final response of a
	// server request.
	outstandingServerRequests map[int64]chan serverRPCReply

	// A manager that handles fetching and caching signature of remote services
	signatureManager *signatureManager

	// We maintain one Veyron server per websocket pipe for publishing JavaScript
	// services.
	veyronServer ipc.Server

	// Endpoint for the server.
	endpoint naming.Endpoint

	// Creates a client writer for a given flow.  This is a member so that tests can override
	// the default implementation.
	writerCreator func(id int64) clientWriter
}

// cleans up any outstanding rpcs.
func (wsp *websocketPipe) cleanup() {
	wsp.Lock()
	defer wsp.Unlock()
	for _, stream := range wsp.outstandingStreams {
		if call, ok := stream.(ipc.Call); ok {
			call.Cancel()
		}
	}

	result := serverRPCReply{
		Results: []interface{}{nil},
		Err: &verror.Standard{
			ID:  verror.Aborted,
			Msg: "timeout",
		},
	}

	for _, reply := range wsp.outstandingServerRequests {
		reply <- result
	}

	if wsp.veyronServer != nil {
		wsp.ctx.logger.VI(0).Infof("Stopping server")
		wsp.veyronServer.Stop()
	}
	wsp.ctx.logger.VI(0).Infof("Stopped server")
}

func (wsp *websocketPipe) setup() {
	wsp.ctx.logger.Info("identity is", wsp.ctx.rt.Identity())
	wsp.signatureManager = newSignatureManager()
	wsp.outstandingServerRequests = make(map[int64]chan serverRPCReply)
	wsp.outstandingStreams = make(map[int64]sender)

	if wsp.writerCreator == nil {
		wsp.writerCreator = func(id int64) clientWriter {
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
		wsp.ctx.logger.VI(0).Infof("websocket upgrade failed: %s", err)
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
	mounttableRoots := strings.Split(os.Getenv("MOUNTTABLE_ROOT"), ",")
	if len(mounttableRoots) == 1 && mounttableRoots[0] == "" {
		mounttableRoots = []string{}
	}
	msg := wsprConfig{
		MounttableRoot: mounttableRoots,
	}

	wc, err := wsp.ws.NextWriter(websocket.TextMessage)
	if err != nil {
		wsp.ctx.logger.VI(0).Infof("failed to create websocket writer: %s", err)
		return
	}
	if err := vom.ObjToJSON(wc, vom.ValueOf(msg)); err != nil {
		wsp.ctx.logger.VI(0).Infof("failed to convert wspr config to json: %s", err)
		return
	}
	wc.Close()
}

func (wsp *websocketPipe) pingLoop() {
	for {
		time.Sleep(pingInterval)
		wsp.ctx.logger.VI(2).Infof("ws: ping")
		if err := wsp.ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
			wsp.ctx.logger.VI(2).Infof("ws: ping failed")
			return
		}
	}
}

func (wsp *websocketPipe) pongHandler(msg string) error {
	wsp.ctx.logger.VI(2).Infof("ws: pong")
	wsp.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	return nil
}

func (wsp *websocketPipe) sendParsedMessageOnStream(id int64, msg interface{}, w clientWriter) {
	wsp.Lock()
	defer wsp.Unlock()
	stream := wsp.outstandingStreams[id]
	if stream == nil {
		w.sendError(fmt.Errorf("unknown stream"))
	}

	stream.Send(msg, w)

}

// sendOnStream writes data on id's stream.  Returns an error if the send failed.
func (wsp *websocketPipe) sendOnStream(id int64, data string, w clientWriter) {
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	var payload interface{}
	if err := decoder.Decode(&payload); err != nil {
		w.sendError(fmt.Errorf("can't unmarshal JSONMessage: %v", err))
		return
	}
	wsp.sendParsedMessageOnStream(id, payload, w)
}

func (wsp *websocketPipe) sendVeyronRequest(id int64, veyronMsg *veyronRPC, w clientWriter, signal chan ipc.Stream) {
	// We have to make the start call synchronous so we can make sure that we populate
	// the call map before we can handle a recieve call.
	call, err := wsp.ctx.startVeyronRequest(w, veyronMsg)
	if err != nil {
		w.sendError(verror.Internalf("can't start Veyron Request: %v", err))
		return
	}

	if signal != nil {
		signal <- call
	}

	finishCall(w, call, veyronMsg)
	if signal != nil {
		wsp.Lock()
		delete(wsp.outstandingStreams, id)
		wsp.Unlock()
	}
}

// handleVeyronRequest starts a veyron rpc and returns before the rpc has been completed.
func (wsp *websocketPipe) handleVeyronRequest(id int64, data string, w *websocketWriter) {
	veyronMsg, err := wsp.ctx.parseVeyronRequest(bytes.NewBufferString(data))

	if err != nil {
		w.sendError(verror.Internalf("can't parse Veyron Request: %v", err))
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
		wsp.outstandingStreams[id] = startQueueingStream(signal)
	}
	go wsp.sendVeyronRequest(id, veyronMsg, w, signal)
}

func (wsp *websocketPipe) closeStream(id int64) {
	wsp.Lock()
	defer wsp.Unlock()
	stream := wsp.outstandingStreams[id]
	if stream == nil {
		wsp.ctx.logger.VI(0).Infof("close called on non-existent call: %v", id)
		return
	}

	var call queueingStream
	var ok bool
	if call, ok = stream.(queueingStream); !ok {
		wsp.ctx.logger.VI(0).Infof("can't close server stream: %v", id)
		return
	}

	if err := call.Close(); err != nil {
		wsp.ctx.logger.VI(0).Infof("client call close failed with: %v", err)
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
			wsp.ctx.logger.VI(0).Infof("websocket receive: %s", err)
			break
		}

		if op != websocket.TextMessage {
			wsp.ctx.logger.VI(0).Infof("unexpected websocket op: %v", op)
		}

		var msg websocketMessage
		decoder := json.NewDecoder(r)
		if err := decoder.Decode(&msg); err != nil {
			errMsg := fmt.Sprintf("can't unmarshall JSONMessage: %v", err)
			wsp.ctx.logger.VI(2).Info(errMsg)
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
		case websocketPublish:
			go wsp.handlePublishRequest(msg.Data, ww)
		case websocketServerResponse:
			go wsp.handleServerResponse(msg.Id, msg.Data)
		case websocketSignatureRequest:
			go wsp.handleSignatureRequest(msg.Data, ww)
		default:
			ww.sendError(verror.Unknownf("unknown message type: %v", msg.Type))
		}
	}
	wsp.cleanup()
}

func (wsp *websocketPipe) publish(publishRequest publishRequest, w clientWriter) {
	// Create a server for the websocket pipe, if it does not exist already
	wsp.Lock()
	shouldListen := false
	if wsp.veyronServer == nil {
		// We have to create a StreamManger per veyronServer so that they
		// have different routing ids if we are listening on the veyron proxy.
		manager, err := wsp.ctx.rt.NewStreamManager()
		if err != nil {
			w.sendError(verror.Internalf("failed creating server: %v", err))
			wsp.Unlock()
			return
		}
		veyronServer, err := wsp.ctx.rt.NewServer(veyron2.StreamManager(manager))
		if err != nil {
			w.sendError(verror.Internalf("failed creating server: %v", err))
			wsp.Unlock()
			return
		}
		wsp.veyronServer = veyronServer
		shouldListen = true
	}
	wsp.Unlock()

	wsp.ctx.logger.VI(2).Infof("publishing under name: %q", publishRequest.Name)

	// Register each service under the server
	for serviceName, jsonIDLSig := range publishRequest.Services {
		// Create a function that is called from the framework by the invoker, which
		// itself is called by the IPC framework.
		// This function returns a channel that will be filled with the result of
		// the call when results come back from JavaScript.
		remoteInvokerFunc := wsp.createRemoteInvokerFunc(publishRequest.ServerId, serviceName)

		sig, err := jsonIDLSig.ServiceSignature()
		if err != nil {
			w.sendError(verror.Internalf("error creating service signature: %v", err))
			return
		}

		// Create an invoker based on the available methods in the IDL and give it
		// the removeInvokerFunction to call
		invoker, err := newInvoker(sig, remoteInvokerFunc)
		if err != nil {
			w.sendError(verror.Internalf("error registering service: %s: %v", serviceName, err))
			return
		}

		//TODO(aghassemi) Security labels need to come from JS service, for now we allow AllLabels
		authorizer := security.NewACLAuthorizer(
			security.ACL{security.AllPrincipals: security.AllLabels},
		)

		dispatcher := newDispatcher(invoker, authorizer)
		if err := wsp.veyronServer.Register(serviceName, dispatcher); err != nil {
			w.sendError(verror.Internalf("error registering service: %s: %v", serviceName, err))
			return
		}
	}

	if shouldListen {
		var err error
		// Create an endpoint and begin listening.
		wsp.endpoint, err = wsp.veyronServer.Listen("veyron", wsp.ctx.veyronProxyEP)
		if err != nil {
			w.sendError(verror.Internalf("error listening to service: %v", err))
			return
		}
	}

	if err := wsp.veyronServer.Publish(publishRequest.Name); err != nil {
		w.sendError(verror.Internalf("error publishing service: %v", err))
		return
	}
	// Send the endpoint back
	endpointData := response{Type: responseFinal, Message: wsp.endpoint.String()}
	if err := vom.ObjToJSON(w, vom.ValueOf(endpointData)); err != nil {
		w.sendError(verror.Internalf("error marshalling results: %v", err))
		return
	}

	if err := w.FinishMessage(); err != nil {
		w.getLogger().VI(2).Info("WSPR: error finishing message: ", err)
		return
	}
}

// handlePublishRequest takes a request to publish a server, creates
// a server, registers the provided services and sends the endpoint back.
func (wsp *websocketPipe) handlePublishRequest(data string, w *websocketWriter) {
	// Decode the publish request which includes IDL, registered services and name
	var publishRequest publishRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&publishRequest); err != nil {
		w.sendError(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}
	wsp.publish(publishRequest, w)
}

// remoteInvokeFunc is a type of function that can invoke a remote method and
// communicate the result back via a channel to the caller
type remoteInvokeFunc func(methodName string, args []interface{}, call ipc.ServerCall) <-chan serverRPCReply

func (wsp *websocketPipe) proxyStream(stream ipc.Stream, w clientWriter, id int64) {
	var item interface{}
	for err := stream.Recv(&item); err == nil; err = stream.Recv(&item) {
		data := response{Type: responseStream, Message: item}
		if err := vom.ObjToJSON(w, vom.ValueOf(data)); err != nil {
			w.sendError(verror.Internalf("error marshalling stream: %v:", err))
			return
		}
		if err := w.FinishMessage(); err != nil {
			w.getLogger().VI(2).Info("WSPR: error finishing message", err)
			return
		}
	}
	wsp.Lock()
	defer wsp.Unlock()
	if _, ok := wsp.outstandingStreams[id]; !ok {
		wsp.ctx.logger.VI(2).Infof("Dropping close for flow: %d", id)
		return
	}

	if err := vom.ObjToJSON(w, vom.ValueOf(response{Type: responseStreamClose})); err != nil {
		w.sendError(verror.Internalf("error closing stream: %v:", err))
		return
	}
	if err := w.FinishMessage(); err != nil {
		w.getLogger().VI(2).Info("WSPR: error finishing message", err)
		return
	}
}

// createRemoteInvokerFunc creates and returns a delegate of type remoteInvokeFunc.
// This delegate function is called by http proxy invoker which itself is called
// by the ipc framework on RPC calls. This function is delegated with the task
// of making a call to JavaScript through websockets and to return a channel that will
// be filled later by handleServerResponse when results come back from JavaScript.
func (wsp *websocketPipe) createRemoteInvokerFunc(serverId uint64, serviceName string) remoteInvokeFunc {
	return func(methodName string, args []interface{}, call ipc.ServerCall) <-chan serverRPCReply {
		wsp.Lock()
		// Register a new channel
		messageId := wsp.lastGeneratedId
		replyChan := make(chan serverRPCReply, 1)
		wsp.outstandingServerRequests[messageId] = replyChan
		wsp.outstandingStreams[messageId] = senderWrapper{stream: call}
		wsp.lastGeneratedId += 2 // Sever generated IDs are even numbers
		wsp.Unlock()

		ww := wsp.writerCreator(messageId)

		//TODO(aghassemi) Add security related stuff (Caveats, Label, LocalID, RemoteID) to context as part of security work
		//TODO(aghassemi) Deadline and Canceled, we need to find a way to do that for JS since
		//canceled being a async WS message would yield wrong results.
		context := serverRPCRequestContext{
			Suffix: call.Suffix(),
			Name:   call.Name(),
		}
		// Send a invocation request to JavaScript
		message := serverRPCRequest{
			ServerId:    serverId,
			ServiceName: serviceName,
			Method:      lowercaseFirstCharacter(methodName),
			Args:        args,
			Context:     context,
		}
		data := response{Type: serverRequest, Message: message}
		if err := vom.ObjToJSON(ww, vom.ValueOf(data)); err != nil {
			// Error in marshaling, pass the error through the channel immediately
			replyChan <- serverRPCReply{nil,
				&verror.Standard{
					ID:  verror.Internal,
					Msg: fmt.Sprintf("could not marshal the method call data: %v", err)},
			}
			return replyChan
		}
		if err := ww.FinishMessage(); err != nil {
			replyChan <- serverRPCReply{nil,
				&verror.Standard{
					ID:  verror.Internal,
					Msg: fmt.Sprintf("WSPR: error finishing message: %v", err)},
			}
			return replyChan
		}

		wsp.ctx.logger.VI(3).Infof("request received to call method %q on "+
			"JavaScript server %q with args %v, MessageId %d was assigned.",
			methodName, serviceName, args, messageId)

		go wsp.proxyStream(call, ww, messageId)
		return replyChan
	}
}

// handleServerResponse handles the completion of outstanding calls to JavaScript services
// by filling the corresponding channel with the result from JavaScript.
func (wsp *websocketPipe) handleServerResponse(id int64, data string) {
	// Find the corresponding Go channel waiting on the result
	serverResponseChan := wsp.outstandingServerRequests[int64(id)]
	if serverResponseChan == nil {
		wsp.ctx.logger.VI(0).Infof("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}

	// Decode the result and send it through the channel
	var serverReply serverRPCReply
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if decoderErr := decoder.Decode(&serverReply); decoderErr != nil {
		err := verror.Standard{
			ID:  verror.Internal,
			Msg: fmt.Sprintf("could not unmarshal the result from the server: %v", decoderErr),
		}
		serverReply = serverRPCReply{nil, &err}
	}

	wsp.ctx.logger.VI(3).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, serverReply)

	wsp.Lock()
	defer wsp.Unlock()
	wsp.ctx.logger.VI(2).Infof("Deleting stream for %v", id)
	delete(wsp.outstandingStreams, id)
	delete(wsp.outstandingServerRequests, id)
	wsp.ctx.logger.VI(2).Infof("Sending back %v", serverReply)
	serverResponseChan <- serverReply
}

type signatureRequest struct {
	Name      string
	PrivateId string
}

func (wsp *websocketPipe) getSignature(name string, privateId string) (JSONServiceSignature, error) {
	client, err := wsp.ctx.newClient(privateId)
	if err != nil {
		return nil, verror.Internalf("error creating client: %v", err)
	}

	// Fetch and adapt signature from the SignatureManager
	ctx := wsp.ctx.rt.TODOContext()
	sig, err := wsp.signatureManager.signature(ctx, name, client)
	if err != nil {
		return nil, verror.Internalf("error getting service signature for %s: %v", name, err)
	}

	return NewJSONServiceSignature(*sig), nil
}

// handleSignatureRequest uses signature manager to get and cache signature of a remote server
func (wsp *websocketPipe) handleSignatureRequest(data string, w *websocketWriter) {

	// Decode the request
	var request signatureRequest
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&request); err != nil {
		w.sendError(verror.Internalf("can't unmarshal JSONMessage: %v", err))
		return
	}

	wsp.ctx.logger.VI(2).Infof("requesting Signature for %q", request.Name)
	wsp.ctx.logger.VI(2).Info("private id is", request.PrivateId)
	jsSig, err := wsp.getSignature(request.Name, request.PrivateId)
	if err != nil {
		w.sendError(err)
		return
	}

	// Send the signature back
	signatureData := response{Type: responseFinal, Message: jsSig}
	if err := vom.ObjToJSON(w, vom.ValueOf(signatureData)); err != nil {
		w.sendError(verror.Internalf("error marshalling results: %v", err))
		return
	}
	if err := w.FinishMessage(); err != nil {
		w.logger.VI(2).Info("WSPR: error finishing message: ", err)
		return
	}
}

func (ctx *WSPR) setup() {

	// Cache up to 20 identity.PrivateID->ipc.Client mappings
	ctx.clientCache = NewClientCache(20)

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
	ctx.logger.VI(0).Infof("Listening on port %d.", ctx.port)
	httpErr := http.ListenAndServe(fmt.Sprintf(":%d", ctx.port), nil)
	if httpErr != nil {
		log.Fatalf("Failed to HTTP serve: %s", httpErr)
	}
}

func (ctx WSPR) Shutdown() {
	ctx.rt.Shutdown()
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
