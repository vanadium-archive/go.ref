package app

import (
	"veyron.io/veyron/veyron2/context"
	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/wspr/veyron/services/wsprd/lib"
)

const (
	verrorPkgPath = "veyron.io/wspr/veyron/services/wsprd/app"
)

var (
	errUnknownMessageType = verror.Register(verrorPkgPath+".unkownMessage", verror.NoRetry, "{1} {2} Unknown message type {_}")
)

// The type of message sent by the JS client to the wspr.
type messageType int32

const (
	// Making a veyron client request, streaming or otherwise
	veyronRequestMessage messageType = 0

	// Serving this  under an object name
	serveMessage = 1

	// A response from a service in javascript to a request
	// from the proxy.
	serverResponseMessage = 2

	// Sending streaming data, either from a JS client or JS service.
	streamingValueMessage = 3

	// A response that means the stream is closed by the client.
	streamCloseMessage = 4

	// A request to get signature of a remote server
	signatureRequestMessage = 5

	// A request to stop a server
	stopServerMessage = 6

	// A request to bless a public key.
	blessPublicKeyMessage = 7

	// A request to unlink blessings.  This request means that
	// we can remove the given handle from the handle store.
	unlinkBlessingsMessage = 8

	// A request to create a new random blessings
	createBlessingsMessage = 9

	// A request to run the lookup function on a dispatcher.
	lookupResponseMessage = 11

	// A request to run the authorizer for an rpc.
	authResponseMessage = 12

	// A request to run a namespace client method
	namespaceRequestMessage = 13

	// A request to cancel an rpc initiated by the JS.
	cancelMessage = 17

	// A request to add a new name to server.
	websocketAddName = 18

	// A request to remove a name from server.
	websocketRemoveName = 19
)

type Message struct {
	Id int64
	// This contains the json encoded payload.
	Data string

	// Whether it is an rpc request or a serve request.
	Type messageType
}

func (c *Controller) HandleIncomingMessage(ctx context.T, msg Message, w lib.ClientWriter) {
	switch msg.Type {
	case veyronRequestMessage:
		c.HandleVeyronRequest(ctx, msg.Id, msg.Data, w)
	case cancelMessage:
		go c.HandleVeyronCancellation(msg.Id)
	case streamingValueMessage:
		// SendOnStream queues up the message to be sent, but doesn't do the send
		// on this goroutine.  We need to queue the messages synchronously so that
		// the order is preserved.
		c.SendOnStream(msg.Id, msg.Data, w)
	case streamCloseMessage:
		c.CloseStream(msg.Id)
	case serveMessage:
		go c.HandleServeRequest(msg.Data, w)
	case stopServerMessage:
		go c.HandleStopRequest(msg.Data, w)
	case websocketAddName:
		go c.HandleAddNameRequest(msg.Data, w)
	case websocketRemoveName:
		go c.HandleRemoveNameRequest(msg.Data, w)
	case serverResponseMessage:
		go c.HandleServerResponse(msg.Id, msg.Data)
	case signatureRequestMessage:
		go c.HandleSignatureRequest(ctx, msg.Data, w)
	case lookupResponseMessage:
		go c.HandleLookupResponse(msg.Id, msg.Data)
	case blessPublicKeyMessage:
		go c.HandleBlessPublicKey(msg.Data, w)
	case createBlessingsMessage:
		go c.HandleCreateBlessings(msg.Data, w)
	case unlinkBlessingsMessage:
		go c.HandleUnlinkJSBlessings(msg.Data, w)
	case authResponseMessage:
		go c.HandleAuthResponse(msg.Id, msg.Data)
	case namespaceRequestMessage:
		go c.HandleNamespaceRequest(ctx, msg.Data, w)
	default:
		w.Error(verror.Make(errUnknownMessageType, ctx, msg.Type))
	}
}
