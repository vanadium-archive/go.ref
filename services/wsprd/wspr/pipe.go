package wspr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
	"veyron.io/wspr/veyron/services/wsprd/app"
	"veyron.io/wspr/veyron/services/wsprd/lib"

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

	// TODO(ataly, ashankar, bjornick): Get rid of the constants below
	// once the old security model is killed.
	// A request to bless an identity
	websocketBlessIdentity = 8

	// A request to unlink an identity.  This request means that
	// we can remove the given handle from the handle store.
	websocketUnlinkIdentity = 9

	// A request to create a new random identity
	websocketCreateIdentity = 10

	// A request to run the lookup function on a dispatcher.
	websocketLookupResponse = 11

	// A request to run the authorizer for an rpc.
	websocketAuthResponse = 12

	// A request to run a namespace client method
	websocketNamespaceRequest = 13

	// A request to bless a public key.
	websocketBlessPublicKey = 14

	// A request to unlink blessings.  This request means that
	// we can remove the given handle from the handle store.
	websocketUnlinkBlessings = 15

	// A request to create a new random blessings
	websocketCreateBlessings = 16
)

type websocketMessage struct {
	Id int64
	// This contains the json encoded payload.
	Data string

	// Whether it is an rpc request or a serve request.
	Type websocketMessageType
}

// wsMessage is the struct that is put on the write queue.
type wsMessage struct {
	buf         []byte
	messageType int
}

type pipe struct {
	// The struct that handles the translation of javascript request to veyron requests.
	controller *app.Controller

	ws *websocket.Conn

	logger vlog.Logger

	wspr *WSPR

	// Creates a client writer for a given flow.  This is a member so that tests can override
	// the default implementation.
	writerCreator func(id int64) lib.ClientWriter

	// There is a single write goroutine because ws.NewWriter() creates a new writer that
	// writes to a shared buffer in the websocket, so it is not safe to have multiple go
	// routines writing to different websocket writers.
	writeQueue chan wsMessage

	// This request is used to tell WSPR which pipe to remove when we shutdown.
	req *http.Request
}

func newPipe(w http.ResponseWriter, req *http.Request, wspr *WSPR, creator func(id int64) lib.ClientWriter) *pipe {
	pipe := &pipe{logger: wspr.rt.Logger(), wspr: wspr, req: req}

	if creator == nil {
		creator = func(id int64) lib.ClientWriter {
			return &websocketWriter{p: pipe, id: id, logger: pipe.logger}
		}
	}
	pipe.writerCreator = creator
	origin := req.Header.Get("Origin")
	if origin == "" {
		wspr.rt.Logger().Errorf("Could not read origin from the request")
		http.Error(w, "Could not read origin from the request", http.StatusBadRequest)
		return nil
	}

	// Shortcut for old security model.
	if wspr.useOldModel {
		return newPipeOldModel(pipe, origin, w, req, wspr, creator)
	}

	p, err := wspr.principalManager.Principal(origin)
	if err != nil {
		p = wspr.rt.Principal()
		wspr.rt.Logger().Errorf("no principal associated with origin %s: %v", origin, err)
		// TODO(bjornick): Send an error to the client when all of the principal stuff is set up.
	}

	pipe.controller, err = app.NewController(creator, &wspr.listenSpec, options.RuntimePrincipal{p})
	if err != nil {
		wspr.rt.Logger().Errorf("Could not create controller: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create controller: %v", err), http.StatusInternalServerError)
		return nil
	}

	pipe.start(w, req)
	return pipe
}

// TODO(ataly, ashankar, bjornick): Get rid of this method once the old security model
// is killed.
func newPipeOldModel(pipe *pipe, origin string, w http.ResponseWriter, req *http.Request, wspr *WSPR, creator func(id int64) lib.ClientWriter) *pipe {
	id, err := wspr.idManager.Identity(origin)
	if err != nil {
		id = wspr.rt.Identity()
		wspr.rt.Logger().Errorf("no identity associated with origin %s: %v", origin, err)
		// TODO(bjornick): Send an error to the client when all of the identity stuff is set up.
	}

	pipe.controller, err = app.NewController(creator, &wspr.listenSpec, options.RuntimeID{id})
	if err != nil {
		wspr.rt.Logger().Errorf("Could not create controller: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create controller: %v", err), http.StatusInternalServerError)
		return nil
	}

	pipe.start(w, req)
	return pipe
}

// cleans up any outstanding rpcs.
func (p *pipe) cleanup() {
	p.logger.VI(0).Info("Cleaning up websocket")
	p.controller.Cleanup()
	p.ws.Close()
	p.wspr.CleanUpPipe(p.req)
}

func (p *pipe) setup() {
	p.writeQueue = make(chan wsMessage, 50)
	go p.writeLoop()
}

func (p *pipe) writeLoop() {
	for {
		msg, ok := <-p.writeQueue
		if !ok {
			p.logger.Errorf("write queue was closed")
			return
		}

		if msg.messageType == websocket.PingMessage {
			p.logger.Infof("sending ping")
		}
		if err := p.ws.WriteMessage(msg.messageType, msg.buf); err != nil {
			p.logger.Errorf("failed to write bytes: %s", err)
		}
	}
}

func (p *pipe) start(w http.ResponseWriter, req *http.Request) {
	ws, err := websocket.Upgrade(w, req, nil, 1024, 1024)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Not a websocket handshake", 400)
		return
	} else if err != nil {
		http.Error(w, "Internal Error", 500)
		p.logger.Errorf("websocket upgrade failed: %s", err)
		return
	}

	p.ws = ws
	p.ws.SetPongHandler(p.pongHandler)
	p.setup()
	p.sendInitialMessage()

	go p.readLoop()
	go p.pingLoop()
}

// Upon first connect, we send a message with the wsprConfig.
func (p *pipe) sendInitialMessage() {
	// TODO(bprosnitz) Use the same root lookup as the rest of veyron so that this is consistent.
	mounttableRoots := strings.Split(os.Getenv("NAMESPACE_ROOT"), ",")
	if len(mounttableRoots) == 1 && mounttableRoots[0] == "" {
		mounttableRoots = []string{}
	}
	if mtRoot := os.Getenv("MOUNTTABLE_ROOT"); mtRoot != "" {
		mounttableRoots = append(mounttableRoots, mtRoot)
	}
	msg := wsprConfig{
		MounttableRoot: mounttableRoots,
	}

	var buf bytes.Buffer
	if err := vom.ObjToJSON(&buf, vom.ValueOf(msg)); err != nil {
		p.logger.Errorf("failed to convert wspr config to json: %s", err)
		return
	}
	p.writeQueue <- wsMessage{messageType: websocket.TextMessage, buf: buf.Bytes()}
}

func (p *pipe) pingLoop() {
	for {
		time.Sleep(pingInterval)
		p.logger.VI(2).Info("ws: ping")
		p.writeQueue <- wsMessage{messageType: websocket.PingMessage, buf: []byte{}}
	}
}

func (p *pipe) pongHandler(msg string) error {
	p.logger.VI(2).Infof("ws: pong")
	p.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	return nil
}

func (p *pipe) readLoop() {
	p.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	for {
		op, r, err := p.ws.NextReader()
		if err == io.ErrUnexpectedEOF { // websocket disconnected
			break
		}
		if err != nil {
			p.logger.VI(1).Infof("websocket receive: %s", err)
			break
		}

		if op != websocket.TextMessage {
			p.logger.Errorf("unexpected websocket op: %v", op)
		}

		var msg websocketMessage
		decoder := json.NewDecoder(r)
		if err := decoder.Decode(&msg); err != nil {
			errMsg := fmt.Sprintf("can't unmarshall JSONMessage: %v", err)
			p.logger.Error(errMsg)
			p.writeQueue <- wsMessage{messageType: websocket.TextMessage, buf: []byte(errMsg)}
			continue
		}

		ww := p.writerCreator(msg.Id)

		// TODO(mattr): Get the proper context information
		// from javascript.
		ctx := p.wspr.rt.NewContext()

		switch msg.Type {
		case websocketVeyronRequest:
			p.controller.HandleVeyronRequest(ctx, msg.Id, msg.Data, ww)
		case websocketStreamingValue:
			// SendOnStream queues up the message to be sent, but doesn't do the send
			// on this goroutine.  We need to queue the messages synchronously so that
			// the order is preserved.
			p.controller.SendOnStream(msg.Id, msg.Data, ww)
		case websocketStreamClose:
			p.controller.CloseStream(msg.Id)
		case websocketServe:
			go p.controller.HandleServeRequest(msg.Data, ww)
		case websocketStopServer:
			go p.controller.HandleStopRequest(msg.Data, ww)
		case websocketServerResponse:
			go p.controller.HandleServerResponse(msg.Id, msg.Data)
		case websocketSignatureRequest:
			go p.controller.HandleSignatureRequest(ctx, msg.Data, ww)
		case websocketLookupResponse:
			go p.controller.HandleLookupResponse(msg.Id, msg.Data)
		case websocketBlessIdentity:
			go p.controller.HandleBlessing(msg.Data, ww)
		case websocketCreateIdentity:
			go p.controller.HandleCreateIdentity(msg.Data, ww)
		case websocketUnlinkIdentity:
			go p.controller.HandleUnlinkJSIdentity(msg.Data, ww)
		case websocketBlessPublicKey:
			go p.controller.HandleBlessPublicKey(msg.Data, ww)
		case websocketCreateBlessings:
			go p.controller.HandleCreateBlessings(msg.Data, ww)
		case websocketUnlinkBlessings:
			go p.controller.HandleUnlinkJSBlessings(msg.Data, ww)
		case websocketAuthResponse:
			go p.controller.HandleAuthResponse(msg.Id, msg.Data)
		case websocketNamespaceRequest:
			go p.controller.HandleNamespaceRequest(ctx, msg.Data, ww)
		default:
			ww.Error(verror.Unknownf("unknown message type: %v", msg.Type))
		}
	}
	p.cleanup()
}
