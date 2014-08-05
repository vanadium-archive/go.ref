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
	"time"

	"veyron/services/wsprd/app"
	"veyron/services/wsprd/lib"
	"veyron2/security"
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

	writerCreator func(id int64) lib.ClientWriter

	// There is a single write goroutine because ws.NewWriter() creates a new writer that
	// writes to a shared buffer in the websocket, so it is not safe to have multiple go
	// routines writing to different websocket writers.
	writeQueue chan wsMessage

	// This request is used to tell WSPR which pipe to remove when we shutdown.
	req *http.Request
}

func newPipe(w http.ResponseWriter, req *http.Request, wspr *WSPR, creator func(id int64) lib.ClientWriter) *pipe {
	pipe := &pipe{logger: wspr.rt.Logger(), writerCreator: creator, req: req, wspr: wspr}
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
	if p.writerCreator == nil {
		p.writerCreator = func(id int64) lib.ClientWriter {
			return &websocketWriter{p: p, id: id, logger: p.logger}
		}
	}

	p.writeQueue = make(chan wsMessage, 50)
	go p.writeLoop()

	p.controller = app.NewController(p.writerCreator, p.wspr.veyronProxyEP)
	// TODO(bjornick):  Pass in the identity linked to this origin.
	p.controller.UpdateIdentity(nil)
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
			p.controller.HandleVeyronRequest(msg.Id, msg.Data, ww)
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
			go p.controller.HandleSignatureRequest(msg.Data, ww)
		default:
			ww.Error(verror.Unknownf("unknown message type: %v", msg.Type))
		}
	}
	p.cleanup()
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
