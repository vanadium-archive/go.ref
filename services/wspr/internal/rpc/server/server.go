// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// An implementation of a server for WSPR

package server

import (
	"fmt"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdlroot/signature"
	vdltime "v.io/v23/vdlroot/time"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/ref/services/wspr/internal/lib"
	"v.io/x/ref/services/wspr/internal/principal"
)

type Flow struct {
	ID     int32
	Writer lib.ClientWriter
}

type FlowHandler interface {
	CreateNewFlow(server interface{}, sender rpc.Stream) *Flow

	CleanupFlow(id int32)
}

type VomHelper interface {
	TypeEncoder() *vom.TypeEncoder

	TypeDecoder() *vom.TypeDecoder
}

type ServerHelper interface {
	FlowHandler
	VomHelper

	SendLogMessage(level lib.LogLevel, msg string) error
	BlessingsCache() *principal.BlessingsCache

	Context() *context.T
}

// AuthRequest is a request for a javascript authorizer to run
// This is exported to make the app test easier.
type AuthRequest struct {
	ServerId uint32       `json:"serverId"`
	Handle   int32        `json:"handle"`
	Call     SecurityCall `json:"call"`
	Context  Context
}

type Server struct {
	// serverStateLock should be aquired when starting or stopping the server.
	// This should be locked before outstandingRequestLock.
	serverStateLock sync.Mutex

	// The server that handles the rpc layer.
	server rpc.Server

	// The saved dispatcher.
	dispatcher *dispatcher

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

	ctx *context.T
}

type serverContextKey struct{}

func NewServer(id uint32, name string, listenSpec *rpc.ListenSpec, helper ServerHelper, opts ...rpc.ServerOpt) (*Server, error) {
	server := &Server{
		id:     id,
		helper: helper,
		outstandingServerRequests:     make(map[int32]chan *lib.ServerRpcReply),
		outstandingAuthRequests:       make(map[int32]chan error),
		outstandingValidationRequests: make(map[int32]chan []error),
	}
	var err error
	ctx := helper.Context()
	ctx = context.WithValue(ctx, serverContextKey{}, server)

	server.serverStateLock.Lock()
	defer server.serverStateLock.Unlock()

	server.dispatcher = newDispatcher(server.id, server, server, server, server.helper)
	ctx = v23.WithListenSpec(ctx, *listenSpec)

	if ctx, server.server, err = v23.WithNewDispatchingServer(ctx, name, server.dispatcher, opts...); err != nil {
		return nil, err
	}
	server.ctx = ctx
	server.statusClose = make(chan struct{}, 1)
	go server.readStatus()

	return server, nil
}

// remoteInvokeFunc is a type of function that can invoke a remote method and
// communicate the result back via a channel to the caller
type remoteInvokeFunc func(ctx *context.T, call rpc.StreamServerCall, methodName string, args []interface{}) <-chan *lib.ServerRpcReply

func (s *Server) createRemoteInvokerFunc(handle int32) remoteInvokeFunc {
	return func(ctx *context.T, call rpc.StreamServerCall, methodName string, args []interface{}) <-chan *lib.ServerRpcReply {
		securityCall := ConvertSecurityCall(s.helper, ctx, call.Security(), true)

		flow := s.helper.CreateNewFlow(s, call)
		replyChan := make(chan *lib.ServerRpcReply, 1)
		s.outstandingRequestLock.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.outstandingRequestLock.Unlock()

		var timeout vdltime.Deadline
		if deadline, ok := ctx.Deadline(); ok {
			timeout.Time = deadline
		}

		errHandler := func(err error) <-chan *lib.ServerRpcReply {
			if ch := s.popServerRequest(flow.ID); ch != nil {
				stdErr := verror.Convert(verror.ErrInternal, ctx, err).(verror.E)
				ch <- &lib.ServerRpcReply{nil, &stdErr, vtrace.Response{}}
				s.helper.CleanupFlow(flow.ID)
			}
			return replyChan
		}

		var grantedBlessings principal.BlessingsId
		if !call.GrantedBlessings().IsZero() {
			grantedBlessings = s.helper.BlessingsCache().Put(call.GrantedBlessings())
		}

		rpcCall := ServerRpcRequestCall{
			SecurityCall: securityCall,
			Deadline:     timeout,
			TraceRequest: vtrace.GetRequest(ctx),
			Context: Context{
				Language: string(i18n.GetLangID(ctx)),
			},
			GrantedBlessings: grantedBlessings,
		}

		var rawBytesArgs []*vom.RawBytes = make([]*vom.RawBytes, len(args))
		for i, arg := range args {
			rawBytesArgs[i] = vom.RawBytesOf(arg)
		}

		// Send a invocation request to JavaScript
		message := ServerRpcRequest{
			ServerId: s.id,
			Handle:   handle,
			Method:   lib.LowercaseFirstCharacter(methodName),
			Args:     rawBytesArgs,
			Call:     rpcCall,
		}
		vomMessage, err := lib.HexVomEncode(message, s.helper.TypeEncoder())
		if err != nil {
			return errHandler(err)
		}
		if err := flow.Writer.Send(lib.ResponseServerRequest, vomMessage); err != nil {
			return errHandler(err)
		}

		ctx.VI(3).Infof("calling method %q with args %v, MessageID %d assigned\n", methodName, args, flow.ID)

		// Watch for cancellation.
		go func() {
			<-ctx.Done()
			ch := s.popServerRequest(flow.ID)
			if ch == nil {
				return
			}

			// Send a cancel message to the JS server.
			flow.Writer.Send(lib.ResponseCancel, nil)
			s.helper.CleanupFlow(flow.ID)

			err := verror.Convert(verror.ErrAborted, ctx, ctx.Err()).(verror.E)
			ch <- &lib.ServerRpcReply{nil, &err, vtrace.Response{}}
		}()

		go s.proxyStream(call, flow, s.helper.TypeEncoder())

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
type remoteGlobFunc func(ctx *context.T, call rpc.ServerCall, pattern string) (<-chan naming.GlobReply, error)

func (s *Server) createRemoteGlobFunc(handle int32) remoteGlobFunc {
	return func(ctx *context.T, call rpc.ServerCall, pattern string) (<-chan naming.GlobReply, error) {
		// Until the tests get fixed, we need to create a security context before creating the flow
		// because creating the security context creates a flow and flow ids will be off.
		// See https://github.com/vanadium/issues/issues/175
		securityCall := ConvertSecurityCall(s.helper, ctx, call.Security(), true)

		globChan := make(chan naming.GlobReply, 1)
		flow := s.helper.CreateNewFlow(s, &globStream{
			ch:  globChan,
			ctx: ctx,
		})
		replyChan := make(chan *lib.ServerRpcReply, 1)
		s.outstandingRequestLock.Lock()
		s.outstandingServerRequests[flow.ID] = replyChan
		s.outstandingRequestLock.Unlock()

		var timeout vdltime.Deadline
		if deadline, ok := ctx.Deadline(); ok {
			timeout.Time = deadline
		}

		errHandler := func(err error) (<-chan naming.GlobReply, error) {
			if ch := s.popServerRequest(flow.ID); ch != nil {
				s.helper.CleanupFlow(flow.ID)
			}
			return nil, verror.Convert(verror.ErrInternal, ctx, err).(verror.E)
		}

		rpcCall := ServerRpcRequestCall{
			SecurityCall:     securityCall,
			Deadline:         timeout,
			GrantedBlessings: s.helper.BlessingsCache().Put(call.GrantedBlessings()),
			Context: Context{
				Language: string(i18n.GetLangID(ctx)),
			},
		}

		// Send a invocation request to JavaScript
		message := ServerRpcRequest{
			ServerId: s.id,
			Handle:   handle,
			Method:   "Glob__",
			Args:     []*vom.RawBytes{vom.RawBytesOf(pattern)},
			Call:     rpcCall,
		}
		vomMessage, err := lib.HexVomEncode(message, s.helper.TypeEncoder())
		if err != nil {
			return errHandler(err)
		}
		if err := flow.Writer.Send(lib.ResponseServerRequest, vomMessage); err != nil {
			return errHandler(err)
		}

		ctx.VI(3).Infof("calling method 'Glob__' with args %v, MessageID %d assigned\n", []interface{}{pattern}, flow.ID)

		// Watch for cancellation.
		go func() {
			<-ctx.Done()
			ch := s.popServerRequest(flow.ID)
			if ch == nil {
				return
			}

			// Send a cancel message to the JS server.
			flow.Writer.Send(lib.ResponseCancel, nil)
			s.helper.CleanupFlow(flow.ID)

			err := verror.Convert(verror.ErrAborted, ctx, ctx.Err()).(verror.E)
			ch <- &lib.ServerRpcReply{nil, &err, vtrace.Response{}}
		}()

		return globChan, nil
	}
}

func (s *Server) proxyStream(stream rpc.Stream, flow *Flow, typeEncoder *vom.TypeEncoder) {
	var item interface{}
	var err error
	w := flow.Writer
	for err = stream.Recv(&item); err == nil; err = stream.Recv(&item) {
		vomItem, err := lib.HexVomEncode(item, typeEncoder)
		if err != nil {
			w.Error(verror.Convert(verror.ErrInternal, nil, err))
			return
		}
		if err := w.Send(lib.ResponseStream, vomItem); err != nil {
			w.Error(verror.Convert(verror.ErrInternal, nil, err))
			return
		}
	}
	s.ctx.VI(1).Infof("Error reading from stream: %v\n", err)
	s.outstandingRequestLock.Lock()
	_, found := s.outstandingServerRequests[flow.ID]
	s.outstandingRequestLock.Unlock()

	if !found {
		// The flow has already been closed.  This is usually because we got a response
		// from the javascript server.
		return
	}

	if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
		w.Error(verror.Convert(verror.ErrInternal, nil, err))
		return
	}
}

func makeListOfErrors(numErrors int, err error) []error {
	errs := make([]error, numErrors)
	for i := 0; i < numErrors; i++ {
		errs[i] = err
	}
	return errs
}

// caveatValidationInGo validates caveats in Go, using the default logic.
func caveatValidationInGo(ctx *context.T, call security.Call, sets [][]security.Caveat) []error {
	results := make([]error, len(sets))
	for i, set := range sets {
		for _, cav := range set {
			if err := cav.Validate(ctx, call); err != nil {
				results[i] = err
				break
			}
		}
	}
	return results
}

// caveatValidationInJavascript validates caveats in javascript.  It resolves
// each []security.Caveat in cavs to an error (or nil) and collects them in a
// slice.
func (s *Server) caveatValidationInJavascript(ctx *context.T, call security.Call, cavs [][]security.Caveat) []error {
	flow := s.helper.CreateNewFlow(s, nil)
	req := CaveatValidationRequest{
		Call: ConvertSecurityCall(s.helper, ctx, call, false),
		Context: Context{
			Language: string(i18n.GetLangID(ctx)),
		},
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
		ctx.VI(2).Infof("Failed to send validate response: %v", err)
		replyChan <- makeListOfErrors(len(cavs), err)
	}

	// TODO(bprosnitz) Consider using a different timeout than the standard rpc timeout.
	var timeoutChan <-chan time.Time
	if deadline, ok := ctx.Deadline(); ok {
		timeoutChan = time.After(deadline.Sub(time.Now()))
	}

	select {
	case <-s.statusClose:
		return caveatValidationInGo(ctx, call, cavs)
	case <-timeoutChan:
		return makeListOfErrors(len(cavs), NewErrCaveatValidationTimeout(ctx))
	case reply := <-replyChan:
		if len(reply) != len(cavs) {
			ctx.VI(2).Infof("Wspr caveat validator received %d results from javascript but expected %d", len(reply), len(cavs))
			return makeListOfErrors(len(cavs), NewErrInvalidValidationResponseFromJavascript(ctx))
		}

		return reply
	}
}

// CaveatValidation implements a function suitable for passing to
// security.OverrideCaveatValidation.
//
// Certain caveats (PublicKeyThirdPartyCaveat) are intercepted and handled in
// go, while all other caveats are evaluated in javascript.
func CaveatValidation(ctx *context.T, call security.Call, cavs [][]security.Caveat) []error {
	// If the server isn't set in the context, we just perform validation in Go.
	ctxServer := ctx.Value(serverContextKey{})
	if ctxServer == nil {
		return caveatValidationInGo(ctx, call, cavs)
	}
	// Otherwise we run our special logic.
	server := ctxServer.(*Server)
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
			// If the server is closed handle all caveats in Go, because Javascript is
			// no longer there.
			select {
			case <-server.statusClose:
				res := cav.Validate(ctx, call)
				if res != nil {
					valStatus[i] = validationStatus{
						err:   res,
						isSet: true,
					}
					continue nextCav
				}
			default:
			}
			switch cav.Id {
			case security.PublicKeyThirdPartyCaveat.Id:
				res := cav.Validate(ctx, call)
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

	jsRes := server.caveatValidationInJavascript(ctx, call, caveatChainsToValidate)

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

func ConvertSecurityCall(helper ServerHelper, ctx *context.T, call security.Call, includeBlessingStrings bool) SecurityCall {
	var localEndpoint string
	if call.LocalEndpoint() != nil {
		localEndpoint = call.LocalEndpoint().String()
	}
	var remoteEndpoint string
	if call.RemoteEndpoint() != nil {
		remoteEndpoint = call.RemoteEndpoint().String()
	}
	anymtags := make([]*vom.RawBytes, len(call.MethodTags()))
	for i, mtag := range call.MethodTags() {
		anymtags[i] = vom.RawBytesOf(mtag)
	}
	secCall := SecurityCall{
		Method:          lib.LowercaseFirstCharacter(call.Method()),
		Suffix:          call.Suffix(),
		MethodTags:      anymtags,
		LocalEndpoint:   localEndpoint,
		RemoteEndpoint:  remoteEndpoint,
		LocalBlessings:  helper.BlessingsCache().Put(call.LocalBlessings()),
		RemoteBlessings: helper.BlessingsCache().Put(call.RemoteBlessings()),
	}
	if includeBlessingStrings {
		secCall.LocalBlessingStrings = security.LocalBlessingNames(ctx, call)
		secCall.RemoteBlessingStrings, _ = security.RemoteBlessingNames(ctx, call)
	}
	return secCall
}

type remoteAuth struct {
	Func   func(*context.T, security.Call, int32) error
	Handle int32
}

func (r remoteAuth) Authorize(ctx *context.T, call security.Call) error {
	return r.Func(ctx, call, r.Handle)
}

func (s *Server) createRemoteAuthorizer(handle int32) security.Authorizer {
	return remoteAuth{s.authorizeRemote, handle}
}

func (s *Server) authorizeRemote(ctx *context.T, call security.Call, handle int32) error {
	// Until the tests get fixed, we need to create a security context before
	// creating the flow because creating the security context creates a flow and
	// flow ids will be off.
	securityCall := ConvertSecurityCall(s.helper, ctx, call, true)

	flow := s.helper.CreateNewFlow(s, nil)
	replyChan := make(chan error, 1)
	s.outstandingRequestLock.Lock()
	s.outstandingAuthRequests[flow.ID] = replyChan
	s.outstandingRequestLock.Unlock()
	message := AuthRequest{
		ServerId: s.id,
		Handle:   handle,
		Call:     securityCall,
		Context: Context{
			Language: string(i18n.GetLangID(ctx)),
		},
	}
	ctx.VI(0).Infof("Sending out auth request for %v, %v", flow.ID, message)

	vomMessage, err := lib.HexVomEncode(message, s.helper.TypeEncoder())
	if err != nil {
		replyChan <- verror.Convert(verror.ErrInternal, nil, err)
	} else if err := flow.Writer.Send(lib.ResponseAuthRequest, vomMessage); err != nil {
		replyChan <- verror.Convert(verror.ErrInternal, nil, err)
	}

	err = <-replyChan
	ctx.VI(0).Infof("going to respond with %v", err)
	s.outstandingRequestLock.Lock()
	delete(s.outstandingAuthRequests, flow.ID)
	s.outstandingRequestLock.Unlock()
	s.helper.CleanupFlow(flow.ID)
	return err
}

func (s *Server) readStatus() {
	// A map of names to the last error message sent.
	lastErrors := map[string]string{}
	for {
		status := s.server.Status()
		for _, e := range status.PublisherStatus {
			var errMsg string
			if e.LastMountErr != nil {
				errMsg = e.LastMountErr.Error()
			}
			name := e.Name
			if lastMessage, ok := lastErrors[name]; !ok || errMsg != lastMessage {
				if errMsg == "" {
					s.helper.SendLogMessage(
						lib.LogLevelInfo, "serve: "+name+" successfully mounted ")
				} else {
					s.helper.SendLogMessage(
						lib.LogLevelError, "serve: "+name+" failed with: "+errMsg)
				}
			}
			lastErrors[name] = errMsg
		}
		select {
		case <-time.After(10 * time.Second):
			continue
		case <-s.statusClose:
			return
		}
	}
}

func (s *Server) popServerRequest(id int32) chan *lib.ServerRpcReply {
	s.outstandingRequestLock.Lock()
	defer s.outstandingRequestLock.Unlock()
	ch := s.outstandingServerRequests[id]
	delete(s.outstandingServerRequests, id)

	return ch
}

func (s *Server) HandleServerResponse(ctx *context.T, id int32, data string) {
	ch := s.popServerRequest(id)
	if ch == nil {
		ctx.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		// Ignore unknown responses that don't belong to any channel
		return
	}

	// Decode the result and send it through the channel
	var reply lib.ServerRpcReply
	if err := lib.HexVomDecode(data, &reply, s.helper.TypeDecoder()); err != nil {
		reply.Err = err
	}

	ctx.VI(0).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, reply)
	s.helper.CleanupFlow(id)
	if reply.Err != nil {
		ch <- &reply
		return
	}
	ch <- &reply
}

func (s *Server) HandleLookupResponse(ctx *context.T, id int32, data string) {
	s.dispatcher.handleLookupResponse(ctx, id, data)
}

func (s *Server) HandleAuthResponse(ctx *context.T, id int32, data string) {
	s.outstandingRequestLock.Lock()
	ch := s.outstandingAuthRequests[id]
	s.outstandingRequestLock.Unlock()
	if ch == nil {
		ctx.Errorf("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results(%s)", id, data)
		// Ignore unknown responses that don't belong to any channel
		return
	}
	// Decode the result and send it through the channel
	var reply AuthReply
	if err := lib.HexVomDecode(data, &reply, s.helper.TypeDecoder()); err != nil {
		err = verror.Convert(verror.ErrInternal, nil, err)
		reply = AuthReply{Err: err}
	}

	ctx.VI(0).Infof("response received from JavaScript server for "+
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

func (s *Server) HandleCaveatValidationResponse(ctx *context.T, id int32, data string) {
	s.outstandingRequestLock.Lock()
	ch := s.outstandingValidationRequests[id]
	s.outstandingRequestLock.Unlock()
	if ch == nil {
		ctx.Errorf("unexpected result from JavaScript. No channel "+
			"for validation response with MessageId: %d exists. Ignoring the results(%s)", id, data)
		// Ignore unknown responses that don't belong to any channel
		return
	}

	var reply CaveatValidationResponse
	if err := lib.HexVomDecode(data, &reply, s.helper.TypeDecoder()); err != nil {
		ctx.Errorf("failed to decode validation response %q: error %v", data, err)
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
		return s.createRemoteAuthorizer(handle), nil
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

	s.outstandingRequestLock.Lock()
	for _, ch := range s.outstandingAuthRequests {
		ch <- fmt.Errorf("Cleaning up server")
	}

	for _, ch := range s.outstandingServerRequests {
		select {
		case ch <- &result:
		default:
		}
	}
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
