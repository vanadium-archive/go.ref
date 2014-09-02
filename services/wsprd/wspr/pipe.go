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

	"veyron/services/wsprd/app"
	"veyron/services/wsprd/lib"
	"veyron2"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"

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

	// A request to bless an identity
	websocketBlessIdentity = 8

	// A request to unlink an identity.  This request means that
	// we can remove the given handle from the handle store.
	websocketUnlinkIdentity = 9

	// A request to create a new random identity
	websocketCreateIdentity = 10
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

	id, err := wspr.idManager.Identity(origin)

	if err != nil {
		id = wspr.rt.Identity()
		wspr.rt.Logger().Errorf("no identity associated with origin %s: %v", origin, err)
		// TODO(bjornick): Send an error to the client when all of the identity stuff is set up.
	}

	pipe.controller, err = app.NewController(creator, wspr.veyronProxyEP, veyron2.RuntimeID(id))

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
	mounttableRoots := strings.Split(os.Getenv("NAMESPACE_ROOT"), ",")
	if len(mounttableRoots) == 1 && mounttableRoots[0] == "" {
		mounttableRoots = []string{}
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

		switch msg.Type {
		case websocketVeyronRequest:
			// TODO(mattr): Get the proper context information
			// from javascript.
			ctx := p.wspr.rt.NewContext()
			p.controller.HandleVeyronRequest(ctx, msg.Id, msg.Data, ww)
		case websocketStreamingValue:
			// This will asynchronous for a client rpc, but synchronous for a
			// server rpc.  This could be potentially bad if the server is sending
			// back large packets.  Making it asynchronous for the server, would make
			// it difficult to guarantee that all stream messages make it to the client
			// before the finish call.
			// TODO(bjornick): Make the server send also asynchronous.
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
			// TODO(mattr): Get the proper context information
			// from javascript.
			ctx := p.wspr.rt.NewContext()
			go p.controller.HandleSignatureRequest(ctx, msg.Data, ww)
		case websocketBlessIdentity:
			go p.controller.HandleBlessing(msg.Data, ww)
		case websocketCreateIdentity:
			go p.controller.HandleCreateIdentity(msg.Data, ww)
		case websocketUnlinkIdentity:
			go p.controller.HandleUnlinkJSIdentity(msg.Data, ww)
		default:
			ww.Error(verror.Unknownf("unknown message type: %v", msg.Type))
		}
	}
	p.cleanup()
}
