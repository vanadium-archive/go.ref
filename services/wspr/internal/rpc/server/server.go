// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// An implementation of a server for WSPR

package server

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	vdltime "v.io/v23/vdlroot/time"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/wspr/internal/lib"
	"v.io/x/ref/services/wspr/internal/principal"
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
	Call     ServerRPCRequestCall
}

type ServerRPCRequestCall struct {
	SecurityCall SecurityCall
	Deadline     vdltime.Deadline
	TraceRequest vtrace.Request
}

type FlowHandler interface {
	CreateNewFlow(server *Server, sender rpc.Stream) *Flow

	CleanupFlow(id int32)
}

type HandleStore interface {
	GetBlessings(handle principal.BlessingsHandle) security.Blessings
	// Gets or adds blessings to the store and returns handle to the blessings
	GetOrAddBlessingsHandle(blessings security.Blessings) principal.BlessingsHandle
}

type ServerHelper interface {
	FlowHandler
	HandleStore

	SendLogMessage(level lib.LogLevel, msg string) error

	Context() *context.T
}

type authReply struct {
	Err *verror.E
}

// AuthRequest is a request for a javascript authorizer to run
// This is exported to make the app test easier.
type AuthRequest struct {
	ServerId uint32       `json:"serverId"`
	Handle   int32        `json:"handle"`
	Call     SecurityCall `json:"call"`
}

type Server struct {
	// serverStateLock should be aquired when starting or stopping the server.
	// This should be locked before outstandingRequestLock.
	serverStateLock sync.Mutex

	// The rpc.ListenSpec to use with server.Listen
	listenSpec *rpc.ListenSpec

	// The server that handles the rpc layer.  Listen on this server is
	// lazily started.
	server rpc.Server

	// The saved dispatcher to reuse when serve is called multiple times.
	dispatcher *dispatcher

	// Whether the server is listening.
	isListening bool

	// The server id.
	id     uint32
	helper ServerHelper

	// outstandingRequestLock should be acquired only to update the outstanding request maps below.
	outstandingRequestLock        sync.Mutex
	outstandingServerRequests     map[int32]chan *lib.ServerRpcReply // GUARDED_BY outstandingRequestLock
	outstandingAuthRequests       map[int32]chan error               // GUARDED_BY outstandingRequestLock
	outstandingValidationRequests map[int32]chan []error             // GUARDED_BY outstandingRequestLock

	// statusClose will be closed when the server is shutting down, this will
	// cause the status poller to exit.
	statusClose chan struct{}
}

func NewServer(id uint32, listenSpec *rpc.ListenSpec, helper ServerHelper) (*Server, error) {
	server := &Server{
		id:                            id,
		helper:                        helper,
		listenSpec:                    listenSpec,
		outstandingServerRequests:     make(map[int32]chan *lib.ServerRpcReply),
		outstandingAuthRequests:       make(map[int32]chan error),
		outstandingValidationRequests: make(map[int32]chan []error),
	}
	var err error
	ctx := helper.Context()
	ctx = context.WithValue(ctx, "customChainValidator", server.wsprCaveatValidator)
	if server.server, err = v23.NewServer(ctx); err != nil {
		return nil, err
	}
	return server, nil
}

// remoteInvokeFunc is a type of function that can invoke a remote method and
// communicate the result back via a channel to the caller
type remoteInvokeFunc func(methodName string, args []interface{}, call rpc.StreamServerCall) <-chan *lib.ServerRpcReply

func (s *Server) createRemoteInvokerFunc(handle int32) remoteInvokeFunc {
	return func(methodName string, args []interface{}, call rpc.StreamServerCall) <-chan *lib.ServerRpcReply {
		securityCall := s.convertSecurityCall(call.Context(), true)

		flow := s.helper.CreateNewFlow(s, call)
		replyChan := make(chan *lib.ServerRpcReply, 1)
		s.outstandingRequestLock.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.outstandingRequestLock.Unlock()

		var timeout vdltime.Deadline
		if deadline, ok := call.Context().Deadline(); ok {
			timeout.Time = deadline
		}

		errHandler := func(err error) <-chan *lib.ServerRpcReply {
			if ch := s.popServerRequest(flow.ID); ch != nil {
				stdErr := verror.Convert(verror.ErrInternal, call.Context(), err).(verror.E)
				ch <- &lib.ServerRpcReply{nil, &stdErr, vtrace.Response{}}
				s.helper.CleanupFlow(flow.ID)
			}
			return replyChan
		}

		rpcCall := ServerRPCRequestCall{
			SecurityCall: securityCall,
			Deadline:     timeout,
			TraceRequest: vtrace.GetRequest(call.Context()),
		}
		var convertedArgs = make([]interface{}, 0)
		for _, arg := range args {
			if blessings, ok := arg.(security.Blessings); ok {
				arg = principal.ConvertBlessingsToHandle(blessings, s.helper.GetOrAddBlessingsHandle(blessings))
			}
			convertedArgs = append(convertedArgs, arg)
		}
		// Send a invocation request to JavaScript
		message := ServerRPCRequest{
			ServerId: s.id,
			Handle:   handle,
			Method:   lib.LowercaseFirstCharacter(methodName),
			Args:     convertedArgs,
			Call:     rpcCall,
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

			err := verror.Convert(verror.ErrAborted, call.Context(), call.Context().Err()).(verror.E)
			ch <- &lib.ServerRpcReply{nil, &err, vtrace.Response{}}
		}()

		go proxyStream(call, flow.Writer, s.helper)

		return replyChan
	}
}

type globStream struct {
	ch  chan naming.GlobReply
	ctx *context.T
}

func (g *globStream) Send(item interface{}) error {
	if v, ok := item.(naming.GlobReply); ok {
		g.ch <- v
		return nil
	}
	return verror.New(verror.ErrBadArg, g.ctx, item)
}

func (g *globStream) Recv(itemptr interface{}) error {
	return verror.New(verror.ErrNoExist, g.ctx, "Can't call recieve on glob stream")
}

func (g *globStream) CloseSend() error {
	close(g.ch)
	return nil
}

// remoteGlobFunc is a type of function that can invoke a remote glob and
// communicate the result back via the channel returned
type remoteGlobFunc func(pattern string, call rpc.ServerCall) (<-chan naming.GlobReply, error)

func (s *Server) createRemoteGlobFunc(handle int32) remoteGlobFunc {
	return func(pattern string, call rpc.ServerCall) (<-chan naming.GlobReply, error) {
		// Until the tests get fixed, we need to create a security context before creating the flow
		// because creating the security context creates a flow and flow ids will be off.
		// See https://github.com/veyron/release-issues/issues/1181
		securityCall := s.convertSecurityCall(call.Context(), true)

		globChan := make(chan naming.GlobReply, 1)
		flow := s.helper.CreateNewFlow(s, &globStream{
			ch:  globChan,
			ctx: call.Context(),
		})
		replyChan := make(chan *lib.ServerRpcReply, 1)
		s.outstandingRequestLock.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.outstandingRequestLock.Unlock()

		var timeout vdltime.Deadline
		if deadline, ok := call.Context().Deadline(); ok {
			timeout.Time = deadline
		}

		errHandler := func(err error) (<-chan naming.GlobReply, error) {
			if ch := s.popServerRequest(flow.ID); ch != nil {
				s.helper.CleanupFlow(flow.ID)
			}
			return nil, verror.Convert(verror.ErrInternal, call.Context(), err).(verror.E)
		}

		rpcCall := ServerRPCRequestCall{
			SecurityCall: securityCall,
			Deadline:     timeout,
		}

		// Send a invocation request to JavaScript
		message := ServerRPCRequest{
			ServerId: s.id,
			Handle:   handle,
			Method:   "Glob__",
			Args:     []interface{}{pattern},
			Call:     rpcCall,
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

			err := verror.Convert(verror.ErrAborted, call.Context(), call.Context().Err()).(verror.E)
			ch <- &lib.ServerRpcReply{nil, &err, vtrace.Response{}}
		}()

		return globChan, nil
	}
}

func proxyStream(stream rpc.Stream, w lib.ClientWriter, blessingsCache HandleStore) {
	var item interface{}
	for err := stream.Recv(&item); err == nil; err = stream.Recv(&item) {
		if blessings, ok := item.(security.Blessings); ok {
			item = principal.ConvertBlessingsToHandle(blessings, blessingsCache.GetOrAddBlessingsHandle(blessings))

		}
		vomItem, err := lib.VomEncode(item)
		if err != nil {
			w.Error(verror.Convert(verror.ErrInternal, nil, err))
			return
		}
		if err := w.Send(lib.ResponseStream, vomItem); err != nil {
			w.Error(verror.Convert(verror.ErrInternal, nil, err))
			return
		}
	}
	if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
		w.Error(verror.Convert(verror.ErrInternal, nil, err))
		return
	}
}

func (s *Server) convertBlessingsToHandle(blessings security.Blessings) principal.JsBlessings {
	return *principal.ConvertBlessingsToHandle(blessings, s.helper.GetOrAddBlessingsHandle(blessings))
}

func makeListOfErrors(numErrors int, err error) []error {
	errs := make([]error, numErrors)
	for i := 0; i < numErrors; i++ {
		errs[i] = err
	}
	return errs
}

// wsprCaveatValidator validates caveats in javascript.
// It resolves each []security.Caveat in cavs to an error (or nil) and collects them in a slice.
func (s *Server) validateCavsInJavascript(ctx *context.T, cavs [][]security.Caveat) []error {
	flow := s.helper.CreateNewFlow(s, nil)
	req := CaveatValidationRequest{
		Call: s.convertSecurityCall(ctx, false),
		Cavs: cavs,
	}

	replyChan := make(chan []error, 1)
	s.outstandingRequestLock.Lock()
	s.outstandingValidationRequests[flow.ID] = replyChan
	s.outstandingRequestLock.Unlock()

	defer func() {
		s.outstandingRequestLock.Lock()
		delete(s.outstandingValidationRequests, flow.ID)
		s.outstandingRequestLock.Unlock()
		s.cleanupFlow(flow.ID)
	}()

	if err := flow.Writer.Send(lib.ResponseValidate, req); err != nil {
		vlog.VI(2).Infof("Failed to send validate response: %v", err)
		replyChan <- makeListOfErrors(len(cavs), err)
	}

	// TODO(bprosnitz) Consider using a different timeout than the standard rpc timeout.
	var timeoutChan <-chan time.Time
	if deadline, ok := ctx.Deadline(); ok {
		timeoutChan = time.After(deadline.Sub(time.Now()))
	}

	select {
	case <-timeoutChan:
		return makeListOfErrors(len(cavs), NewErrCaveatValidationTimeout(ctx))
	case reply := <-replyChan:
		if len(reply) != len(cavs) {
			vlog.VI(2).Infof("Wspr caveat validator received %d results from javascript but expected %d", len(reply), len(cavs))
			return makeListOfErrors(len(cavs), NewErrInvalidValidationResponseFromJavascript(ctx))
		}

		return reply
	}
}

// wsprCaveatValidator validates caveats for javascript.
// Certain caveats (PublicKeyThirdPartyCaveatX) are intercepted and handled in go.
// This call validateCavsInJavascript to process the remaining caveats in javascript.
func (s *Server) wsprCaveatValidator(ctx *context.T, cavs [][]security.Caveat) []error {
	type validationStatus struct {
		err   error
		isSet bool
	}
	valStatus := make([]validationStatus, len(cavs))

	var caveatChainsToValidate [][]security.Caveat
nextCav:
	for i, chainCavs := range cavs {
		var newChainCavs []security.Caveat
		for _, cav := range chainCavs {
			switch cav.Id {
			case security.PublicKeyThirdPartyCaveatX.Id:
				res := cav.Validate(ctx)
				if res != nil {
					valStatus[i] = validationStatus{
						err:   res,
						isSet: true,
					}
					continue nextCav
				}
			default:
				newChainCavs = append(newChainCavs, cav)
			}
		}
		if len(newChainCavs) == 0 {
			valStatus[i] = validationStatus{
				err:   nil,
				isSet: true,
			}
		} else {
			caveatChainsToValidate = append(caveatChainsToValidate, newChainCavs)
		}
	}

	jsRes := s.validateCavsInJavascript(ctx, caveatChainsToValidate)

	outResults := make([]error, len(cavs))
	jsIndex := 0
	for i, status := range valStatus {
		if status.isSet {
			outResults[i] = status.err
		} else {
			outResults[i] = jsRes[jsIndex]
			jsIndex++
		}
	}

	return outResults
}

func (s *Server) convertSecurityCall(ctx *context.T, includeBlessingStrings bool) SecurityCall {
	call := security.GetCall(ctx)
	var localEndpoint string
	if call.LocalEndpoint() != nil {
		localEndpoint = call.LocalEndpoint().String()
	}
	var remoteEndpoint string
	if call.RemoteEndpoint() != nil {
		remoteEndpoint = call.RemoteEndpoint().String()
	}
	var localBlessings principal.JsBlessings
	if !call.LocalBlessings().IsZero() {
		localBlessings = s.convertBlessingsToHandle(call.LocalBlessings())
	}
	anymtags := make([]*vdl.Value, len(call.MethodTags()))
	for i, mtag := range call.MethodTags() {
		anymtags[i] = mtag
	}
	secCall := SecurityCall{
		Method:          lib.LowercaseFirstCharacter(call.Method()),
		Suffix:          call.Suffix(),
		MethodTags:      anymtags,
		LocalEndpoint:   localEndpoint,
		RemoteEndpoint:  remoteEndpoint,
		LocalBlessings:  localBlessings,
		RemoteBlessings: s.convertBlessingsToHandle(call.RemoteBlessings()),
	}
	if includeBlessingStrings {
		secCall.LocalBlessingStrings = security.LocalBlessingNames(ctx)
		secCall.RemoteBlessingStrings, _ = security.RemoteBlessingNames(ctx)
	}
	return secCall
}

type remoteAuthFunc func(ctx *context.T) error

func (s *Server) createRemoteAuthFunc(handle int32) remoteAuthFunc {
	return func(ctx *context.T) error {
		// Until the tests get fixed, we need to create a security context before creating the flow
		// because creating the security context creates a flow and flow ids will be off.
		securityCall := s.convertSecurityCall(ctx, true)

		flow := s.helper.CreateNewFlow(s, nil)
		replyChan := make(chan error, 1)
		s.outstandingRequestLock.Lock()
		s.outstandingAuthRequests[flow.ID] = replyChan
		s.outstandingRequestLock.Unlock()
		message := AuthRequest{
			ServerId: s.id,
			Handle:   handle,
			Call:     securityCall,
		}
		vlog.VI(0).Infof("Sending out auth request for %v, %v", flow.ID, message)

		vomMessage, err := lib.VomEncode(message)
		if err != nil {
			replyChan <- verror.Convert(verror.ErrInternal, nil, err)
		} else if err := flow.Writer.Send(lib.ResponseAuthRequest, vomMessage); err != nil {
			replyChan <- verror.Convert(verror.ErrInternal, nil, err)
		}

		err = <-replyChan
		vlog.VI(0).Infof("going to respond with %v", err)
		s.outstandingRequestLock.Lock()
		delete(s.outstandingAuthRequests, flow.ID)
		s.outstandingRequestLock.Unlock()
		s.helper.CleanupFlow(flow.ID)
		return err
	}
}

func (s *Server) readStatus() {
	// A map of names to the last error message sent.
	lastErrors := map[string]string{}
	for {
		status := s.server.Status()
		for _, mountStatus := range status.Mounts {
			var errMsg string
			if mountStatus.LastMountErr != nil {
				errMsg = mountStatus.LastMountErr.Error()
			}
			mountName := mountStatus.Name
			if lastMessage, ok := lastErrors[mountName]; !ok || errMsg != lastMessage {
				if errMsg == "" {
					s.helper.SendLogMessage(
						lib.LogLevelInfo, "serve: "+mountName+" successfully mounted ")
				} else {
					s.helper.SendLogMessage(
						lib.LogLevelError, "serve: "+mountName+" failed with: "+errMsg)
				}
			}
			lastErrors[mountName] = errMsg
		}
		select {
		case <-time.After(10 * time.Second):
			continue
		case <-s.statusClose:
			return
		}
	}
}

func (s *Server) Serve(name string) error {
	s.serverStateLock.Lock()
	defer s.serverStateLock.Unlock()

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
	s.statusClose = make(chan struct{}, 1)
	go s.readStatus()
	return nil
}

func (s *Server) popServerRequest(id int32) chan *lib.ServerRpcReply {
	s.outstandingRequestLock.Lock()
	defer s.outstandingRequestLock.Unlock()
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
	var reply lib.ServerRpcReply
	if err := lib.VomDecode(data, &reply); err != nil {
		reply.Err = err
	}

	vlog.VI(0).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, reply)
	s.helper.CleanupFlow(id)
	if reply.Err != nil {
		ch <- &reply
		return
	}
	jsBlessingsType := vdl.TypeOf(principal.JsBlessings{})
	for i, val := range reply.Results {
		if val.Type() == jsBlessingsType {
			var jsBlessings principal.JsBlessings
			if err := vdl.Convert(&jsBlessings, val); err != nil {
				reply.Err = err
				break
			}
			reply.Results[i] = vdl.ValueOf(
				s.helper.GetBlessings(jsBlessings.Handle))
		}
	}
	ch <- &reply
}

func (s *Server) HandleLookupResponse(id int32, data string) {
	s.dispatcher.handleLookupResponse(id, data)
}

func (s *Server) HandleAuthResponse(id int32, data string) {
	s.outstandingRequestLock.Lock()
	ch := s.outstandingAuthRequests[id]
	s.outstandingRequestLock.Unlock()
	if ch == nil {
		vlog.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results(%s)", id, data)
		// Ignore unknown responses that don't belong to any channel
		return
	}
	// Decode the result and send it through the channel
	var reply authReply
	if decoderErr := json.Unmarshal([]byte(data), &reply); decoderErr != nil {
		err := verror.Convert(verror.ErrInternal, nil, decoderErr).(verror.E)
		reply = authReply{Err: &err}
	}

	vlog.VI(0).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, reply)
	s.helper.CleanupFlow(id)
	// A nil verror.E does not result in an nil error.  Instead, we have create
	// a variable for the error interface and only set it's value if the struct is non-
	// nil.
	var err error
	if reply.Err != nil {
		err = reply.Err
	}
	ch <- err
}

func (s *Server) HandleCaveatValidationResponse(id int32, data string) {
	s.outstandingRequestLock.Lock()
	ch := s.outstandingValidationRequests[id]
	s.outstandingRequestLock.Unlock()
	if ch == nil {
		vlog.Errorf("unexpected result from JavaScript. No channel "+
			"for validation response with MessageId: %d exists. Ignoring the results(%s)", id, data)
		// Ignore unknown responses that don't belong to any channel
		return
	}

	var reply CaveatValidationResponse
	if err := lib.VomDecode(data, &reply); err != nil {
		vlog.Errorf("failed to decode validation response %q: error %v", data, err)
		ch <- []error{}
		return
	}

	ch <- reply.Results
}

func (s *Server) createFlow() *Flow {
	return s.helper.CreateNewFlow(s, nil)
}

func (s *Server) cleanupFlow(id int32) {
	s.helper.CleanupFlow(id)
}

func (s *Server) createInvoker(handle int32, sig []signature.Interface, hasGlobber bool) (rpc.Invoker, error) {
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
	stdErr := verror.New(verror.ErrTimeout, nil).(verror.E)
	result := lib.ServerRpcReply{
		Results: nil,
		Err:     &stdErr,
	}
	s.serverStateLock.Lock()

	if s.statusClose != nil {
		close(s.statusClose)
	}
	if s.dispatcher != nil {
		s.dispatcher.Cleanup()
	}

	for _, ch := range s.outstandingAuthRequests {
		ch <- fmt.Errorf("Cleaning up server")
	}

	for _, ch := range s.outstandingServerRequests {
		select {
		case ch <- &result:
		default:
		}
	}
	s.outstandingRequestLock.Lock()
	s.outstandingAuthRequests = make(map[int32]chan error)
	s.outstandingServerRequests = make(map[int32]chan *lib.ServerRpcReply)
	s.outstandingRequestLock.Unlock()
	s.serverStateLock.Unlock()
	s.server.Stop()

	// Only clear the validation requests map after stopping. Clearing them before
	// can cause the publisher to get stuck waiting for a caveat validation that
	// will never be answered, which prevents the server from stopping.
	s.serverStateLock.Lock()
	s.outstandingRequestLock.Lock()
	s.outstandingValidationRequests = make(map[int32]chan []error)
	s.outstandingRequestLock.Unlock()
	s.serverStateLock.Unlock()
}

func (s *Server) AddName(name string) error {
	return s.server.AddName(name)
}

func (s *Server) RemoveName(name string) {
	s.server.RemoveName(name)
}
