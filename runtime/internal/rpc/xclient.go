// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"fmt"
	"io"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	"v.io/v23/i18n"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	vtime "v.io/v23/vdlroot/time"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/ref/lib/apilog"
	slib "v.io/x/ref/lib/security"
	"v.io/x/ref/runtime/internal/flow/manager"
	inaming "v.io/x/ref/runtime/internal/naming"
)

const (
	dataFlow = 'd'
	typeFlow = 't'

	dischargeBuffer = time.Minute
)

type clientFlowManagerOpt struct {
	mgr flow.Manager
}

func (clientFlowManagerOpt) RPCClientOpt() {}

type xclient struct {
	flowMgr            flow.Manager
	ns                 namespace.T
	preferredProtocols []string
	ctx                *context.T
	// stop is kept for backward compatibilty to implement Close().
	// TODO(mattr): deprecate Close.
	stop func()

	// typeCache maintains a cache of type encoders and decoders.
	typeCache *typeCache

	wg      sync.WaitGroup
	mu      sync.Mutex
	closing bool
	closed  chan struct{}
}

var _ rpc.Client = (*xclient)(nil)

func NewXClient(ctx *context.T, ns namespace.T, opts ...rpc.ClientOpt) rpc.Client {
	ctx, cancel := context.WithCancel(ctx)
	c := &xclient{
		ctx:       ctx,
		ns:        ns,
		typeCache: newTypeCache(),
		stop:      cancel,
		closed:    make(chan struct{}),
	}

	for _, opt := range opts {
		switch v := opt.(type) {
		case PreferredProtocols:
			c.preferredProtocols = v
		case clientFlowManagerOpt:
			c.flowMgr = v.mgr
		}
	}
	if c.flowMgr == nil {
		c.flowMgr = manager.New(ctx, naming.NullRoutingID, nil, 0)
	}

	go func() {
		<-ctx.Done()
		c.mu.Lock()
		c.closing = true
		c.mu.Unlock()

		<-c.flowMgr.Closed()
		c.wg.Wait()
		close(c.closed)
	}()

	return c
}

func (c *xclient) StartCall(ctx *context.T, name, method string, args []interface{}, opts ...rpc.CallOpt) (rpc.ClientCall, error) {
	defer apilog.LogCallf(ctx, "name=%.10s...,method=%.10s...,args=,opts...=%v", name, method, opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if !ctx.Initialized() {
		return nil, verror.ExplicitNew(verror.ErrBadArg, i18n.LangID("en-us"), "<rpc.Client>", "StartCall", "context not initialized")
	}
	deadline := getDeadline(ctx, opts)
	return c.startCall(ctx, name, method, args, deadline, opts)
}

func (c *xclient) startCall(ctx *context.T, name, method string, args []interface{}, deadline time.Time, opts []rpc.CallOpt) (rpc.ClientCall, error) {
	ctx, span := vtrace.WithNewSpan(ctx, fmt.Sprintf("<rpc.Client>%q.%s", name, method))
	for retries := uint(0); ; retries++ {
		call, action, requireResolve, err := c.tryCall(ctx, name, method, args, opts)
		switch {
		case err == nil:
			return call, nil
		case !shouldRetry(action, requireResolve, deadline, opts):
			span.Annotatef("Cannot retry after error: %s", err)
			return nil, err
		case !backoff(retries, deadline):
			return nil, err
		default:
			span.Annotatef("Retrying due to error: %s", err)
			ctx.VI(2).Infof("Retrying due to error: %s", err)
		}
	}
}

func (c *xclient) Call(ctx *context.T, name, method string, inArgs, outArgs []interface{}, opts ...rpc.CallOpt) error {
	defer apilog.LogCallf(ctx, "name=%.10s...,method=%.10s...,inArgs=,outArgs=,opts...=%v", name, method, opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	deadline := getDeadline(ctx, opts)
	for retries := uint(0); ; retries++ {
		call, err := c.startCall(ctx, name, method, inArgs, deadline, opts)
		if err != nil {
			return err
		}
		switch err := call.Finish(outArgs...); {
		case err == nil:
			return nil
		case !shouldRetryBackoff(verror.Action(err), deadline, opts):
			ctx.VI(4).Infof("Cannot retry after error: %s", err)
			return err
		case !backoff(retries, deadline):
			return err
		default:
			ctx.VI(4).Infof("Retrying due to error: %s", err)
		}
	}
}

type xserverStatus struct {
	index          int
	server, suffix string
	flow           flow.Flow
	serverErr      *verror.SubErr
	typeEnc        *vom.TypeEncoder
	typeDec        *vom.TypeDecoder
}

// tryCreateFlow attempts to establish a Flow to "server" (which must be a
// rooted name), over which a method invocation request could be sent.
//
// The server at the remote end of the flow is authorized using the provided
// authorizer, both during creation of the VC underlying the flow and the
// flow itself.
// TODO(cnicolaou): implement real, configurable load balancing.
func (c *xclient) tryCreateFlow(
	ctx *context.T,
	index int,
	name, server, method string,
	args []interface{},
	auth security.Authorizer,
	channelTimeout time.Duration,
	ch chan<- *xserverStatus) {
	defer c.wg.Done()
	status := &xserverStatus{index: index, server: server}
	var span vtrace.Span
	ctx, span = vtrace.WithNewSpan(ctx, "<client>tryCreateFlow "+server)
	defer func() {
		ch <- status
		span.Finish()
	}()
	suberr := func(err error) *verror.SubErr {
		return &verror.SubErr{
			Name:    suberrName(server, name, method),
			Err:     err,
			Options: verror.Print,
		}
	}

	address, suffix := naming.SplitAddressName(server)
	if len(address) == 0 {
		status.serverErr = suberr(verror.New(errNonRootedName, ctx, server))
		return
	}
	status.suffix = suffix

	ep, err := inaming.NewEndpoint(address)
	if err != nil {
		status.serverErr = suberr(verror.New(errInvalidEndpoint, ctx))
		return
	}
	peerAuth := peerAuthorizer{auth, method, args}
	flow, err := c.flowMgr.Dial(ctx, ep, peerAuth, channelTimeout)
	if err != nil {
		ctx.VI(2).Infof("rpc: failed to create Flow with %v: %v", server, err)
		status.serverErr = suberr(err)
		return
	}
	if write := c.typeCache.writer(flow.Conn()); write != nil {
		// Create the type flow with a root-cancellable context.
		// This flow must outlive the flow we're currently creating.
		// It lives as long as the connection to which it is bound.
		tctx, tcancel := context.WithRootCancel(ctx)
		tflow, err := c.flowMgr.Dial(tctx, ep, typeFlowAuthorizer{}, channelTimeout)
		if err != nil {
			status.serverErr = suberr(newErrTypeFlowFailure(tctx, err))
			flow.Close()
			return
		}
		if tflow.Conn() != flow.Conn() {
			status.serverErr = suberr(newErrTypeFlowFailure(ctx, nil))
			flow.Close()
			tflow.Close()
			return
		}
		if _, err = tflow.Write([]byte{typeFlow}); err != nil {
			status.serverErr = suberr(newErrTypeFlowFailure(tctx, err))
			flow.Close()
			tflow.Close()
			return
		}
		write(tflow, tcancel)
	}
	status.typeEnc, status.typeDec, err = c.typeCache.get(ctx, flow.Conn())
	if err != nil {
		status.serverErr = suberr(newErrTypeFlowFailure(ctx, err))
		flow.Close()
		return
	}
	status.flow = flow
}

type typeFlowAuthorizer struct{}

func (a typeFlowAuthorizer) AuthorizePeer(
	ctx *context.T,
	localEP, remoteEP naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge) ([]string, []security.RejectedBlessing, error) {
	return nil, nil, nil
}

func (a typeFlowAuthorizer) BlessingsForPeer(ctx *context.T, peerNames []string) (
	security.Blessings, map[string]security.Discharge, error) {
	return security.Blessings{}, nil, nil
}

type peerAuthorizer struct {
	auth   security.Authorizer
	method string
	args   []interface{}
}

func (x peerAuthorizer) AuthorizePeer(
	ctx *context.T,
	localEP, remoteEP naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge) ([]string, []security.RejectedBlessing, error) {
	localPrincipal := v23.GetPrincipal(ctx)
	// The "Method" and "Suffix" fields of the call are not populated
	// as they are considered irrelevant for authorizing server blessings.
	call := security.NewCall(&security.CallParams{
		Timestamp:        time.Now(),
		LocalPrincipal:   localPrincipal,
		LocalEndpoint:    localEP,
		RemoteBlessings:  remoteBlessings,
		RemoteDischarges: remoteDischarges,
		RemoteEndpoint:   remoteEP,
	})
	if err := x.auth.Authorize(ctx, call); err != nil {
		return nil, nil, verror.New(errPeerAuthorizeFailed, ctx, call.RemoteBlessings(), err)
	}
	peerNames, rejectedPeerNames := security.RemoteBlessingNames(ctx, call)
	return peerNames, rejectedPeerNames, nil
}

func (x peerAuthorizer) BlessingsForPeer(ctx *context.T, peerNames []string) (
	security.Blessings, map[string]security.Discharge, error) {
	localPrincipal := v23.GetPrincipal(ctx)
	clientB := localPrincipal.BlessingStore().ForPeer(peerNames...)
	impetus, err := mkDischargeImpetus(peerNames, x.method, x.args)
	if err != nil {
		return security.Blessings{}, nil, err
	}
	return clientB, slib.PrepareDischarges(ctx, clientB, impetus, dischargeBuffer), nil
}

// tryCall makes a single attempt at a call. It may connect to multiple servers
// (all that serve "name"), but will invoke the method on at most one of them
// (the server running on the most preferred protcol and network amongst all
// the servers that were successfully connected to and authorized).
// if requireResolve is true on return, then we shouldn't bother retrying unless
// you can re-resolve.
//
// TODO(toddw): Remove action from out-args, the error should tell us the action.
func (c *xclient) tryCall(ctx *context.T, name, method string, args []interface{}, opts []rpc.CallOpt) (call rpc.ClientCall, action verror.ActionCode, requireResolve bool, err error) {
	blessingPattern, name := security.SplitPatternName(name)
	resolved, err := c.ns.Resolve(ctx, name, getNamespaceOpts(opts)...)
	switch {
	case verror.ErrorID(err) == naming.ErrNoSuchName.ID:
		return nil, verror.RetryRefetch, false, verror.New(verror.ErrNoServers, ctx, name)
	case verror.ErrorID(err) == verror.ErrNoServers.ID:
		return nil, verror.NoRetry, false, err // avoid unnecessary wrapping
	case verror.ErrorID(err) == verror.ErrTimeout.ID:
		return nil, verror.NoRetry, false, err // return timeout without wrapping
	case err != nil:
		return nil, verror.NoRetry, false, verror.New(verror.ErrNoServers, ctx, name, err)
	case len(resolved.Servers) == 0:
		// This should never happen.
		return nil, verror.NoRetry, true, verror.New(verror.ErrInternal, ctx, name)
	}
	if resolved.Servers, err = filterAndOrderServers(resolved.Servers, c.preferredProtocols); err != nil {
		return nil, verror.RetryRefetch, true, verror.New(verror.ErrNoServers, ctx, name, err)
	}

	// servers is now ordered by the priority heurestic implemented in
	// filterAndOrderServers.
	//
	// Try to connect to all servers in parallel.  Provide sufficient
	// buffering for all of the connections to finish instantaneously. This
	// is important because we want to process the responses in priority
	// order; that order is indicated by the order of entries in servers.
	// So, if two respones come in at the same 'instant', we prefer the
	// first in the resolved.Servers)
	//
	// TODO(toddw): Refactor the parallel dials so that the policy can be changed,
	// and so that the goroutines for each Call are tracked separately.
	responses := make([]*xserverStatus, len(resolved.Servers))
	ch := make(chan *xserverStatus, len(resolved.Servers))
	authorizer := newServerAuthorizer(blessingPattern, opts...)
	channelTimeout := time.Duration(0)
	for _, opt := range opts {
		if t, ok := opt.(options.ChannelTimeout); ok {
			channelTimeout = time.Duration(t)
		}
	}
	for i, server := range resolved.Names() {
		c.mu.Lock()
		if c.closing {
			c.mu.Unlock()
			return nil, verror.NoRetry, false, verror.New(errClientCloseAlreadyCalled, ctx)
		}
		c.wg.Add(1)
		c.mu.Unlock()

		go c.tryCreateFlow(ctx, i, name, server, method, args, authorizer, channelTimeout, ch)
	}

	for {
		// Block for at least one new response from the server, or the timeout.
		select {
		case r := <-ch:
			responses[r.index] = r
			// Read as many more responses as we can without blocking.
		LoopNonBlocking:
			for {
				select {
				default:
					break LoopNonBlocking
				case r := <-ch:
					responses[r.index] = r
				}
			}
		case <-ctx.Done():
			_, _, _, err := c.failedTryCall(ctx, name, method, responses, ch)
			if verror.ErrorID(err) != verror.ErrTimeout.ID {
				return nil, verror.NoRetry, false, verror.New(verror.ErrTimeout, ctx, err)
			}
			return nil, verror.NoRetry, false, err
		}

		// Process new responses, in priority order.
		numResponses := 0
		for _, r := range responses {
			if r != nil {
				numResponses++
				if r.serverErr != nil && verror.ErrorID(r.serverErr.Err) == message.ErrWrongProtocol.ID {
					return nil, verror.NoRetry, false, r.serverErr.Err
				}
			}
			if r == nil || r.flow == nil {
				continue
			}

			fc, err := newFlowXClient(ctx, r.flow, r.typeEnc, r.typeDec)
			if err != nil {
				return nil, verror.NoRetry, false, err
			}

			// This is the 'point of no return'; once the RPC is started (fc.start
			// below) we can't be sure if it makes it to the server or not so, this
			// code will never call fc.start more than once to ensure that we provide
			// 'at-most-once' rpc semantics at this level. Retrying the network
			// connections (i.e. creating flows) is fine since we can cleanup that
			// state if we abort a call (i.e. close the flow).
			//
			// We must ensure that all flows other than r.flow are closed.
			//
			// TODO(cnicolaou): all errors below are marked as NoRetry
			// because we want to provide at-most-once rpc semantics so
			// we only ever attempt an RPC once. In the future, we'll cache
			// responses on the server and then we can retry in-flight
			// RPCs.
			go xcleanupTryCall(r, responses, ch)

			// TODO(toddw): It's wasteful to create this goroutine just for a vtrace
			// annotation.  Refactor this when we refactor the parallel dial logic.
			/*
				if ctx.Done() != nil {
					go func() {
						select {
						case <-ctx.Done():
							vtrace.GetSpan(fc.ctx).Annotate("Canceled")
						case <-fc.flow.Closed():
						}
					}()
				}
			*/

			deadline, _ := ctx.Deadline()
			if verr := fc.start(r.suffix, method, args, deadline, opts); verr != nil {
				return nil, verror.NoRetry, false, verr
			}
			return fc, verror.NoRetry, false, nil
		}
		if numResponses == len(responses) {
			return c.failedTryCall(ctx, name, method, responses, ch)
		}
	}
}

// xcleanupTryCall ensures we've waited for every response from the tryCreateFlow
// goroutines, and have closed the flow from each one except skip.  This is a
// blocking function; it should be called in its own goroutine.
func xcleanupTryCall(skip *xserverStatus, responses []*xserverStatus, ch chan *xserverStatus) {
	numPending := 0
	for _, r := range responses {
		switch {
		case r == nil:
			// The response hasn't arrived yet.
			numPending++
		case r == skip || r.flow == nil:
			// Either we should skip this flow, or we've closed the flow for this
			// response already; nothing more to do.
		default:
			// We received the response, but haven't closed the flow yet.
			//
			// TODO(toddw): Currently we only notice cancellation when we read or
			// write the flow.  Decide how to handle this.
			r.flow.WriteMsgAndClose() // TODO(toddw): cancel context instead?
		}
	}
	// Now we just need to wait for the pending responses and close their flows.
	for i := 0; i < numPending; i++ {
		if r := <-ch; r.flow != nil {
			r.flow.WriteMsgAndClose() // TODO(toddw): cancel context instead?
		}
	}
}

// failedTryCall performs asynchronous cleanup for tryCall, and returns an
// appropriate error from the responses we've already received.  All parallel
// calls in tryCall failed or we timed out if we get here.
func (c *xclient) failedTryCall(ctx *context.T, name, method string, responses []*xserverStatus, ch chan *xserverStatus) (rpc.ClientCall, verror.ActionCode, bool, error) {
	go xcleanupTryCall(nil, responses, ch)
	c.ns.FlushCacheEntry(ctx, name)
	suberrs := []verror.SubErr{}
	topLevelError := verror.ErrNoServers
	topLevelAction := verror.RetryRefetch
	onlyErrNetwork := true
	for _, r := range responses {
		if r != nil && r.serverErr != nil && r.serverErr.Err != nil {
			switch verror.ErrorID(r.serverErr.Err) {
			case /*stream.ErrNotTrusted.ID,*/ verror.ErrNotTrusted.ID, errPeerAuthorizeFailed.ID:
				topLevelError = verror.ErrNotTrusted
				topLevelAction = verror.NoRetry
				onlyErrNetwork = false
			/*case stream.ErrAborted.ID, stream.ErrNetwork.ID:*/
			// do nothing
			case verror.ErrTimeout.ID:
				topLevelError = verror.ErrTimeout
				onlyErrNetwork = false
			default:
				onlyErrNetwork = false
			}
			suberrs = append(suberrs, *r.serverErr)
		}
	}

	if onlyErrNetwork {
		// If we only encountered network errors, then report ErrBadProtocol.
		topLevelError = verror.ErrBadProtocol
	}

	// TODO(cnicolaou): we get system errors for things like dialing using
	// the 'ws' protocol which can never succeed even if we retry the connection,
	// hence we return RetryRefetch below except for the case where the servers
	// are not trusted, in case there's no point in retrying at all.
	// TODO(cnicolaou): implementing at-most-once rpc semantics in the future
	// will require thinking through all of the cases where the RPC can
	// be retried by the client whilst it's actually being executed on the
	// server.
	return nil, topLevelAction, false, verror.AddSubErrs(verror.New(topLevelError, ctx), ctx, suberrs...)
}

func (c *xclient) Close() {
	c.stop()
	<-c.Closed()
}

func (c *xclient) Closed() <-chan struct{} {
	return c.closed
}

// flowXClient implements the RPC client-side protocol for a single RPC, over a
// flow that's already connected to the server.
type flowXClient struct {
	ctx          *context.T   // context to annotate with call details
	flow         flow.Flow    // the underlying flow
	dec          *vom.Decoder // to decode responses and results from the server
	enc          *vom.Encoder // to encode requests and args to the server
	response     rpc.Response // each decoded response message is kept here
	remoteBNames []string

	sendClosedMu sync.Mutex
	sendClosed   bool // is the send side already closed? GUARDED_BY(sendClosedMu)
	finished     bool // has Finish() already been called?
}

var _ rpc.ClientCall = (*flowXClient)(nil)
var _ rpc.Stream = (*flowXClient)(nil)

func newFlowXClient(ctx *context.T, flow flow.Flow, typeEnc *vom.TypeEncoder, typeDec *vom.TypeDecoder) (*flowXClient, error) {
	if _, err := flow.Write([]byte{dataFlow}); err != nil {
		flow.Close()
		return nil, err
	}
	fc := &flowXClient{
		ctx:  ctx,
		flow: flow,
		dec:  vom.NewDecoderWithTypeDecoder(flow, typeDec),
		enc:  vom.NewEncoderWithTypeEncoder(flow, typeEnc),
	}
	return fc, nil
}

// close determines the appropriate error to return, in particular,
// if a timeout or cancelation has occured then any error
// is turned into a timeout or cancelation as appropriate.
// Cancelation takes precedence over timeout. This is needed because
// a timeout can lead to any other number of errors due to the underlying
// network connection being shutdown abruptly.
func (fc *flowXClient) close(err error) error {
	subErr := verror.SubErr{Err: err, Options: verror.Print}
	subErr.Name = "remote=" + fc.flow.RemoteEndpoint().String()
	// TODO(toddw): cancel context instead?
	if _, cerr := fc.flow.WriteMsgAndClose(); cerr != nil && err == nil {
		// TODO(mattr): The context is often already canceled here, in
		// which case we'll get an error.  Not clear what to do.
		//return verror.New(verror.ErrInternal, fc.ctx, subErr)
	}
	if err == nil {
		return nil
	}
	switch verror.ErrorID(err) {
	case verror.ErrCanceled.ID:
		return err
	case verror.ErrTimeout.ID:
		// Canceled trumps timeout.
		if fc.ctx.Err() == context.Canceled {
			return verror.AddSubErrs(verror.New(verror.ErrCanceled, fc.ctx), fc.ctx, subErr)
		}
		return err
	default:
		switch fc.ctx.Err() {
		case context.DeadlineExceeded:
			timeout := verror.New(verror.ErrTimeout, fc.ctx)
			err := verror.AddSubErrs(timeout, fc.ctx, subErr)
			return err
		case context.Canceled:
			canceled := verror.New(verror.ErrCanceled, fc.ctx)
			err := verror.AddSubErrs(canceled, fc.ctx, subErr)
			return err
		}
	}
	switch verror.ErrorID(err) {
	case errRequestEncoding.ID, errArgEncoding.ID, errResponseDecoding.ID:
		return verror.New(verror.ErrBadProtocol, fc.ctx, err)
	}
	return err
}

func (fc *flowXClient) start(suffix, method string, args []interface{}, deadline time.Time, opts []rpc.CallOpt) error {
	grantedB, err := fc.initSecurity(fc.ctx, method, suffix, opts)
	if err != nil {
		berr := verror.New(verror.ErrNotTrusted, fc.ctx, err)
		return fc.close(berr)
	}
	req := rpc.Request{
		Suffix:           suffix,
		Method:           method,
		NumPosArgs:       uint64(len(args)),
		Deadline:         vtime.Deadline{Time: deadline},
		GrantedBlessings: grantedB,
		TraceRequest:     vtrace.GetRequest(fc.ctx),
		Language:         string(i18n.GetLangID(fc.ctx)),
	}
	if err := fc.enc.Encode(req); err != nil {
		berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errRequestEncoding, fc.ctx, fmt.Sprintf("%#v", req), err))
		return fc.close(berr)
	}
	for ix, arg := range args {
		if err := fc.enc.Encode(arg); err != nil {
			berr := verror.New(errArgEncoding, fc.ctx, ix, err)
			return fc.close(berr)
		}
	}
	return nil
}

func (fc *flowXClient) initSecurity(ctx *context.T, method, suffix string, opts []rpc.CallOpt) (security.Blessings, error) {
	// The "Method" and "Suffix" fields of the call are not populated
	// as they are considered irrelevant for authorizing server blessings.
	call := security.NewCall(&security.CallParams{
		LocalPrincipal:   v23.GetPrincipal(ctx),
		LocalBlessings:   fc.flow.LocalBlessings(),
		RemoteBlessings:  fc.flow.RemoteBlessings(),
		LocalEndpoint:    fc.flow.LocalEndpoint(),
		RemoteEndpoint:   fc.flow.RemoteEndpoint(),
		LocalDischarges:  fc.flow.LocalDischarges(),
		RemoteDischarges: fc.flow.RemoteDischarges(),
	})
	// TODO(suharshs): Its unfortunate that we compute these here and also in the
	// peerAuthorizer struct. Find a way to only do this once.
	fc.remoteBNames, _ = security.RemoteBlessingNames(ctx, call)
	var grantedB security.Blessings
	for _, o := range opts {
		switch v := o.(type) {
		case rpc.Granter:
			if b, err := v.Grant(ctx, call); err != nil {
				return grantedB, verror.New(errBlessingGrant, fc.ctx, err)
			} else if grantedB, err = security.UnionOfBlessings(grantedB, b); err != nil {
				return grantedB, verror.New(errBlessingAdd, fc.ctx, err)
			}
		}
	}
	return grantedB, nil
}

func (fc *flowXClient) Send(item interface{}) error {
	defer apilog.LogCallf(nil, "item=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if fc.sendClosed {
		return verror.New(verror.ErrAborted, fc.ctx)
	}

	// The empty request header indicates what follows is a streaming arg.
	if err := fc.enc.Encode(rpc.Request{}); err != nil {
		berr := verror.New(errRequestEncoding, fc.ctx, rpc.Request{}, err)
		return fc.close(berr)
	}
	if err := fc.enc.Encode(item); err != nil {
		berr := verror.New(errArgEncoding, fc.ctx, -1, err)
		return fc.close(berr)
	}
	return nil
}

func (fc *flowXClient) Recv(itemptr interface{}) error {
	defer apilog.LogCallf(nil, "itemptr=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	switch {
	case fc.response.Error != nil:
		return verror.New(verror.ErrBadProtocol, fc.ctx, fc.response.Error)
	case fc.response.EndStreamResults:
		return io.EOF
	}

	// Decode the response header and handle errors and EOF.
	if err := fc.dec.Decode(&fc.response); err != nil {
		id, verr := decodeNetError(fc.ctx, err)
		berr := verror.New(id, fc.ctx, verror.New(errResponseDecoding, fc.ctx, verr))
		return fc.close(berr)
	}
	if fc.response.Error != nil {
		return fc.response.Error
	}
	if fc.response.EndStreamResults {
		// Return EOF to indicate to the caller that there are no more stream
		// results.  Any error sent by the server is kept in fc.response.Error, and
		// returned to the user in Finish.
		return io.EOF
	}
	// Decode the streaming result.
	if err := fc.dec.Decode(itemptr); err != nil {
		id, verr := decodeNetError(fc.ctx, err)
		berr := verror.New(id, fc.ctx, verror.New(errResponseDecoding, fc.ctx, verr))
		// TODO(cnicolaou): should we be caching this?
		fc.response.Error = berr
		return fc.close(berr)
	}
	return nil
}

func (fc *flowXClient) CloseSend() error {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return fc.closeSend()
}

func (fc *flowXClient) closeSend() error {
	fc.sendClosedMu.Lock()
	defer fc.sendClosedMu.Unlock()
	if fc.sendClosed {
		return nil
	}
	if err := fc.enc.Encode(rpc.Request{EndStreamArgs: true}); err != nil {
		// TODO(caprita): Indiscriminately closing the flow below causes
		// a race as described in:
		// https://docs.google.com/a/google.com/document/d/1C0kxfYhuOcStdV7tnLZELZpUhfQCZj47B0JrzbE29h8/edit
		//
		// There should be a finer grained way to fix this (for example,
		// encoding errors should probably still result in closing the
		// flow); on the flip side, there may exist other instances
		// where we are closing the flow but should not.
		//
		// For now, commenting out the line below removes the flakiness
		// from our existing unit tests, but this needs to be revisited
		// and fixed correctly.
		//
		//   return fc.close(verror.ErrBadProtocolf("rpc: end stream args encoding failed: %v", err))
	}
	fc.sendClosed = true
	return nil
}

// TODO(toddw): Should we require Finish to be called, even if send or recv
// return an error?
func (fc *flowXClient) Finish(resultptrs ...interface{}) error {
	defer apilog.LogCallf(nil, "resultptrs...=%v", resultptrs)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	defer vtrace.GetSpan(fc.ctx).Finish()
	if fc.finished {
		err := verror.New(errClientFinishAlreadyCalled, fc.ctx)
		return fc.close(verror.New(verror.ErrBadState, fc.ctx, err))
	}
	fc.finished = true

	// Call closeSend implicitly, if the user hasn't already called it.  There are
	// three cases:
	// 1) Server is blocked on Recv waiting for the final request message.
	// 2) Server has already finished processing, the final response message and
	//    out args are queued up on the client, and the flow is closed.
	// 3) Between 1 and 2: the server isn't blocked on Recv, but the final
	//    response and args aren't queued up yet, and the flow isn't closed.
	//
	// We must call closeSend to handle case (1) and unblock the server; otherwise
	// we'll deadlock with both client and server waiting for each other.  We must
	// ignore the error (if any) to handle case (2).  In that case the flow is
	// closed, meaning writes will fail and reads will succeed, and closeSend will
	// always return an error.  But this isn't a "real" error; the client should
	// read the rest of the results and succeed.
	_ = fc.closeSend()
	// Decode the response header, if it hasn't already been decoded by Recv.
	if fc.response.Error == nil && !fc.response.EndStreamResults {
		if err := fc.dec.Decode(&fc.response); err != nil {
			id, verr := decodeNetError(fc.ctx, err)
			berr := verror.New(id, fc.ctx, verror.New(errResponseDecoding, fc.ctx, verr))
			return fc.close(berr)
		}

		// The response header must indicate the streaming results have ended.
		if fc.response.Error == nil && !fc.response.EndStreamResults {
			berr := verror.New(errRemainingStreamResults, fc.ctx)
			return fc.close(berr)
		}
	}
	// Incorporate any VTrace info that was returned.
	vtrace.GetStore(fc.ctx).Merge(fc.response.TraceResponse)
	if fc.response.Error != nil {
		id := verror.ErrorID(fc.response.Error)
		if id == verror.ErrNoAccess.ID {
			// In case the error was caused by a bad discharge, we do not want to get stuck
			// with retrying again and again with this discharge. As there is no direct way
			// to detect it, we conservatively flush all discharges we used from the cache.
			// TODO(ataly,andreser): add verror.BadDischarge and handle it explicitly?
			l := len(fc.flow.LocalDischarges())
			dis := make([]security.Discharge, 0, l)
			for _, d := range fc.flow.LocalDischarges() {
				dis = append(dis, d)
			}
			fc.ctx.VI(3).Infof("Discarding %d discharges as RPC failed with %v", l, fc.response.Error)
			v23.GetPrincipal(fc.ctx).BlessingStore().ClearDischarges(dis...)
		}
		if id == errBadNumInputArgs.ID || id == errBadInputArg.ID {
			return fc.close(verror.New(verror.ErrBadProtocol, fc.ctx, fc.response.Error))
		}
		return fc.close(verror.Convert(verror.ErrInternal, fc.ctx, fc.response.Error))
	}
	if got, want := fc.response.NumPosResults, uint64(len(resultptrs)); got != want {
		berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errMismatchedResults, fc.ctx, got, want))
		return fc.close(berr)
	}
	for ix, r := range resultptrs {
		if err := fc.dec.Decode(r); err != nil {
			id, verr := decodeNetError(fc.ctx, err)
			berr := verror.New(id, fc.ctx, verror.New(errResultDecoding, fc.ctx, ix, verr))
			return fc.close(berr)
		}
	}
	return fc.close(nil)
}

func (fc *flowXClient) RemoteBlessings() ([]string, security.Blessings) {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return fc.remoteBNames, fc.flow.RemoteBlessings()
}
