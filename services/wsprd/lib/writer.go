package lib

type ResponseType int

const (
	ResponseFinal            ResponseType = 0
	ResponseStream                        = 1
	ResponseError                         = 2
	ResponseServerRequest                 = 3
	ResponseStreamClose                   = 4
	ResponseDispatcherLookup              = 5
	ResponseAuthRequest                   = 6
	ResponseCancel                        = 7
)

type Response struct {
	Type    ResponseType
	Message interface{}
}

// This is basically an io.Writer interface, that allows passing error message
// strings.  This is how the proxy will talk to the javascript/java clients.
type ClientWriter interface {
	Send(messageType ResponseType, data interface{}) error

	Error(err error)
}

// The response from the javascript server to the proxy.
type ServerRPCReply struct {
	Results []interface{}
	Err     error
}
