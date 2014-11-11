package app

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"

	"veyron.io/veyron/veyron2/context"
	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
	"veyron.io/wspr/veyron/services/wsprd/lib"
)

const (
	verrorPkgPath = "veyron.io/wspr/veyron/services/wsprd/app"
)

var (
	errUnknownMessageType = verror.Register(verrorPkgPath+".unkownMessage", verror.NoRetry, "{1} {2} Unknown message type {_}")
)

// Incoming message from the javascript client to WSPR.
type MessageType int32

const (
	// Making a veyron client request, streaming or otherwise
	VeyronRequestMessage MessageType = 0

	// Serving this  under an object name
	ServeMessage = 1

	// A response from a service in javascript to a request
	// from the proxy.
	ServerResponseMessage = 2

	// Sending streaming data, either from a JS client or JS service.
	StreamingValueMessage = 3

	// A response that means the stream is closed by the client.
	StreamCloseMessage = 4

	// A request to get signature of a remote server
	SignatureRequestMessage = 5

	// A request to stop a server
	StopServerMessage = 6

	// A request to bless a public key.
	BlessPublicKeyMessage = 7

	// A request to unlink blessings.  This request means that
	// we can remove the given handle from the handle store.
	UnlinkBlessingsMessage = 8

	// A request to create a new random blessings
	CreateBlessingsMessage = 9

	// A request to run the lookup function on a dispatcher.
	LookupResponseMessage = 11

	// A request to run the authorizer for an rpc.
	AuthResponseMessage = 12

	// A request to run a namespace client method
	NamespaceRequestMessage = 13

	// A request to cancel an rpc initiated by the JS.
	CancelMessage = 17

	// A request to add a new name to server.
	WebsocketAddName = 18

	// A request to remove a name from server.
	WebsocketRemoveName = 19
)

type Response struct {
	Type    lib.ResponseType
	Message interface{}
}

type Message struct {
	Id int64
	// This contains the json encoded payload.
	Data string

	// Whether it is an rpc request or a serve request.
	Type MessageType
}

// HandleIncomingMessage handles most incoming messages from JS and calls the appropriate handler.
func (c *Controller) HandleIncomingMessage(ctx context.T, msg Message, w lib.ClientWriter) {
	switch msg.Type {
	case VeyronRequestMessage:
		c.HandleVeyronRequest(ctx, msg.Id, msg.Data, w)
	case CancelMessage:
		go c.HandleVeyronCancellation(msg.Id)
	case StreamingValueMessage:
		// SendOnStream queues up the message to be sent, but doesn't do the send
		// on this goroutine.  We need to queue the messages synchronously so that
		// the order is preserved.
		c.SendOnStream(msg.Id, msg.Data, w)
	case StreamCloseMessage:
		c.CloseStream(msg.Id)
	case ServeMessage:
		go c.HandleServeRequest(msg.Data, w)
	case StopServerMessage:
		go c.HandleStopRequest(msg.Data, w)
	case WebsocketAddName:
		go c.HandleAddNameRequest(msg.Data, w)
	case WebsocketRemoveName:
		go c.HandleRemoveNameRequest(msg.Data, w)
	case ServerResponseMessage:
		go c.HandleServerResponse(msg.Id, msg.Data)
	case SignatureRequestMessage:
		go c.HandleSignatureRequest(ctx, msg.Data, w)
	case LookupResponseMessage:
		go c.HandleLookupResponse(msg.Id, msg.Data)
	case BlessPublicKeyMessage:
		go c.HandleBlessPublicKey(msg.Data, w)
	case CreateBlessingsMessage:
		go c.HandleCreateBlessings(msg.Data, w)
	case UnlinkBlessingsMessage:
		go c.HandleUnlinkJSBlessings(msg.Data, w)
	case AuthResponseMessage:
		go c.HandleAuthResponse(msg.Id, msg.Data)
	case NamespaceRequestMessage:
		go c.HandleNamespaceRequest(ctx, msg.Data, w)
	default:
		w.Error(verror.Make(errUnknownMessageType, ctx, msg.Type))
	}
}

// ConstructOutgoingMessage constructs a message to send to javascript in a consistent format.
// TODO(bprosnitz) Don't double-encode
func ConstructOutgoingMessage(messageId int64, messageType lib.ResponseType, data interface{}) (string, error) {
	var buf bytes.Buffer
	if err := vom.ObjToJSON(&buf, vom.ValueOf(Response{Type: messageType, Message: data})); err != nil {
		return "", err
	}

	var buf2 bytes.Buffer
	if err := vom.ObjToJSON(&buf2, vom.ValueOf(Message{Id: messageId, Data: buf.String()})); err != nil {
		return "", err
	}

	return buf2.String(), nil
}

// FormatAsVerror formats an error as a verror.
// This also logs the error to the given logger.
func FormatAsVerror(err error, logger vlog.Logger) error {
	verr := verror.Convert(verror.Unknown, nil, err)

	// Also log the error but write internal errors at a more severe log level
	var logLevel vlog.Level = 2
	logErr := fmt.Sprintf("%v", verr)

	// Prefix the message with the code locations associated with verr,
	// except the last, which is the Convert() above.  This does nothing if
	// err was not a verror error.
	verrStack := verror.Stack(verr)
	for i := 0; i < len(verrStack)-1; i++ {
		pc := verrStack[i]
		fnc := runtime.FuncForPC(pc)
		file, line := fnc.FileLine(pc)
		logErr = fmt.Sprintf("%s:%d: %s", file, line)
	}

	// We want to look at the stack three frames up to find where the error actually
	// occurred.  (caller -> websocketErrorResponse/sendError -> generateErrorMessage).
	if _, file, line, ok := runtime.Caller(3); ok {
		logErr = fmt.Sprintf("%s:%d: %s", filepath.Base(file), line, logErr)
	}
	if verror.Is(verr, verror.Internal.ID) {
		logLevel = 2
	}
	logger.VI(logLevel).Info(logErr)

	return verr
}
