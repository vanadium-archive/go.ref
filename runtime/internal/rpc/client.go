// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"fmt"
	"io"
	"math/rand"
	"net"
	"reflect"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	vtime "v.io/v23/vdlroot/time"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"

	"v.io/x/ref/lib/apilog"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
)

const pkgPath = "v.io/x/ref/runtime/internal/rpc"

func reg(id, msg string) verror.IDAction {
	// Note: the error action is never used and is instead computed
	// at a higher level. The errors here are purely for informational
	// purposes.
	return verror.Register(verror.ID(pkgPath+id), verror.NoRetry, msg)
}

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errClientCloseAlreadyCalled  = reg(".errCloseAlreadyCalled", "rpc.Client.Close has already been called")
	errClientFinishAlreadyCalled = reg(".errFinishAlreadyCalled", "rpc.ClientCall.Finish has already been called")
	errNonRootedName             = reg(".errNonRootedName", "{3} does not appear to contain an address")
	errInvalidEndpoint           = reg(".errInvalidEndpoint", "failed to parse endpoint")
	errIncompatibleEndpoint      = reg(".errIncompatibleEndpoint", "incompatible endpoint")
	errRequestEncoding           = reg(".errRequestEncoding", "failed to encode request {3}{:4}")
	errDischargeEncoding         = reg(".errDischargeEncoding", "failed to encode discharges {:3}")
	errBlessingEncoding          = reg(".errBlessingEncoding", "failed to encode blessing {3}{:4}")
	errArgEncoding               = reg(".errArgEncoding", "failed to encode arg #{3}{:4:}")
	errMismatchedResults         = reg(".errMismatchedResults", "got {3} results, but want {4}")
	errResultDecoding            = reg(".errResultDecoding", "failed to decode result #{3}{:4}")
	errResponseDecoding          = reg(".errResponseDecoding", "failed to decode response{:3}")
	errRemainingStreamResults    = reg(".errRemaingStreamResults", "stream closed with remaining stream results")
	errBlessingGrant             = reg(".errBlessingGrant", "failed to grant blessing to server with blessings{:3}")
	errBlessingAdd               = reg(".errBlessingAdd", "failed to add blessing granted to server{:3}")
	errPeerAuthorizeFailed       = reg(".errPeerAuthorizedFailed", "failed to authorize flow with remote blessings{:3} {:4}")

	errPrepareBlessingsAndDischarges = reg(".prepareBlessingsAndDischarges", "failed to prepare blessings and discharges: remote blessings{:3} {:4}")

	errDischargeImpetus = reg(".errDischargeImpetus", "couldn't make discharge impetus{:3}")
	errNoPrincipal      = reg(".errNoPrincipal", "principal required for secure connections")
)

type client struct {
	streamMgr          stream.Manager
	ns                 namespace.T
	vcOpts             []stream.VCOpt // vc opts passed to dial
	preferredProtocols []string
	vcCache            *vc.VCCache
	wg                 sync.WaitGroup
	dc                 vc.DischargeClient

	mu      sync.Mutex
	closed  bool
	closech chan struct{}
}

var _ rpc.Client = (*client)(nil)

func DeprecatedNewClient(streamMgr stream.Manager, ns namespace.T, opts ...rpc.ClientOpt) rpc.Client {
	c := &client{
		streamMgr: streamMgr,
		ns:        ns,
		vcCache:   vc.NewVCCache(),
		closech:   make(chan struct{}),
	}
	c.dc = InternalNewDischargeClient(nil, c, 0)
	for _, opt := range opts {
		// Collect all client opts that are also vc opts.
		switch v := opt.(type) {
		case stream.VCOpt:
			c.vcOpts = append(c.vcOpts, v)
		case PreferredProtocols:
			c.preferredProtocols = v
		}
	}
	return c
}

func (c *client) createFlow(ctx *context.T, principal security.Principal, ep naming.Endpoint, vcOpts []stream.VCOpt, flowOpts []stream.FlowOpt) (stream.Flow, *verror.SubErr) {
	suberr := func(err error) *verror.SubErr {
		return &verror.SubErr{Err: err, Options: verror.Print}
	}

	found, err := c.vcCache.ReservedFind(ep, principal)
	if err != nil {
		return nil, suberr(verror.New(errClientCloseAlreadyCalled, ctx))
	}
	defer c.vcCache.Unreserve(ep, principal)
	if found != nil {
		// We are serializing the creation of all flows per VC. This is okay
		// because if one flow creation is to block, it is likely that all others
		// for that VC would block as well.
		if flow, err := found.Connect(flowOpts...); err == nil {
			return flow, nil
		}
		// If the vc fails to establish a new flow, we assume it's
		// broken, remove it from the cache, and proceed to establishing
		// a new vc.
		//
		// TODO(suharshs): The decision to redial 1 time when the dialing the vc
		// in the cache fails is a bit inconsistent with the behavior when a newly
		// dialed vc.Connect fails. We should revisit this.
		//
		// TODO(caprita): Should we distinguish errors due to vc being
		// closed from other errors?  If not, should we call vc.Close()
		// before removing the vc from the cache?
		if err := c.vcCache.Delete(found); err != nil {
			return nil, suberr(verror.New(errClientCloseAlreadyCalled, ctx))
		}
	}

	sm := c.streamMgr
	v, err := sm.Dial(ctx, ep, vcOpts...)
	if err != nil {
		return nil, suberr(err)
	}

	flow, err := v.Connect(flowOpts...)
	if err != nil {
		return nil, suberr(err)
	}

	if err := c.vcCache.Insert(v.(*vc.VC)); err != nil {
		sm.ShutdownEndpoint(ep)
		return nil, suberr(verror.New(errClientCloseAlreadyCalled, ctx))
	}

	return flow, nil
}

// A randomized exponential backoff. The randomness deters error convoys
// from forming.  The first time you retry n should be 0, then 1 etc.
func backoff(n uint, deadline time.Time) bool {
	// This is ((100 to 200) * 2^n) ms.
	b := time.Duration((100+rand.Intn(100))<<n) * time.Millisecond
	if b > maxBackoff {
		b = maxBackoff
	}
	r := deadline.Sub(time.Now())
	if b > r {
		// We need to leave a little time for the call to start or
		// we'll just timeout in startCall before we actually do
		// anything.  If we just have a millisecond left, give up.
		if r <= time.Millisecond {
			return false
		}
		b = r - time.Millisecond
	}
	time.Sleep(b)
	return true
}

func (c *client) StartCall(ctx *context.T, name, method string, args []interface{}, opts ...rpc.CallOpt) (rpc.ClientCall, error) {
	defer apilog.LogCallf(ctx, "name=%.10s...,method=%.10s...,args=,opts...=%v", name, method, opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return c.startCall(ctx, name, method, args, opts)
}

func (c *client) Call(ctx *context.T, name, method string, inArgs, outArgs []interface{}, opts ...rpc.CallOpt) error {
	defer apilog.LogCallf(ctx, "name=%.10s...,method=%.10s...,inArgs=,outArgs=,opts...=%v", name, method, opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	deadline := getDeadline(ctx, opts)

	var lastErr error
	for retries := uint(0); ; retries++ {
		call, err := c.startCall(ctx, name, method, inArgs, opts)
		if err != nil {
			return err
		}
		err = call.Finish(outArgs...)
		if err == nil {
			return nil
		}
		lastErr = err
		// We only retry if RetryBackoff is returned by the application because other
		// RetryConnection and RetryRefetch required actions by the client before
		// retrying.
		if !shouldRetryBackoff(verror.Action(lastErr), deadline, opts) {
			ctx.VI(4).Infof("Cannot retry after error: %s", lastErr)
			break
		}
		if !backoff(retries, deadline) {
			break
		}
		ctx.VI(4).Infof("Retrying due to error: %s", lastErr)
	}
	return lastErr
}

func getDeadline(ctx *context.T, opts []rpc.CallOpt) time.Time {
	// Context specified deadline.
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		// Default deadline.
		deadline = time.Now().Add(defaultCallTimeout)
	}
	if r, ok := getRetryTimeoutOpt(opts); ok {
		// Caller specified deadline.
		deadline = time.Now().Add(r)
	}
	return deadline
}

func shouldRetryBackoff(action verror.ActionCode, deadline time.Time, opts []rpc.CallOpt) bool {
	switch {
	case noRetry(opts):
		return false
	case action != verror.RetryBackoff:
		return false
	case time.Now().After(deadline):
		return false
	}
	return true
}

func shouldRetry(action verror.ActionCode, requireResolve bool, deadline time.Time, opts []rpc.CallOpt) bool {
	switch {
	case noRetry(opts):
		return false
	case action != verror.RetryConnection && action != verror.RetryRefetch:
		return false
	case time.Now().After(deadline):
		return false
	case requireResolve && getNoNamespaceOpt(opts):
		// If we're skipping resolution and there are no servers for
		// this call retrying is not going to help, we can't come up
		// with new servers if there is no resolution.
		return false
	}
	return true
}

func mkDischargeImpetus(serverBlessings []string, method string, args []interface{}) (security.DischargeImpetus, error) {
	var impetus security.DischargeImpetus
	if len(serverBlessings) > 0 {
		impetus.Server = make([]security.BlessingPattern, len(serverBlessings))
		for i, b := range serverBlessings {
			impetus.Server[i] = security.BlessingPattern(b)
		}
	}
	impetus.Method = method
	if len(args) > 0 {
		impetus.Arguments = make([]*vdl.Value, len(args))
		for i, a := range args {
			vArg, err := vdl.ValueFromReflect(reflect.ValueOf(a))
			if err != nil {
				return security.DischargeImpetus{}, err
			}
			impetus.Arguments[i] = vArg
		}
	}
	return impetus, nil
}

// startCall ensures StartCall always returns verror.E.
func (c *client) startCall(ctx *context.T, name, method string, args []interface{}, opts []rpc.CallOpt) (rpc.ClientCall, error) {
	if !ctx.Initialized() {
		return nil, verror.ExplicitNew(verror.ErrBadArg, i18n.LangID("en-us"), "<rpc.Client>", "StartCall", "context not initialized")
	}
	ctx, span := vtrace.WithNewSpan(ctx, fmt.Sprintf("<rpc.Client>%q.%s", name, method))
	if err := canCreateServerAuthorizer(ctx, opts); err != nil {
		return nil, verror.New(verror.ErrBadArg, ctx, err)
	}

	deadline := getDeadline(ctx, opts)

	var lastErr error
	for retries := uint(0); ; retries++ {
		call, action, requireResolve, err := c.tryCall(ctx, name, method, args, opts)
		if err == nil {
			return call, nil
		}
		lastErr = err
		if !shouldRetry(action, requireResolve, deadline, opts) {
			span.Annotatef("Cannot retry after error: %s", err)
			break
		}
		if !backoff(retries, deadline) {
			break
		}
		span.Annotatef("Retrying due to error: %s", err)
	}
	return nil, lastErr
}

type serverStatus struct {
	index             int
	server, suffix    string
	flow              stream.Flow
	blessings         []string                    // authorized server blessings
	rejectedBlessings []security.RejectedBlessing // rejected server blessings
	serverErr         *verror.SubErr
}

func suberrName(server, name, method string) string {
	// In the case the client directly dialed an endpoint we want to avoid printing
	// the endpoint twice.
	if server == name {
		return fmt.Sprintf("%s.%s", server, method)
	}
	return fmt.Sprintf("%s:%s.%s", server, name, method)
}

// tryCreateFlow attempts to establish a Flow to "server" (which must be a
// rooted name), over which a method invocation request could be sent.
//
// The server at the remote end of the flow is authorized using the provided
// authorizer, both during creation of the VC underlying the flow and the
// flow itself.
// TODO(cnicolaou): implement real, configurable load balancing.
func (c *client) tryCreateFlow(ctx *context.T, principal security.Principal, index int, name, server, method string, auth security.Authorizer, ch chan<- *serverStatus, vcOpts []stream.VCOpt, flowOpts []stream.FlowOpt) {
	defer c.wg.Done()
	status := &serverStatus{index: index, server: server}
	var span vtrace.Span
	ctx, span = vtrace.WithNewSpan(ctx, "<client>tryCreateFlow")
	span.Annotatef("address:%v", server)
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
	if status.flow, status.serverErr = c.createFlow(ctx, principal, ep, append(vcOpts, &vc.ServerAuthorizer{Suffix: status.suffix, Method: method, Policy: auth}), flowOpts); status.serverErr != nil {
		status.serverErr.Name = suberrName(server, name, method)
		ctx.VI(2).Infof("rpc: Failed to create Flow with %v: %v", server, status.serverErr.Err)
		return
	}

	// Authorize the remote end of the flow using the provided authorizer.
	if status.flow.LocalPrincipal() == nil {
		// LocalPrincipal is nil which means we are operating under
		// SecurityNone.
		return
	}

	seccall := security.NewCall(&security.CallParams{
		LocalPrincipal:   status.flow.LocalPrincipal(),
		LocalBlessings:   status.flow.LocalBlessings(),
		RemoteBlessings:  status.flow.RemoteBlessings(),
		LocalEndpoint:    status.flow.LocalEndpoint(),
		RemoteEndpoint:   status.flow.RemoteEndpoint(),
		RemoteDischarges: status.flow.RemoteDischarges(),
		Method:           method,
		Suffix:           status.suffix,
	})
	if err := auth.Authorize(ctx, seccall); err != nil {
		// We will test for errPeerAuthorizeFailed in failedTryCall and report
		// verror.ErrNotTrusted
		status.serverErr = suberr(verror.New(errPeerAuthorizeFailed, ctx, status.flow.RemoteBlessings(), err))
		ctx.VI(2).Infof("rpc: Failed to authorize Flow created with server %v: %s", server, status.serverErr.Err)
		status.flow.Close()
		status.flow = nil
		return
	}
	status.blessings, status.rejectedBlessings = security.RemoteBlessingNames(ctx, seccall)
	return
}

// tryCall makes a single attempt at a call. It may connect to multiple servers
// (all that serve "name"), but will invoke the method on at most one of them
// (the server running on the most preferred protcol and network amongst all
// the servers that were successfully connected to and authorized).
// if requireResolve is true on return, then we shouldn't bother retrying unless
// you can re-resolve.
func (c *client) tryCall(ctx *context.T, name, method string, args []interface{}, opts []rpc.CallOpt) (call rpc.ClientCall, action verror.ActionCode, requireResolve bool, err error) {
	var resolved *naming.MountEntry
	var blessingPattern security.BlessingPattern
	blessingPattern, name = security.SplitPatternName(name)
	if resolved, err = c.ns.Resolve(ctx, name, getNamespaceOpts(opts)...); err != nil {
		// We always return NoServers as the error so that the caller knows
		// that's ok to retry the operation since the name may be registered
		// in the near future.
		switch {
		case verror.ErrorID(err) == naming.ErrNoSuchName.ID:
			return nil, verror.RetryRefetch, false, verror.New(verror.ErrNoServers, ctx, name)
		case verror.ErrorID(err) == verror.ErrNoServers.ID:
			// Avoid wrapping errors unnecessarily.
			return nil, verror.NoRetry, false, err
		case verror.ErrorID(err) == verror.ErrTimeout.ID:
			// If the call timed out we actually want to propagate that error.
			return nil, verror.NoRetry, false, err
		default:
			return nil, verror.NoRetry, false, verror.New(verror.ErrNoServers, ctx, name, err)
		}
	} else {
		if len(resolved.Servers) == 0 {
			// This should never happen.
			return nil, verror.NoRetry, true, verror.New(verror.ErrInternal, ctx, name)
		}
		// An empty set of protocols means all protocols...
		if resolved.Servers, err = filterAndOrderServers(resolved.Servers, c.preferredProtocols); err != nil {
			return nil, verror.RetryRefetch, true, verror.New(verror.ErrNoServers, ctx, name, err)
		}
	}

	// We need to ensure calls to v23 factory methods do not occur during runtime
	// initialization. Currently, the agent, which uses SecurityNone, is the only caller
	// during runtime initialization. We would like to set the principal in the context
	// to nil if we are running in SecurityNone, but this always results in a panic since
	// the agent client would trigger the call v23.WithPrincipal during runtime
	// initialization. So, we gate the call to v23.GetPrincipal instead since the agent
	// client will have callEncrypted == false.
	// Potential solutions to this are:
	// (1) Create a separate client for the agent so that this code doesn't have to
	//     account for its use during runtime initialization.
	// (2) Have a ctx.IsRuntimeInitialized() method that we can additionally predicate
	//     on here.
	var principal security.Principal
	if callEncrypted(opts) {
		if principal = v23.GetPrincipal(ctx); principal == nil {
			return nil, verror.NoRetry, false, verror.New(errNoPrincipal, ctx)
		}
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
	attempts := len(resolved.Servers)

	responses := make([]*serverStatus, attempts)
	ch := make(chan *serverStatus, attempts)
	vcOpts, flowOpts := translateStreamOpts(opts)
	vcOpts = append(vcOpts, c.vcOpts...)

	authorizer := newServerAuthorizer(blessingPattern, opts...)
	for i, server := range resolved.Names() {
		// Create a copy of vcOpts for each call to tryCreateFlow
		// to avoid concurrent tryCreateFlows from stepping on each
		// other while manipulating their copy of the options.
		vcOptsCopy := make([]stream.VCOpt, len(vcOpts))
		copy(vcOptsCopy, vcOpts)
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, verror.NoRetry, false, verror.New(errClientCloseAlreadyCalled, ctx)
		}
		c.wg.Add(1)
		c.mu.Unlock()

		go c.tryCreateFlow(ctx, principal, i, name, server, method, authorizer, ch, vcOptsCopy, flowOpts)
	}

	var timeoutChan <-chan time.Time
	if deadline, ok := ctx.Deadline(); ok {
		timeoutChan = time.After(deadline.Sub(time.Now()))
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
		case <-timeoutChan:
			ctx.VI(2).Infof("rpc: timeout on connection to server %v ", name)
			_, _, _, err := c.failedTryCall(ctx, name, method, responses, ch)
			if verror.ErrorID(err) != verror.ErrTimeout.ID {
				return nil, verror.NoRetry, false, verror.New(verror.ErrTimeout, ctx, err)
			}
			return nil, verror.NoRetry, false, err
		}

		dc := c.dc
		if shouldNotFetchDischarges(opts) {
			dc = nil
		}
		// Process new responses, in priority order.
		numResponses := 0
		for _, r := range responses {
			if r != nil {
				numResponses++
			}
			if r == nil || r.flow == nil {
				continue
			}

			doneChan := ctx.Done()
			r.flow.SetDeadline(doneChan)
			fc, err := newFlowClient(ctx, r.flow, r.blessings, dc)
			if err != nil {
				return nil, verror.NoRetry, false, err
			}

			if err := fc.prepareBlessingsAndDischarges(ctx, method, r.suffix, args, r.rejectedBlessings, opts); err != nil {
				r.serverErr = &verror.SubErr{
					Name:    suberrName(r.server, name, method),
					Options: verror.Print,
					Err:     verror.New(verror.ErrNotTrusted, nil, verror.New(errPrepareBlessingsAndDischarges, ctx, r.flow.RemoteBlessings(), err)),
				}
				ctx.VI(2).Infof("rpc: err: %s", r.serverErr)
				r.flow.Close()
				r.flow = nil
				continue
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
			go cleanupTryCall(r, responses, ch)

			if doneChan != nil {
				go func() {
					select {
					case <-doneChan:
						vtrace.GetSpan(fc.ctx).Annotate("Canceled")
						fc.flow.Cancel()
					case <-fc.flow.Closed():
					}
				}()
			}

			deadline, _ := ctx.Deadline()
			if verr := fc.start(r.suffix, method, args, deadline); verr != nil {
				return nil, verror.NoRetry, false, verr
			}
			return fc, verror.NoRetry, false, nil
		}
		if numResponses == len(responses) {
			return c.failedTryCall(ctx, name, method, responses, ch)
		}
	}
}

// cleanupTryCall ensures we've waited for every response from the tryCreateFlow
// goroutines, and have closed the flow from each one except skip.  This is a
// blocking function; it should be called in its own goroutine.
func cleanupTryCall(skip *serverStatus, responses []*serverStatus, ch chan *serverStatus) {
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
			r.flow.Close()
		}
	}
	// Now we just need to wait for the pending responses and close their flows.
	for i := 0; i < numPending; i++ {
		if r := <-ch; r.flow != nil {
			r.flow.Close()
		}
	}
}

// failedTryCall performs ©asynchronous cleanup for tryCall, and returns an
// appropriate error from the responses we've already received.  All parallel
// calls in tryCall failed or we timed out if we get here.
func (c *client) failedTryCall(ctx *context.T, name, method string, responses []*serverStatus, ch chan *serverStatus) (rpc.ClientCall, verror.ActionCode, bool, error) {
	go cleanupTryCall(nil, responses, ch)
	c.ns.FlushCacheEntry(ctx, name)
	suberrs := []verror.SubErr{}
	topLevelError := verror.ErrNoServers
	topLevelAction := verror.RetryRefetch
	onlyErrNetwork := true
	for _, r := range responses {
		if r != nil && r.serverErr != nil && r.serverErr.Err != nil {
			switch verror.ErrorID(r.serverErr.Err) {
			case stream.ErrNotTrusted.ID, verror.ErrNotTrusted.ID, errPeerAuthorizeFailed.ID:
				topLevelError = verror.ErrNotTrusted
				topLevelAction = verror.NoRetry
				onlyErrNetwork = false
			case stream.ErrAborted.ID, stream.ErrNetwork.ID:
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

// prepareBlessingsAndDischarges prepares blessings and discharges for
// the call.
//
// This includes: (1) preparing blessings that must be granted to the
// server, (2) preparing blessings that the client authenticates with,
// and, (3) preparing any discharges for third-party caveats on the client's
// blessings.
func (fc *flowClient) prepareBlessingsAndDischarges(ctx *context.T, method, suffix string, args []interface{}, rejectedServerBlessings []security.RejectedBlessing, opts []rpc.CallOpt) error {
	// LocalPrincipal is nil which means we are operating under
	// SecurityNone.
	if fc.flow.LocalPrincipal() == nil {
		return nil
	}

	// Fetch blessings from the client's blessing store that are to be
	// shared with the server, if any.
	fc.blessings = fc.flow.LocalPrincipal().BlessingStore().ForPeer(fc.server...)

	// Fetch any discharges for third-party caveats on the client's blessings.
	if !fc.blessings.IsZero() && fc.dc != nil {
		impetus, err := mkDischargeImpetus(fc.server, method, args)
		if err != nil {
			return verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errDischargeImpetus, nil, err))
		}
		fc.discharges = fc.dc.PrepareDischarges(fc.ctx, fc.blessings.ThirdPartyCaveats(), impetus)
	}

	// Prepare blessings that must be granted to the server (using any
	// rpc.Granter implementation in 'opts').
	//
	// NOTE(ataly, suharshs): Before invoking the granter, we set the parameters
	// of the current call. The user can now retrieve the principal via
	// v23.GetPrincipal(ctx), or via call.LocalPrincipal(). While in theory the
	// two principals can be different, the flow.LocalPrincipal == nil check at
	// the beginning of this method ensures that the two are the same and non-nil
	// at this point in the code.
	ldischargeMap := make(map[string]security.Discharge)
	for _, d := range fc.discharges {
		ldischargeMap[d.ID()] = d
	}
	seccall := security.NewCall(&security.CallParams{
		LocalPrincipal:   fc.flow.LocalPrincipal(),
		LocalBlessings:   fc.blessings,
		RemoteBlessings:  fc.flow.RemoteBlessings(),
		LocalEndpoint:    fc.flow.LocalEndpoint(),
		RemoteEndpoint:   fc.flow.RemoteEndpoint(),
		LocalDischarges:  ldischargeMap,
		RemoteDischarges: fc.flow.RemoteDischarges(),
		Method:           method,
		Suffix:           suffix,
	})
	if err := fc.prepareGrantedBlessings(ctx, seccall, opts); err != nil {
		return err
	}
	return nil
}

func (fc *flowClient) prepareGrantedBlessings(ctx *context.T, call security.Call, opts []rpc.CallOpt) error {
	for _, o := range opts {
		switch v := o.(type) {
		case rpc.Granter:
			if b, err := v.Grant(ctx, call); err != nil {
				return verror.New(errBlessingGrant, fc.ctx, err)
			} else if fc.grantedBlessings, err = security.UnionOfBlessings(fc.grantedBlessings, b); err != nil {
				return verror.New(errBlessingAdd, fc.ctx, err)
			}
		}
	}
	return nil
}

func (c *client) Close() {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.closech)
	}
	c.mu.Unlock()
	for _, v := range c.vcCache.Close() {
		c.streamMgr.ShutdownEndpoint(v.RemoteEndpoint())
	}
	c.wg.Wait()
}

func (c *client) Closed() <-chan struct{} {
	return c.closech
}

// flowClient implements the RPC client-side protocol for a single RPC, over a
// flow that's already connected to the server.
type flowClient struct {
	ctx      *context.T   // context to annotate with call details
	dec      *vom.Decoder // to decode responses and results from the server
	enc      *vom.Encoder // to encode requests and args to the server
	server   []string     // Blessings bound to the server that authorize it to receive the RPC request from the client.
	flow     stream.Flow  // the underlying flow
	response rpc.Response // each decoded response message is kept here

	discharges []security.Discharge // discharges used for this request
	dc         vc.DischargeClient   // client-global discharge-client

	blessings        security.Blessings // the local blessings for the current RPC.
	grantedBlessings security.Blessings // the blessings granted to the server.

	sendClosedMu sync.Mutex
	sendClosed   bool // is the send side already closed? GUARDED_BY(sendClosedMu)
	finished     bool // has Finish() already been called?
}

var _ rpc.ClientCall = (*flowClient)(nil)
var _ rpc.Stream = (*flowClient)(nil)

func newFlowClient(ctx *context.T, flow stream.Flow, server []string, dc vc.DischargeClient) (*flowClient, error) {
	fc := &flowClient{
		ctx:    ctx,
		flow:   flow,
		server: server,
		dc:     dc,
	}
	typeenc := flow.VCDataCache().Get(vc.TypeEncoderKey{})
	if typeenc == nil {
		fc.enc = vom.NewEncoder(flow)
		fc.dec = vom.NewDecoder(flow)
	} else {
		fc.enc = vom.NewEncoderWithTypeEncoder(flow, typeenc.(*vom.TypeEncoder))
		typedec := flow.VCDataCache().Get(vc.TypeDecoderKey{})
		fc.dec = vom.NewDecoderWithTypeDecoder(flow, typedec.(*vom.TypeDecoder))
	}
	return fc, nil
}

// close determines the appropriate error to return, in particular,
// if a timeout or cancelation has occured then any error
// is turned into a timeout or cancelation as appropriate.
// Cancelation takes precedence over timeout. This is needed because
// a timeout can lead to any other number of errors due to the underlying
// network connection being shutdown abruptly.
func (fc *flowClient) close(err error) error {
	subErr := verror.SubErr{Err: err, Options: verror.Print}
	subErr.Name = "remote=" + fc.flow.RemoteEndpoint().String()
	if cerr := fc.flow.Close(); cerr != nil && err == nil {
		return verror.New(verror.ErrInternal, fc.ctx, subErr)
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

func (fc *flowClient) start(suffix, method string, args []interface{}, deadline time.Time) error {
	// Encode the Blessings information for the client to authorize the flow.
	var blessingsRequest rpc.BlessingsRequest
	if fc.flow.LocalPrincipal() != nil {
		blessingsRequest = clientEncodeBlessings(fc.flow.VCDataCache(), fc.blessings)
	}
	req := rpc.Request{
		Suffix:           suffix,
		Method:           method,
		NumPosArgs:       uint64(len(args)),
		Deadline:         vtime.Deadline{Time: deadline},
		GrantedBlessings: fc.grantedBlessings,
		Blessings:        blessingsRequest,
		Discharges:       fc.discharges,
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

func (fc *flowClient) Send(item interface{}) error {
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

// decodeNetError tests for a net.Error from the lower stream code and
// translates it into an appropriate error to be returned by the higher level
// RPC api calls. It also tests for the net.Error being a stream.NetError
// and if so, uses the error it stores rather than the stream.NetError itself
// as its retrun value. This allows for the stack trace of the original
// error to be chained to that of any verror created with it as a first parameter.
func decodeNetError(ctx *context.T, err error) (verror.IDAction, error) {
	if neterr, ok := err.(net.Error); ok {
		if streamNeterr, ok := err.(*stream.NetError); ok {
			err = streamNeterr.Err() // return the error stored in the stream.NetError
		}
		if neterr.Timeout() || neterr.Temporary() {
			// If a read is canceled in the lower levels we see
			// a timeout error - see readLocked in vc/reader.go
			if ctx.Err() == context.Canceled {
				return verror.ErrCanceled, err
			}
			return verror.ErrTimeout, err
		}
	}
	if id := verror.ErrorID(err); id != verror.ErrUnknown.ID {
		return verror.IDAction{
			ID:     id,
			Action: verror.Action(err),
		}, err
	}
	return verror.ErrBadProtocol, err
}

func (fc *flowClient) Recv(itemptr interface{}) error {
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

func (fc *flowClient) CloseSend() error {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return fc.closeSend()
}

// closeSend ensures CloseSend always returns verror.E.
func (fc *flowClient) closeSend() error {
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

func (fc *flowClient) Finish(resultptrs ...interface{}) error {
	defer apilog.LogCallf(nil, "resultptrs...=%v", resultptrs)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	err := fc.finish(resultptrs...)
	vtrace.GetSpan(fc.ctx).Finish()
	return err
}

// finish ensures Finish always returns a verror.E.
func (fc *flowClient) finish(resultptrs ...interface{}) error {
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
			select {
			case <-fc.ctx.Done():
				if fc.ctx.Err() == context.Canceled {
					return verror.New(verror.ErrCanceled, fc.ctx)
				}
				return verror.New(verror.ErrTimeout, fc.ctx)
			default:
				id, verr := decodeNetError(fc.ctx, err)
				berr := verror.New(id, fc.ctx, verror.New(errResponseDecoding, fc.ctx, verr))
				return fc.close(berr)
			}
		}
		// The response header must indicate the streaming results have ended.
		if fc.response.Error == nil && !fc.response.EndStreamResults {
			berr := verror.New(errRemainingStreamResults, fc.ctx)
			return fc.close(berr)
		}
	}
	if fc.response.AckBlessings {
		clientAckBlessings(fc.flow.VCDataCache(), fc.blessings)
	}
	// Incorporate any VTrace info that was returned.
	vtrace.GetStore(fc.ctx).Merge(fc.response.TraceResponse)
	if fc.response.Error != nil {
		id := verror.ErrorID(fc.response.Error)
		if id == verror.ErrNoAccess.ID && fc.dc != nil {
			// In case the error was caused by a bad discharge, we do not want to get stuck
			// with retrying again and again with this discharge. As there is no direct way
			// to detect it, we conservatively flush all discharges we used from the cache.
			// TODO(ataly,andreser): add verror.BadDischarge and handle it explicitly?
			fc.ctx.VI(3).Infof("Discarding %d discharges as RPC failed with %v", len(fc.discharges), fc.response.Error)
			fc.dc.Invalidate(fc.ctx, fc.discharges...)
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

func (fc *flowClient) RemoteBlessings() ([]string, security.Blessings) {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return fc.server, fc.flow.RemoteBlessings()
}

func bpatterns(patterns []string) []security.BlessingPattern {
	if patterns == nil {
		return nil
	}
	bpatterns := make([]security.BlessingPattern, len(patterns))
	for i, p := range patterns {
		bpatterns[i] = security.BlessingPattern(p)
	}
	return bpatterns
}
