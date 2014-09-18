// An implementation of a server for WSPR

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/services/wsprd/ipc/stream"
	"veyron.io/veyron/veyron/services/wsprd/lib"
	"veyron.io/veyron/veyron/services/wsprd/signature"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

type Flow struct {
	ID     int64
	Writer lib.ClientWriter
}

// A request from the proxy to javascript to handle an RPC
type serverRPCRequest struct {
	ServerId uint64
	Method   string
	Args     []interface{}
	Context  serverRPCRequestContext
}

type publicID struct {
	Handle int64
	Names  []string
}

// call context for a serverRPCRequest
type serverRPCRequestContext struct {
	Suffix   string
	Name     string
	RemoteID publicID
}

// The response from the javascript server to the proxy.
type serverRPCReply struct {
	Results []interface{}
	Err     *verror.Standard
}

type FlowHandler interface {
	CreateNewFlow(server *Server, sender stream.Sender) *Flow

	CleanupFlow(id int64)
}

type HandleStore interface {
	// Adds an identity to the store and returns handle to the identity
	AddIdentity(identity security.PublicID) int64
}

type ServerHelper interface {
	FlowHandler
	HandleStore

	GetLogger() vlog.Logger

	RT() veyron2.Runtime
}

type Server struct {
	sync.Mutex

	// The server that handles the ipc layer.  Listen on this server is
	// lazily started.
	server ipc.Server

	// The saved dispatcher to reuse when serve is called multiple times.
	dispatcher ipc.Dispatcher

	// The endpoint of the server.  This is empty until the server has been
	// started and listen has been called on it.
	endpoint string

	// The server id.
	id     uint64
	helper ServerHelper

	// The proxy to listen through.
	veyronProxy string

	// The set of outstanding server requests.
	outstandingServerRequests map[int64]chan *serverRPCReply
}

func NewServer(id uint64, veyronProxy string, helper ServerHelper) (*Server, error) {
	server := &Server{
		id:                        id,
		helper:                    helper,
		veyronProxy:               veyronProxy,
		outstandingServerRequests: make(map[int64]chan *serverRPCReply),
	}
	var err error
	if server.server, err = helper.RT().NewServer(); err != nil {
		return nil, err
	}
	return server, nil
}

// remoteInvokeFunc is a type of function that can invoke a remote method and
// communicate the result back via a channel to the caller
type remoteInvokeFunc func(methodName string, args []interface{}, call ipc.ServerCall) <-chan *serverRPCReply

func (s *Server) createRemoteInvokerFunc() remoteInvokeFunc {
	return func(methodName string, args []interface{}, call ipc.ServerCall) <-chan *serverRPCReply {
		flow := s.helper.CreateNewFlow(s, senderWrapper{stream: call})
		replyChan := make(chan *serverRPCReply, 1)
		s.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.Unlock()
		remoteID := call.RemoteID()
		context := serverRPCRequestContext{
			Suffix: call.Suffix(),
			Name:   call.Name(),
			RemoteID: publicID{
				Handle: s.helper.AddIdentity(remoteID),
				Names:  remoteID.Names(),
			},
		}
		// Send a invocation request to JavaScript
		message := serverRPCRequest{
			ServerId: s.id,
			Method:   lib.LowercaseFirstCharacter(methodName),
			Args:     args,
			Context:  context,
		}

		if err := flow.Writer.Send(lib.ResponseServerRequest, message); err != nil {
			// Error in marshaling, pass the error through the channel immediately
			replyChan <- &serverRPCReply{nil,
				&verror.Standard{
					ID:  verror.Internal,
					Msg: fmt.Sprintf("could not marshal the method call data: %v", err)},
			}
			return replyChan
		}

		s.helper.GetLogger().VI(3).Infof("request received to call method %q on "+
			"JavaScript server with args %v, MessageId %d was assigned.",
			methodName, args, flow.ID)

		go proxyStream(call, flow.Writer, s.helper.GetLogger())
		return replyChan
	}
}

func proxyStream(stream ipc.Stream, w lib.ClientWriter, logger vlog.Logger) {
	var item interface{}
	for err := stream.Recv(&item); err == nil; err = stream.Recv(&item) {
		if err := w.Send(lib.ResponseStream, item); err != nil {
			w.Error(verror.Internalf("error marshalling stream: %v:", err))
			return
		}
	}

	if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
		w.Error(verror.Internalf("error closing stream: %v:", err))
		return
	}
}

func (s *Server) Serve(name string, sig signature.JSONServiceSignature) (string, error) {
	s.Lock()
	defer s.Unlock()

	serviceSig, err := sig.ServiceSignature()
	if err != nil {
		return "", err
	}

	remoteInvokeFunc := s.createRemoteInvokerFunc()
	invoker, err := newInvoker(serviceSig, remoteInvokeFunc)

	if err != nil {
		return "", err
	}

	if s.dispatcher == nil {
		s.dispatcher = newDispatcher(invoker,
			vsecurity.NewACLAuthorizer(security.ACL{In: map[security.BlessingPattern]security.LabelSet{
				security.AllPrincipals: security.AllLabels,
			}}))
	}

	if s.endpoint == "" {
		endpoint, err := s.server.Listen("veyron", s.veyronProxy)

		if err != nil {
			return "", err
		}
		s.endpoint = endpoint.String()
	}
	if err := s.server.Serve(name, s.dispatcher); err != nil {
		return "", err
	}
	s.helper.GetLogger().VI(1).Infof("endpoint is %s", s.endpoint)
	return s.endpoint, nil
}

func (s *Server) HandleServerResponse(id int64, data string) {
	s.Lock()
	ch := s.outstandingServerRequests[id]
	delete(s.outstandingServerRequests, id)
	s.Unlock()
	if ch == nil {
		s.helper.GetLogger().Errorf("unexpected result from JavaScript. No channel "+
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

	s.helper.GetLogger().VI(3).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, serverReply)
	s.helper.CleanupFlow(id)
	ch <- &serverReply
}

func (s *Server) Stop() {
	result := serverRPCReply{
		Results: []interface{}{nil},
		Err: &verror.Standard{
			ID:  verror.Aborted,
			Msg: "timeout",
		},
	}
	s.Lock()
	defer s.Unlock()
	for _, ch := range s.outstandingServerRequests {
		select {
		case ch <- &result:
		default:
		}
	}
	s.outstandingServerRequests = make(map[int64]chan *serverRPCReply)
	s.server.Stop()
}
