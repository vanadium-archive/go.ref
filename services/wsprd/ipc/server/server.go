// An implementation of a server for WSPR

package server

import (
	"encoding/json"
	"sync"
	"time"

	"v.io/wspr/veyron/services/wsprd/lib"
	"v.io/wspr/veyron/services/wsprd/principal"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl/vdlroot/src/signature"
	"v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
)

type Flow struct {
	ID     int32
	Writer lib.ClientWriter
}

// A request from the proxy to javascript to handle an RPC
type ServerRPCRequest struct {
	ServerId uint32
	Handle   int32
	Method   string
	Args     []interface{}
	Context  ServerRPCRequestContext
}

type ServerRPCRequestContext struct {
	SecurityContext SecurityContext
	Timeout         int64 // The time period (in ns) between now and the deadline.
}

type FlowHandler interface {
	CreateNewFlow(server *Server, sender ipc.Stream) *Flow

	CleanupFlow(id int32)
}

type HandleStore interface {
	// Adds blessings to the store and returns handle to the blessings
	AddBlessings(blessings security.Blessings) int32
}

type ServerHelper interface {
	FlowHandler
	HandleStore

	Context() *context.T
}

type authReply struct {
	Err *verror2.Standard
}

// Contex is the security context passed to Javascript.
// This is exported to make the app test easier.
type SecurityContext struct {
	Method                string
	Name                  string
	Suffix                string
	MethodTags            []interface{}
	LocalBlessings        principal.BlessingsHandle
	LocalBlessingStrings  []string
	RemoteBlessings       principal.BlessingsHandle
	RemoteBlessingStrings []string
	LocalEndpoint         string
	RemoteEndpoint        string
}

// AuthRequest is a request for a javascript authorizer to run
// This is exported to make the app test easier.
type AuthRequest struct {
	ServerID uint32          `json:"serverID"`
	Handle   int32           `json:"handle"`
	Context  SecurityContext `json:"context"`
}

type Server struct {
	mu sync.Mutex

	// The ipc.ListenSpec to use with server.Listen
	listenSpec *ipc.ListenSpec

	// The server that handles the ipc layer.  Listen on this server is
	// lazily started.
	server ipc.Server

	// The saved dispatcher to reuse when serve is called multiple times.
	dispatcher *dispatcher

	// Whether the server is listening.
	isListening bool

	// The server id.
	id     uint32
	helper ServerHelper

	// The set of outstanding server requests.
	outstandingServerRequests map[int32]chan *lib.ServerRPCReply

	outstandingAuthRequests map[int32]chan error
}

func NewServer(id uint32, listenSpec *ipc.ListenSpec, helper ServerHelper) (*Server, error) {
	server := &Server{
		id:                        id,
		helper:                    helper,
		listenSpec:                listenSpec,
		outstandingServerRequests: make(map[int32]chan *lib.ServerRPCReply),
		outstandingAuthRequests:   make(map[int32]chan error),
	}
	var err error
	if server.server, err = veyron2.NewServer(helper.Context()); err != nil {
		return nil, err
	}
	return server, nil
}

// remoteInvokeFunc is a type of function that can invoke a remote method and
// communicate the result back via a channel to the caller
type remoteInvokeFunc func(methodName string, args []interface{}, call ipc.ServerCall) <-chan *lib.ServerRPCReply

func (s *Server) createRemoteInvokerFunc(handle int32) remoteInvokeFunc {
	return func(methodName string, args []interface{}, call ipc.ServerCall) <-chan *lib.ServerRPCReply {
		flow := s.helper.CreateNewFlow(s, call)
		replyChan := make(chan *lib.ServerRPCReply, 1)
		s.mu.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.mu.Unlock()

		timeout := lib.JSIPCNoTimeout
		if deadline, ok := call.Context().Deadline(); ok {
			timeout = lib.GoToJSDuration(deadline.Sub(time.Now()))
		}

		errHandler := func(err error) <-chan *lib.ServerRPCReply {
			if ch := s.popServerRequest(flow.ID); ch != nil {
				stdErr := verror2.Convert(verror2.Internal, call.Context(), err).(verror2.Standard)
				ch <- &lib.ServerRPCReply{nil, &stdErr}
				s.helper.CleanupFlow(flow.ID)
			}
			return replyChan

		}

		context := ServerRPCRequestContext{
			SecurityContext: s.convertSecurityContext(call),
			Timeout:         timeout,
		}

		// Send a invocation request to JavaScript
		message := ServerRPCRequest{
			ServerId: s.id,
			Handle:   handle,
			Method:   lib.LowercaseFirstCharacter(methodName),
			Args:     args,
			Context:  context,
		}
		vomMessage, err := lib.VomEncode(message)
		if err != nil {
			return errHandler(err)
		}
		if err := flow.Writer.Send(lib.ResponseServerRequest, vomMessage); err != nil {
			return errHandler(err)
		}

		vlog.VI(3).Infof("calling method %q with args %v, MessageID %d assigned\n", methodName, args, flow.ID)

		// Watch for cancellation.
		go func() {
			<-call.Context().Done()
			ch := s.popServerRequest(flow.ID)
			if ch == nil {
				return
			}

			// Send a cancel message to the JS server.
			flow.Writer.Send(lib.ResponseCancel, nil)
			s.helper.CleanupFlow(flow.ID)

			err := verror2.Convert(verror2.Aborted, call.Context(), call.Context().Err()).(verror2.Standard)
			ch <- &lib.ServerRPCReply{nil, &err}
		}()

		go proxyStream(call, flow.Writer)

		return replyChan
	}
}

type globStream struct {
	ch  chan naming.VDLMountEntry
	ctx *context.T
}

func (g *globStream) Send(item interface{}) error {
	if v, ok := item.(naming.VDLMountEntry); ok {
		g.ch <- v
		return nil
	}
	return verror2.Make(verror2.BadArg, g.ctx, item)
}

func (g *globStream) Recv(itemptr interface{}) error {
	return verror2.Make(verror2.NoExist, g.ctx, "Can't call recieve on glob stream")
}

func (g *globStream) CloseSend() error {
	close(g.ch)
	return nil
}

// remoteGlobFunc is a type of function that can invoke a remote glob and
// communicate the result back via the channel returned
type remoteGlobFunc func(pattern string, call ipc.ServerContext) (<-chan naming.VDLMountEntry, error)

func (s *Server) createRemoteGlobFunc(handle int32) remoteGlobFunc {
	return func(pattern string, call ipc.ServerContext) (<-chan naming.VDLMountEntry, error) {
		globChan := make(chan naming.VDLMountEntry, 1)
		flow := s.helper.CreateNewFlow(s, &globStream{
			ch:  globChan,
			ctx: call.Context(),
		})
		replyChan := make(chan *lib.ServerRPCReply, 1)
		s.mu.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.mu.Unlock()

		timeout := lib.JSIPCNoTimeout
		if deadline, ok := call.Context().Deadline(); ok {
			timeout = lib.GoToJSDuration(deadline.Sub(time.Now()))
		}

		errHandler := func(err error) (<-chan naming.VDLMountEntry, error) {
			if ch := s.popServerRequest(flow.ID); ch != nil {
				s.helper.CleanupFlow(flow.ID)
			}
			return nil, verror2.Convert(verror2.Internal, call.Context(), err).(verror2.Standard)
		}

		context := ServerRPCRequestContext{
			SecurityContext: s.convertSecurityContext(call),
			Timeout:         timeout,
		}

		// Send a invocation request to JavaScript
		message := ServerRPCRequest{
			ServerId: s.id,
			Handle:   handle,
			Method:   "Glob__",
			Args:     []interface{}{pattern},
			Context:  context,
		}
		vomMessage, err := lib.VomEncode(message)
		if err != nil {
			return errHandler(err)
		}
		if err := flow.Writer.Send(lib.ResponseServerRequest, vomMessage); err != nil {
			return errHandler(err)
		}

		vlog.VI(3).Infof("calling method 'Glob__' with args %v, MessageID %d assigned\n", []interface{}{pattern}, flow.ID)

		// Watch for cancellation.
		go func() {
			<-call.Context().Done()
			ch := s.popServerRequest(flow.ID)
			if ch == nil {
				return
			}

			// Send a cancel message to the JS server.
			flow.Writer.Send(lib.ResponseCancel, nil)
			s.helper.CleanupFlow(flow.ID)

			err := verror2.Convert(verror2.Aborted, call.Context(), call.Context().Err()).(verror2.Standard)
			ch <- &lib.ServerRPCReply{nil, &err}
		}()

		return globChan, nil
	}
}

func proxyStream(stream ipc.Stream, w lib.ClientWriter) {
	var item interface{}
	for err := stream.Recv(&item); err == nil; err = stream.Recv(&item) {
		vomItem, err := lib.VomEncode(item)
		if err != nil {
			w.Error(verror2.Convert(verror2.Internal, nil, err))
			return
		}
		if err := w.Send(lib.ResponseStream, vomItem); err != nil {
			w.Error(verror2.Convert(verror2.Internal, nil, err))
			return
		}
	}
	if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
		w.Error(verror2.Convert(verror2.Internal, nil, err))
		return
	}
}

func (s *Server) convertBlessingsToHandle(blessings security.Blessings) principal.BlessingsHandle {
	return *principal.ConvertBlessingsToHandle(blessings, s.helper.AddBlessings(blessings))
}

func (s *Server) convertSecurityContext(ctx security.Context) SecurityContext {
	return SecurityContext{
		Method:                lib.LowercaseFirstCharacter(ctx.Method()),
		Name:                  ctx.Name(),
		Suffix:                ctx.Suffix(),
		MethodTags:            ctx.MethodTags(),
		LocalEndpoint:         ctx.LocalEndpoint().String(),
		RemoteEndpoint:        ctx.RemoteEndpoint().String(),
		LocalBlessings:        s.convertBlessingsToHandle(ctx.LocalBlessings()),
		LocalBlessingStrings:  ctx.LocalBlessings().ForContext(ctx),
		RemoteBlessings:       s.convertBlessingsToHandle(ctx.RemoteBlessings()),
		RemoteBlessingStrings: ctx.RemoteBlessings().ForContext(ctx),
	}
}

type remoteAuthFunc func(ctx security.Context) error

func (s *Server) createRemoteAuthFunc(handle int32) remoteAuthFunc {
	return func(ctx security.Context) error {
		flow := s.helper.CreateNewFlow(s, nil)
		replyChan := make(chan error, 1)
		s.mu.Lock()
		s.outstandingAuthRequests[flow.ID] = replyChan
		s.mu.Unlock()
		message := AuthRequest{
			ServerID: s.id,
			Handle:   handle,
			Context:  s.convertSecurityContext(ctx),
		}
		vlog.VI(0).Infof("Sending out auth request for %v, %v", flow.ID, message)

		vomMessage, err := lib.VomEncode(message)
		if err != nil {
			replyChan <- verror2.Convert(verror2.Internal, nil, err)
		} else if err := flow.Writer.Send(lib.ResponseAuthRequest, vomMessage); err != nil {
			replyChan <- verror2.Convert(verror2.Internal, nil, err)
		}

		err = <-replyChan
		vlog.VI(0).Infof("going to respond with %v", err)
		s.mu.Lock()
		delete(s.outstandingAuthRequests, flow.ID)
		s.mu.Unlock()
		s.helper.CleanupFlow(flow.ID)
		return err
	}
}

func (s *Server) Serve(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dispatcher == nil {
		s.dispatcher = newDispatcher(s.id, s, s, s)
	}

	if !s.isListening {
		_, err := s.server.Listen(*s.listenSpec)
		if err != nil {
			return err
		}
		s.isListening = true
	}
	if err := s.server.ServeDispatcher(name, s.dispatcher); err != nil {
		return err
	}
	return nil
}

func (s *Server) popServerRequest(id int32) chan *lib.ServerRPCReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.outstandingServerRequests[id]
	delete(s.outstandingServerRequests, id)

	return ch
}

func (s *Server) HandleServerResponse(id int32, data string) {
	ch := s.popServerRequest(id)
	if ch == nil {
		vlog.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		// Ignore unknown responses that don't belong to any channel
		return
	}

	// Decode the result and send it through the channel
	var reply lib.ServerRPCReply
	if err := lib.VomDecode(data, &reply); err != nil {
		reply.Err = err
	}

	vlog.VI(0).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, reply)
	s.helper.CleanupFlow(id)
	ch <- &reply
}

func (s *Server) HandleLookupResponse(id int32, data string) {
	s.dispatcher.handleLookupResponse(id, data)
}

func (s *Server) HandleAuthResponse(id int32, data string) {
	s.mu.Lock()
	ch := s.outstandingAuthRequests[id]
	s.mu.Unlock()
	if ch == nil {
		vlog.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results(%s)", id, data)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	// Decode the result and send it through the channel
	var reply authReply
	if decoderErr := json.Unmarshal([]byte(data), &reply); decoderErr != nil {
		err := verror2.Convert(verror2.Internal, nil, decoderErr).(verror2.Standard)
		reply = authReply{Err: &err}
	}

	vlog.VI(0).Infof("response received from JavaScript server for "+
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

func (s *Server) cleanupFlow(id int32) {
	s.helper.CleanupFlow(id)
}

func (s *Server) createInvoker(handle int32, sig []signature.Interface, hasGlobber bool) (ipc.Invoker, error) {
	remoteInvokeFunc := s.createRemoteInvokerFunc(handle)
	var globFunc remoteGlobFunc
	if hasGlobber {
		globFunc = s.createRemoteGlobFunc(handle)
	}
	return newInvoker(sig, remoteInvokeFunc, globFunc), nil
}

func (s *Server) createAuthorizer(handle int32, hasAuthorizer bool) (security.Authorizer, error) {
	if hasAuthorizer {
		return &authorizer{authFunc: s.createRemoteAuthFunc(handle)}, nil
	}
	return nil, nil
}

func (s *Server) Stop() {
	stdErr := verror2.Make(verror2.Timeout, nil).(verror2.Standard)
	result := lib.ServerRPCReply{
		Results: nil,
		Err:     &stdErr,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.outstandingServerRequests {
		select {
		case ch <- &result:
		default:
		}
	}
	s.outstandingServerRequests = make(map[int32]chan *lib.ServerRPCReply)
	s.server.Stop()
}

func (s *Server) AddName(name string) error {
	return s.server.AddName(name)
}

func (s *Server) RemoveName(name string) {
	s.server.RemoveName(name)
}
