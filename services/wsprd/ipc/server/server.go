// An implementation of a server for WSPR

package server

import (
	"encoding/json"
	"fmt"
	"sync"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/services/wsprd/lib"
	"veyron.io/veyron/veyron/services/wsprd/signature"
)

type Flow struct {
	ID     int64
	Writer lib.ClientWriter
}

// A request from the proxy to javascript to handle an RPC
type serverRPCRequest struct {
	ServerId uint64
	Handle   int64
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
	CreateNewFlow(server *Server, sender ipc.Stream) *Flow

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

type authReply struct {
	Err *verror.Standard
}

type context struct {
	Method         string         `json:"method"`
	Name           string         `json:"name"`
	Suffix         string         `json:"suffix"`
	Label          security.Label `json:"label"`
	LocalID        publicID       `json:"localId"`
	RemoteID       publicID       `json:"remoteId"`
	LocalEndpoint  string         `json:"localEndpoint"`
	RemoteEndpoint string         `json:"remoteEndpoint"`
}

type authRequest struct {
	ServerID uint64  `json:"serverID"`
	Handle   int64   `json:"handle"`
	Context  context `json:"context"`
}

type Server struct {
	mu sync.Mutex

	// The server that handles the ipc layer.  Listen on this server is
	// lazily started.
	server ipc.Server

	// The saved dispatcher to reuse when serve is called multiple times.
	dispatcher *dispatcher

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

	outstandingAuthRequests map[int64]chan error
}

func NewServer(id uint64, veyronProxy string, helper ServerHelper) (*Server, error) {
	server := &Server{
		id:                        id,
		helper:                    helper,
		veyronProxy:               veyronProxy,
		outstandingServerRequests: make(map[int64]chan *serverRPCReply),
		outstandingAuthRequests:   make(map[int64]chan error),
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

func (s *Server) createRemoteInvokerFunc(handle int64) remoteInvokeFunc {
	return func(methodName string, args []interface{}, call ipc.ServerCall) <-chan *serverRPCReply {
		flow := s.helper.CreateNewFlow(s, call)
		replyChan := make(chan *serverRPCReply, 1)
		s.mu.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.mu.Unlock()
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
			Handle:   handle,
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

func (s *Server) convertPublicID(id security.PublicID) publicID {
	return publicID{
		Handle: s.helper.AddIdentity(id),
		Names:  id.Names(),
	}

}

type remoteAuthFunc func(security.Context) error

func (s *Server) createRemoteAuthFunc(handle int64) remoteAuthFunc {
	return func(ctx security.Context) error {
		flow := s.helper.CreateNewFlow(s, nil)
		replyChan := make(chan error, 1)
		s.mu.Lock()
		s.outstandingAuthRequests[flow.ID] = replyChan
		s.mu.Unlock()
		message := authRequest{
			ServerID: s.id,
			Handle:   handle,
			Context: context{
				Method:         lib.LowercaseFirstCharacter(ctx.Method()),
				Name:           ctx.Name(),
				Suffix:         ctx.Suffix(),
				Label:          ctx.Label(),
				LocalID:        s.convertPublicID(ctx.LocalID()),
				RemoteID:       s.convertPublicID(ctx.RemoteID()),
				LocalEndpoint:  ctx.LocalEndpoint().String(),
				RemoteEndpoint: ctx.RemoteEndpoint().String(),
			},
		}
		s.helper.GetLogger().VI(0).Infof("Sending out auth request for %v, %v", flow.ID, message)

		if err := flow.Writer.Send(lib.ResponseAuthRequest, message); err != nil {
			replyChan <- verror.Internalf("failed to find authorizer %v", err)
		}

		err := <-replyChan
		s.helper.GetLogger().VI(0).Infof("going to respond with %v", err)
		s.mu.Lock()
		delete(s.outstandingAuthRequests, flow.ID)
		s.mu.Unlock()
		s.helper.CleanupFlow(flow.ID)
		return err
	}
}

func (s *Server) Serve(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dispatcher == nil {
		s.dispatcher = newDispatcher(s.id, s, s, s, s.helper.GetLogger())
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
	s.mu.Lock()
	ch := s.outstandingServerRequests[id]
	delete(s.outstandingServerRequests, id)
	s.mu.Unlock()
	if ch == nil {
		s.helper.GetLogger().Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	// Decode the result and send it through the channel
	var serverReply serverRPCReply
	if decoderErr := json.Unmarshal([]byte(data), &serverReply); decoderErr != nil {
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

func (s *Server) HandleLookupResponse(id int64, data string) {
	s.dispatcher.handleLookupResponse(id, data)
}

func (s *Server) HandleAuthResponse(id int64, data string) {
	s.mu.Lock()
	ch := s.outstandingAuthRequests[id]
	s.mu.Unlock()
	if ch == nil {
		s.helper.GetLogger().Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	// Decode the result and send it through the channel
	var reply authReply
	if decoderErr := json.Unmarshal([]byte(data), &reply); decoderErr != nil {
		reply = authReply{Err: &verror.Standard{
			ID:  verror.Internal,
			Msg: fmt.Sprintf("could not unmarshal the result from the server: %v", decoderErr),
		}}
	}

	s.helper.GetLogger().VI(0).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, reply)
	s.helper.CleanupFlow(id)
	// A nil verror.Standard does not result in an nil error.  Instead, we have create
	// a variable for the error interface and only set it's value if the struct is non-
	// nil.
	var err error
	if reply.Err != nil {
		err = reply.Err
	}
	ch <- err
}

func (s *Server) createFlow() *Flow {
	return s.helper.CreateNewFlow(s, nil)
}

func (s *Server) cleanupFlow(id int64) {
	s.helper.CleanupFlow(id)
}

func (s *Server) createInvoker(handle int64, sig signature.JSONServiceSignature, label security.Label) (ipc.Invoker, error) {
	serviceSig, err := sig.ServiceSignature()
	if err != nil {
		return nil, err
	}

	remoteInvokeFunc := s.createRemoteInvokerFunc(handle)
	return newInvoker(serviceSig, label, remoteInvokeFunc), nil
}

func (s *Server) createAuthorizer(handle int64, hasAuthorizer bool) (security.Authorizer, error) {
	if hasAuthorizer {
		return &authorizer{authFunc: s.createRemoteAuthFunc(handle)}, nil
	}
	return vsecurity.NewACLAuthorizer(security.ACL{In: map[security.BlessingPattern]security.LabelSet{
		security.AllPrincipals: security.AllLabels,
	}}), nil
}

func (s *Server) Stop() {
	result := serverRPCReply{
		Results: []interface{}{nil},
		Err: &verror.Standard{
			ID:  verror.Aborted,
			Msg: "timeout",
		},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.outstandingServerRequests {
		select {
		case ch <- &result:
		default:
		}
	}
	s.outstandingServerRequests = make(map[int64]chan *serverRPCReply)
	s.server.Stop()
}
