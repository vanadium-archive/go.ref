// An implementation of a server for WSPR

package server

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/principal"
	"veyron.io/wspr/veyron/services/wsprd/signature"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom2"
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
	Args     string
	Context  serverRPCRequestContext
}

// call context for a serverRPCRequest
type serverRPCRequestContext struct {
	Suffix                string
	Name                  string
	RemoteBlessings       principal.BlessingsHandle
	RemoteBlessingStrings []string
	Timeout               int64 // The time period (in ns) between now and the deadline.
}

// The response from the javascript server to the proxy.
type serverRPCReply struct {
	Results []interface{}
	Err     *verror2.Standard
}

type FlowHandler interface {
	CreateNewFlow(server *Server, sender ipc.Stream) *Flow

	CleanupFlow(id int64)
}

type HandleStore interface {
	// Adds blessings to the store and returns handle to the blessings
	AddBlessings(blessings security.Blessings) int64
}

type ServerHelper interface {
	FlowHandler
	HandleStore

	GetLogger() vlog.Logger

	RT() veyron2.Runtime
}

type authReply struct {
	Err *verror2.Standard
}

type context struct {
	Method                string                    `json:"method"`
	Name                  string                    `json:"name"`
	Suffix                string                    `json:"suffix"`
	Label                 security.Label            `json:"label"`
	LocalBlessings        principal.BlessingsHandle `json:"localBlessings"`
	LocalBlessingStrings  []string                  `json:"localBlessingStrings"`
	RemoteBlessings       principal.BlessingsHandle `json:"remoteBlessings"`
	RemoteBlessingStrings []string                  `json:"remoteBlessingStrings"`
	LocalEndpoint         string                    `json:"localEndpoint"`
	RemoteEndpoint        string                    `json:"remoteEndpoint"`
}

type authRequest struct {
	ServerID uint64  `json:"serverID"`
	Handle   int64   `json:"handle"`
	Context  context `json:"context"`
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
	id     uint64
	helper ServerHelper

	// The set of outstanding server requests.
	outstandingServerRequests map[int64]chan *serverRPCReply

	outstandingAuthRequests map[int64]chan error
}

func NewServer(id uint64, listenSpec *ipc.ListenSpec, helper ServerHelper) (*Server, error) {
	server := &Server{
		id:                        id,
		helper:                    helper,
		listenSpec:                listenSpec,
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

		timeout := lib.JSIPCNoTimeout
		if deadline, ok := call.Deadline(); ok {
			timeout = lib.GoToJSDuration(deadline.Sub(time.Now()))
		}

		context := serverRPCRequestContext{
			Suffix:                call.Suffix(),
			Name:                  call.Name(),
			Timeout:               timeout,
			RemoteBlessings:       s.convertBlessingsToHandle(call.RemoteBlessings()),
			RemoteBlessingStrings: call.RemoteBlessings().ForContext(call),
		}

		errHandler := func(err error) <-chan *serverRPCReply {
			if ch := s.popServerRequest(flow.ID); ch != nil {
				stdErr := verror2.Convert(verror2.Internal, call, err).(verror2.Standard)
				ch <- &serverRPCReply{nil, &stdErr}
				s.helper.CleanupFlow(flow.ID)
			}
			return replyChan

		}
		var buf bytes.Buffer
		encoder, err := vom2.NewBinaryEncoder(&buf)
		if err != nil {
			return errHandler(err)
		}

		if err := encoder.Encode(args); err != nil {
			return errHandler(err)
		}

		// Send a invocation request to JavaScript
		message := serverRPCRequest{
			ServerId: s.id,
			Handle:   handle,
			Method:   lib.LowercaseFirstCharacter(methodName),
			Args:     hex.EncodeToString(buf.Bytes()),
			Context:  context,
		}

		if err := flow.Writer.Send(lib.ResponseServerRequest, message); err != nil {
			return errHandler(err)
		}

		s.helper.GetLogger().VI(3).Infof("request received to call method %q on "+
			"JavaScript server with args %v, MessageId %d was assigned.",
			methodName, args, flow.ID)

		// Watch for cancellation.
		go func() {
			<-call.Done()
			ch := s.popServerRequest(flow.ID)
			if ch == nil {
				return
			}

			// Send a cancel message to the JS server.
			flow.Writer.Send(lib.ResponseCancel, nil)
			s.helper.CleanupFlow(flow.ID)

			err := verror2.Convert(verror2.Aborted, call, call.Err()).(verror2.Standard)
			ch <- &serverRPCReply{nil, &err}
		}()

		go proxyStream(call, flow.Writer, s.helper.GetLogger())

		return replyChan
	}
}

func proxyStream(stream ipc.Stream, w lib.ClientWriter, logger vlog.Logger) {
	var item interface{}
	for err := stream.Recv(&item); err == nil; err = stream.Recv(&item) {
		var buf bytes.Buffer
		encoder, err := vom2.NewBinaryEncoder(&buf)
		if err != nil {
			w.Error(verror2.Convert(verror2.Internal, nil, err))
			return
		}

		if err := encoder.Encode(item); err != nil {
			w.Error(verror2.Convert(verror2.Internal, nil, err))
			return
		}

		if err := w.Send(lib.ResponseStream, hex.EncodeToString(buf.Bytes())); err != nil {
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
				Method:                lib.LowercaseFirstCharacter(ctx.Method()),
				Name:                  ctx.Name(),
				Suffix:                ctx.Suffix(),
				Label:                 ctx.Label(),
				LocalEndpoint:         ctx.LocalEndpoint().String(),
				RemoteEndpoint:        ctx.RemoteEndpoint().String(),
				LocalBlessings:        s.convertBlessingsToHandle(ctx.LocalBlessings()),
				LocalBlessingStrings:  ctx.LocalBlessings().ForContext(ctx),
				RemoteBlessings:       s.convertBlessingsToHandle(ctx.RemoteBlessings()),
				RemoteBlessingStrings: ctx.RemoteBlessings().ForContext(ctx),
			},
		}
		s.helper.GetLogger().VI(0).Infof("Sending out auth request for %v, %v", flow.ID, message)

		if err := flow.Writer.Send(lib.ResponseAuthRequest, message); err != nil {
			replyChan <- verror2.Convert(verror2.Internal, nil, err)
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

func (s *Server) Serve(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dispatcher == nil {
		s.dispatcher = newDispatcher(s.id, s, s, s, s.helper.GetLogger())
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
	// TODO(nlacasse): This Sleep is necessary to prevent the client from
	// making calls to the name before it has been mounted in the
	// mounttable.  Remove this once
	// https://veyron-review.googlesource.com/#/c/7358/ has landed.
	time.Sleep(1 * time.Second)
	return nil
}

func (s *Server) popServerRequest(id int64) chan *serverRPCReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.outstandingServerRequests[id]
	delete(s.outstandingServerRequests, id)

	return ch
}

func (s *Server) HandleServerResponse(id int64, data string) {
	ch := s.popServerRequest(id)
	if ch == nil {
		s.helper.GetLogger().Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}

	// Decode the result and send it through the channel
	var serverReply serverRPCReply
	if decoderErr := json.Unmarshal([]byte(data), &serverReply); decoderErr != nil {
		err := verror2.Convert(verror2.Internal, nil, decoderErr).(verror2.Standard)
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
	return nil, nil
}

func (s *Server) Stop() {
	stdErr := verror2.Make(verror2.Timeout, nil).(verror2.Standard)
	result := serverRPCReply{
		Results: []interface{}{nil},
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
	s.outstandingServerRequests = make(map[int64]chan *serverRPCReply)
	s.server.Stop()
}

func (s *Server) AddName(name string) error {
	return s.server.AddName(name)
}

func (s *Server) RemoveName(name string) error {
	return s.server.RemoveName(name)
}
