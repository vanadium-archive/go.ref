// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	vtime "v.io/v23/vdlroot/time"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"

	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
	"v.io/x/ref/profiles/internal/rpc/version"
)

const pkgPath = "v.io/x/ref/profiles/internal/rpc"

var (
	// Local errs that are used to provide details to the public ones.
	errClientCloseAlreadyCalled = verror.Register(pkgPath+".closeAlreadyCalled", verror.NoRetry,
		"rpc.Client.Close has already been called")

	errClientFinishAlreadyCalled = verror.Register(pkgPath+".finishAlreadyCalled", verror.NoRetry, "rpc.ClientCall.Finish has already been called")

	errNonRootedName = verror.Register(pkgPath+".nonRootedName", verror.NoRetry, "{3} does not appear to contain an address")

	errInvalidEndpoint = verror.Register(pkgPath+".invalidEndpoint", verror.RetryRefetch, "{3} is an invalid endpoint")

	errIncompatibleEndpoint = verror.Register(pkgPath+".invalidEndpoint", verror.RetryRefetch, "{3} is an incompatible endpoint")

	errNotTrusted = verror.Register(pkgPath+".notTrusted", verror.NoRetry, "name {3} not trusted using blessings {4}{:5}")

	errAuthError = verror.Register(pkgPath+".authError", verror.RetryRefetch, "authentication error from server {3}{:4}")

	errSystemRetry = verror.Register(pkgPath+".sysErrorRetryConnection", verror.RetryConnection, "{:3:}")

	errVomEncoder = verror.Register(pkgPath+".vomEncoder", verror.NoRetry, "failed to create vom encoder {:3}")
	errVomDecoder = verror.Register(pkgPath+".vomDecoder", verror.NoRetry, "failed to create vom decoder {:3}")

	errRequestEncoding = verror.Register(pkgPath+".requestEncoding", verror.NoRetry, "failed to encode request {3}{:4}")

	errDischargeEncoding = verror.Register(pkgPath+".dischargeEncoding", verror.NoRetry, "failed to encode discharges {:3}")

	errBlessingEncoding = verror.Register(pkgPath+".blessingEncoding", verror.NoRetry, "failed to encode blessing {3}{:4}")

	errArgEncoding = verror.Register(pkgPath+".argEncoding", verror.NoRetry, "failed to encode arg #{3}{:4:}")

	errMismatchedResults = verror.Register(pkgPath+".mismatchedResults", verror.NoRetry, "got {3} results, but want {4}")

	errResultDecoding = verror.Register(pkgPath+".resultDecoding", verror.NoRetry, "failed to decode result #{3}{:4}")

	errResponseDecoding = verror.Register(pkgPath+".responseDecoding", verror.NoRetry, "failed to decode response{:3}")

	errRemainingStreamResults = verror.Register(pkgPath+".remaingStreamResults", verror.NoRetry, "stream closed with remaining stream results")

	errNoBlessingsForPeer = verror.Register(pkgPath+".noBlessingsForPeer", verror.NoRetry, "no blessings tagged for peer {3}{:4}")

	errBlessingGrant = verror.Register(pkgPath+".blessingGrantFailed", verror.NoRetry, "failed to grant blessing to server with blessings {3}{:4}")

	errBlessingAdd = verror.Register(pkgPath+".blessingAddFailed", verror.NoRetry, "failed to add blessing granted to server {3}{:4}")
)

type client struct {
	streamMgr          stream.Manager
	ns                 ns.Namespace
	vcOpts             []stream.VCOpt // vc opts passed to dial
	preferredProtocols []string

	// We cache the IP networks on the device since it is not that cheap to read
	// network interfaces through os syscall.
	// TODO(jhahn): Add monitoring the network interface changes.
	ipNets []*net.IPNet

	// We support concurrent calls to StartCall and Close, so we must protect the
	// vcMap.  Everything else is initialized upon client construction, and safe
	// to use concurrently.
	vcMapMu sync.Mutex
	vcMap   map[vcMapKey]*vcInfo

	dc vc.DischargeClient
}

var _ rpc.Client = (*client)(nil)

type vcInfo struct {
	vc       stream.VC
	remoteEP naming.Endpoint
}

type vcMapKey struct {
	endpoint        string
	clientPublicKey string // clientPublicKey = "" means we are running unencrypted (i.e. SecurityNone)
}

func InternalNewClient(streamMgr stream.Manager, ns ns.Namespace, opts ...rpc.ClientOpt) (rpc.Client, error) {
	c := &client{
		streamMgr: streamMgr,
		ns:        ns,
		ipNets:    ipNetworks(),
		vcMap:     make(map[vcMapKey]*vcInfo),
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

	return c, nil
}

func (c *client) createFlow(ctx *context.T, principal security.Principal, ep naming.Endpoint, vcOpts []stream.VCOpt) (stream.Flow, error) {
	c.vcMapMu.Lock()
	defer c.vcMapMu.Unlock()
	if c.vcMap == nil {
		return nil, verror.New(errClientCloseAlreadyCalled, ctx)
	}
	vcKey := vcMapKey{endpoint: ep.String()}
	if principal != nil {
		vcKey.clientPublicKey = principal.PublicKey().String()
	}
	if vcinfo := c.vcMap[vcKey]; vcinfo != nil {
		if flow, err := vcinfo.vc.Connect(); err == nil {
			return flow, nil
		}
		// If the vc fails to establish a new flow, we assume it's
		// broken, remove it from the map, and proceed to establishing
		// a new vc.
		// TODO(caprita): Should we distinguish errors due to vc being
		// closed from other errors?  If not, should we call vc.Close()
		// before removing the vc from the map?
		delete(c.vcMap, vcKey)
	}
	sm := c.streamMgr
	c.vcMapMu.Unlock()

	vc, err := sm.Dial(ep, principal, vcOpts...)
	c.vcMapMu.Lock()
	if err != nil {
		if strings.Contains(err.Error(), "authentication failed") {
			return nil, verror.New(errAuthError, ctx, ep, err)
		} else {
			return nil, verror.New(errSystemRetry, ctx, err)
		}
	}
	if c.vcMap == nil {
		sm.ShutdownEndpoint(ep)
		return nil, verror.New(errClientCloseAlreadyCalled, ctx)
	}
	if othervc, exists := c.vcMap[vcKey]; exists {
		vc = othervc.vc
		// TODO(ashankar,toddw): Figure out how to close up the VC that
		// is discarded. vc.Close?
	} else {
		c.vcMap[vcKey] = &vcInfo{vc: vc, remoteEP: ep}
	}
	flow, err := vc.Connect()
	if err != nil {

		return nil, verror.New(errAuthError, ctx, ep, err)
	}
	return flow, nil
}

// A randomized exponential backoff. The randomness deters error convoys
// from forming.
func backoff(n int, deadline time.Time) bool {
	b := time.Duration(math.Pow(1.5+(rand.Float64()/2.0), float64(n)) * float64(time.Second))
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
	defer vlog.LogCall()()
	return c.startCall(ctx, name, method, args, opts)
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
	ctx, span := vtrace.SetNewSpan(ctx, fmt.Sprintf("<rpc.Client>%q.%s", name, method))
	if err := canCreateServerAuthorizer(ctx, opts); err != nil {
		return nil, verror.New(verror.ErrBadArg, ctx, err)
	}

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

	var lastErr error
	for retries := 0; ; retries++ {
		if retries != 0 {
			if !backoff(retries, deadline) {
				break
			}
		}
		call, action, err := c.tryCall(ctx, name, method, args, opts)
		if err == nil {
			return call, nil
		}
		lastErr = err
		shouldRetry := true
		switch {
		case getNoRetryOpt(opts):
			shouldRetry = false
		case action != verror.RetryConnection && action != verror.RetryRefetch:
			shouldRetry = false
		case time.Now().After(deadline):
			shouldRetry = false
		case action == verror.RetryRefetch && getNoResolveOpt(opts):
			// If we're skipping resolution and there are no servers for
			// this call retrying is not going to help, we can't come up
			// with new servers if there is no resolution.
			shouldRetry = false
		}
		if !shouldRetry {
			span.Annotatef("Cannot retry after error: %s", err)
			break
		}
		span.Annotatef("Retrying due to error: %s", err)
	}
	return nil, lastErr
}

type serverStatus struct {
	index             int
	suffix            string
	flow              stream.Flow
	blessings         []string                    // authorized server blessings
	rejectedBlessings []security.RejectedBlessing // rejected server blessings
	err               error
}

// tryCreateFlow attempts to establish a Flow to "server" (which must be a
// rooted name), over which a method invocation request could be sent.
//
// The server at the remote end of the flow is authorized using the provided
// authorizer, both during creation of the VC underlying the flow and the
// flow itself.
// TODO(cnicolaou): implement real, configurable load balancing.
func (c *client) tryCreateFlow(ctx *context.T, principal security.Principal, index int, name, server, method string, auth security.Authorizer, ch chan<- *serverStatus, vcOpts []stream.VCOpt) {
	status := &serverStatus{index: index}
	var span vtrace.Span
	ctx, span = vtrace.SetNewSpan(ctx, "<client>tryCreateFlow")
	span.Annotatef("address:%v", server)
	defer func() {
		ch <- status
		span.Finish()
	}()

	address, suffix := naming.SplitAddressName(server)
	if len(address) == 0 {
		status.err = verror.New(errNonRootedName, ctx, server)
		return
	}
	status.suffix = suffix

	ep, err := inaming.NewEndpoint(address)
	if err != nil {
		status.err = verror.New(errInvalidEndpoint, ctx, address)
		return
	}
	if err = version.CheckCompatibility(ep); err != nil {
		status.err = verror.New(errIncompatibleEndpoint, ctx, ep)
		return
	}
	if status.flow, status.err = c.createFlow(ctx, principal, ep, append(vcOpts, &vc.ServerAuthorizer{Suffix: status.suffix, Method: method, Policy: auth})); status.err != nil {
		vlog.VI(2).Infof("rpc: Failed to create Flow with %v: %v", server, status.err)
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
	ctx = security.SetCall(ctx, seccall)
	if err := auth.Authorize(ctx); err != nil {
		status.err = verror.New(verror.ErrNotTrusted, ctx, name, status.flow.RemoteBlessings(), err)
		vlog.VI(2).Infof("rpc: Failed to authorize Flow created with server %v: %s", server, status.err)
		status.flow.Close()
		status.flow = nil
		return
	}
	status.blessings, status.rejectedBlessings = security.RemoteBlessingNames(ctx)
	return
}

// tryCall makes a single attempt at a call. It may connect to multiple servers
// (all that serve "name"), but will invoke the method on at most one of them
// (the server running on the most preferred protcol and network amongst all
// the servers that were successfully connected to and authorized).
func (c *client) tryCall(ctx *context.T, name, method string, args []interface{}, opts []rpc.CallOpt) (rpc.ClientCall, verror.ActionCode, error) {
	var resolved *naming.MountEntry
	var err error
	var blessingPattern security.BlessingPattern
	blessingPattern, name = security.SplitPatternName(name)
	if resolved, err = c.ns.Resolve(ctx, name, getResolveOpts(opts)...); err != nil {
		vlog.Errorf("Resolve: %v", err)
		// We always return NoServers as the error so that the caller knows
		// that's ok to retry the operation since the name may be registered
		// in the near future.
		switch {
		case verror.ErrorID(err) == naming.ErrNoSuchName.ID:
			return nil, verror.RetryRefetch, verror.New(verror.ErrNoServers, ctx, name)
		case verror.ErrorID(err) == verror.ErrNoServers.ID:
			// Avoid wrapping errors unnecessarily.
			return nil, verror.NoRetry, err
		default:
			return nil, verror.NoRetry, verror.New(verror.ErrNoServers, ctx, name, err)
		}
	} else {
		if len(resolved.Servers) == 0 {
			// This should never happen.
			return nil, verror.NoRetry, verror.New(verror.ErrInternal, ctx, name)
		}
		// An empty set of protocols means all protocols...
		if resolved.Servers, err = filterAndOrderServers(resolved.Servers, c.preferredProtocols, c.ipNets); err != nil {
			return nil, verror.RetryRefetch, verror.New(verror.ErrNoServers, ctx, name, err)
		}
	}

	// We need to ensure calls to v23 factory methods do not occur during runtime
	// initialization. Currently, the agent, which uses SecurityNone, is the only caller
	// during runtime initialization. We would like to set the principal in the context
	// to nil if we are running in SecurityNone, but this always results in a panic since
	// the agent client would trigger the call v23.SetPrincipal during runtime
	// initialization. So, we gate the call to v23.GetPrincipal instead since the agent
	// client will have callEncrypted == false.
	// Potential solutions to this are:
	// (1) Create a separate client for the agent so that this code doesn't have to
	//     account for its use during runtime initialization.
	// (2) Have a ctx.IsRuntimeInitialized() method that we can additionally predicate
	//     on here.
	var principal security.Principal
	if callEncrypted(opts) {
		principal = v23.GetPrincipal(ctx)
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
	vcOpts := append(getVCOpts(opts), c.vcOpts...)
	authorizer := newServerAuthorizer(blessingPattern, opts...)
	for i, server := range resolved.Names() {
		// Create a copy of vcOpts for each call to tryCreateFlow
		// to avoid concurrent tryCreateFlows from stepping on each
		// other while manipulating their copy of the options.
		vcOptsCopy := make([]stream.VCOpt, len(vcOpts))
		copy(vcOptsCopy, vcOpts)
		go c.tryCreateFlow(ctx, principal, i, name, server, method, authorizer, ch, vcOptsCopy)
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
			vlog.VI(2).Infof("rpc: timeout on connection to server %v ", name)
			_, _, err := c.failedTryCall(ctx, name, method, responses, ch)
			if verror.ErrorID(err) != verror.ErrTimeout.ID {
				return nil, verror.NoRetry, verror.New(verror.ErrTimeout, ctx, err)
			}
			return nil, verror.NoRetry, err
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
			// TODO(cnicolaou): continue verror testing from here.
			fc, err := newFlowClient(ctx, r.flow, r.blessings, dc)
			if err != nil {
				return nil, verror.NoRetry, err
			}

			if err := fc.prepareBlessingsAndDischarges(method, args, r.rejectedBlessings, opts); err != nil {
				r.err = verror.New(verror.ErrNotTrusted, ctx, name, r.flow.RemoteBlessings(), err)
				vlog.VI(2).Infof("rpc: err: %s", r.err)
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
						vtrace.GetSpan(fc.ctx).Annotate("Cancelled")
						fc.flow.Cancel()
					case <-fc.flow.Closed():
					}
				}()
			}

			deadline, _ := ctx.Deadline()
			if verr := fc.start(r.suffix, method, args, deadline); verr != nil {
				return nil, verror.NoRetry, verr
			}
			return fc, verror.NoRetry, nil
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

// failedTryCall performs asynchronous cleanup for tryCall, and returns an
// appropriate error from the responses we've already received.  All parallel
// calls in tryCall failed or we timed out if we get here.
func (c *client) failedTryCall(ctx *context.T, name, method string, responses []*serverStatus, ch chan *serverStatus) (rpc.ClientCall, verror.ActionCode, error) {
	go cleanupTryCall(nil, responses, ch)
	c.ns.FlushCacheEntry(name)
	noconn, untrusted := []string{}, []string{}
	for _, r := range responses {
		if r != nil && r.err != nil {
			switch {
			case verror.ErrorID(r.err) == verror.ErrNotTrusted.ID || verror.ErrorID(r.err) == errAuthError.ID:
				untrusted = append(untrusted, "("+r.err.Error()+") ")
			default:
				noconn = append(noconn, "("+r.err.Error()+") ")
			}
		}
	}
	// TODO(cnicolaou): we get system errors for things like dialing using
	// the 'ws' protocol which can never succeed even if we retry the connection,
	// hence we return RetryRefetch below except for the case where the servers
	// are not trusted, in case there's no point in retrying at all.
	// TODO(cnicolaou): implementing at-most-once rpc semantics in the future
	// will require thinking through all of the cases where the RPC can
	// be retried by the client whilst it's actually being executed on the
	// client.
	switch {
	case len(untrusted) > 0 && len(noconn) > 0:
		return nil, verror.RetryRefetch, verror.New(verror.ErrNoServersAndAuth, ctx, append(noconn, untrusted...))
	case len(noconn) > 0:
		return nil, verror.RetryRefetch, verror.New(verror.ErrNoServers, ctx, noconn)
	case len(untrusted) > 0:
		return nil, verror.NoRetry, verror.New(verror.ErrNotTrusted, ctx, untrusted)
	default:
		return nil, verror.RetryRefetch, verror.New(verror.ErrTimeout, ctx)
	}
}

// prepareBlessingsAndDischarges prepares blessings and discharges for
// the call.
//
// This includes: (1) preparing blessings that must be granted to the
// server, (2) preparing blessings that the client authenticates with,
// and, (3) preparing any discharges for third-party caveats on the client's
// blessings.
func (fc *flowClient) prepareBlessingsAndDischarges(method string, args []interface{}, rejectedServerBlessings []security.RejectedBlessing, opts []rpc.CallOpt) error {
	// LocalPrincipal is nil which means we are operating under
	// SecurityNone.
	if fc.flow.LocalPrincipal() == nil {
		return nil
	}

	// Prepare blessings that must be granted to the server (using any
	// rpc.Granter implementation in 'opts').
	if err := fc.prepareGrantedBlessings(opts); err != nil {
		return err
	}

	// Fetch blessings from the client's blessing store that are to be
	// shared with the server.
	if fc.blessings = fc.flow.LocalPrincipal().BlessingStore().ForPeer(fc.server...); fc.blessings.IsZero() {
		// TODO(ataly, ashankar): We need not error out here and instead can just send the <nil> blessings
		// to the server.
		return verror.New(errNoBlessingsForPeer, fc.ctx, fc.server, rejectedServerBlessings)
	}

	// Fetch any discharges for third-party caveats on the client's blessings.
	if !fc.blessings.IsZero() && fc.dc != nil {
		impetus, err := mkDischargeImpetus(fc.server, method, args)
		if err != nil {
			// TODO(toddw): Fix up the internal error.
			return verror.New(verror.ErrBadProtocol, fc.ctx, fmt.Errorf("couldn't make discharge impetus: %v", err))
		}
		fc.discharges = fc.dc.PrepareDischarges(fc.ctx, fc.blessings.ThirdPartyCaveats(), impetus)
	}
	return nil
}

func (fc *flowClient) prepareGrantedBlessings(opts []rpc.CallOpt) error {
	for _, o := range opts {
		switch v := o.(type) {
		case rpc.Granter:
			if b, err := v.Grant(fc.flow.RemoteBlessings()); err != nil {
				return verror.New(errBlessingGrant, fc.ctx, fc.server, err)
			} else if fc.grantedBlessings, err = security.UnionOfBlessings(fc.grantedBlessings, b); err != nil {
				return verror.New(errBlessingAdd, fc.ctx, fc.server, err)
			}
		}
	}
	return nil
}

func (c *client) Close() {
	defer vlog.LogCall()()
	c.vcMapMu.Lock()
	for _, v := range c.vcMap {
		c.streamMgr.ShutdownEndpoint(v.remoteEP)
	}
	c.vcMap = nil
	c.vcMapMu.Unlock()
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
	var err error
	typeenc := flow.VCDataCache().Get(vc.TypeEncoderKey{})
	if typeenc == nil {
		if fc.enc, err = vom.NewEncoder(flow); err != nil {
			berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errVomEncoder, fc.ctx, err))
			return nil, fc.close(berr)
		}
		if fc.dec, err = vom.NewDecoder(flow); err != nil {
			berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errVomDecoder, fc.ctx, err))
			return nil, fc.close(berr)
		}
	} else {
		if fc.enc, err = vom.NewEncoderWithTypeEncoder(flow, typeenc.(*vom.TypeEncoder)); err != nil {
			berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errVomEncoder, fc.ctx, err))
			return nil, fc.close(berr)
		}
		typedec := flow.VCDataCache().Get(vc.TypeDecoderKey{})
		if fc.dec, err = vom.NewDecoderWithTypeDecoder(flow, typedec.(*vom.TypeDecoder)); err != nil {
			berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errVomDecoder, fc.ctx, err))
			return nil, fc.close(berr)
		}
	}
	return fc, nil
}

func (fc *flowClient) close(err error) error {
	if _, ok := err.(verror.E); err != nil && !ok {
		// TODO(cnicolaou): remove this once the second CL in this
		// series of CLs to use verror consistently is complete.
		vlog.Infof("WARNING: expected %v to be a verror", err)
	}
	subErr := verror.SubErr{Err: err, Options: verror.Print}
	subErr.Name = "remote=" + fc.flow.RemoteEndpoint().String()
	if cerr := fc.flow.Close(); cerr != nil && err == nil {
		return verror.New(verror.ErrInternal, fc.ctx, subErr)
	}
	switch {
	case verror.ErrorID(err) == verror.ErrBadProtocol.ID:
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
	case verror.ErrorID(err) == verror.ErrTimeout.ID:
		// Canceled trumps timeout.
		if fc.ctx.Err() == context.Canceled {
			// TODO(cnicolaou,m3b): reintroduce 'append' when the new verror API is done.
			return verror.New(verror.ErrCanceled, fc.ctx, err.Error())
		}
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
		Deadline:         vtime.Deadline{deadline},
		GrantedBlessings: fc.grantedBlessings,
		Blessings:        blessingsRequest,
		Discharges:       fc.discharges,
		TraceRequest:     vtrace.GetRequest(fc.ctx),
	}
	if err := fc.enc.Encode(req); err != nil {
		berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errRequestEncoding, fc.ctx, fmt.Sprintf("%#v", req), err))
		return fc.close(berr)
	}
	for ix, arg := range args {
		if err := fc.enc.Encode(arg); err != nil {
			berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errArgEncoding, fc.ctx, ix, err))
			return fc.close(berr)
		}
	}
	return nil
}

func (fc *flowClient) Send(item interface{}) error {
	defer vlog.LogCall()()
	if fc.sendClosed {
		return verror.New(verror.ErrAborted, fc.ctx)
	}

	// The empty request header indicates what follows is a streaming arg.
	if err := fc.enc.Encode(rpc.Request{}); err != nil {
		berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errRequestEncoding, fc.ctx, rpc.Request{}, err))
		return fc.close(berr)
	}
	if err := fc.enc.Encode(item); err != nil {
		berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errArgEncoding, fc.ctx, -1, err))
		return fc.close(berr)
	}
	return nil
}

func decodeNetError(ctx *context.T, err error) verror.IDAction {
	if neterr, ok := err.(net.Error); ok {
		if neterr.Timeout() || neterr.Temporary() {
			// If a read is canceled in the lower levels we see
			// a timeout error - see readLocked in vc/reader.go
			if ctx.Err() == context.Canceled {
				return verror.ErrCanceled
			}
			return verror.ErrTimeout
		}
	}
	return verror.ErrBadProtocol
}

func (fc *flowClient) Recv(itemptr interface{}) error {
	defer vlog.LogCall()()
	switch {
	case fc.response.Error != nil:
		// TODO(cnicolaou): this will become a verror.E when we convert the
		// server.
		return verror.New(verror.ErrBadProtocol, fc.ctx, fc.response.Error)
	case fc.response.EndStreamResults:
		return io.EOF
	}

	// Decode the response header and handle errors and EOF.
	if err := fc.dec.Decode(&fc.response); err != nil {
		berr := verror.New(decodeNetError(fc.ctx, err), fc.ctx, verror.New(errResponseDecoding, fc.ctx, err))
		return fc.close(berr)
	}
	if fc.response.Error != nil {
		// TODO(cnicolaou): this will become a verror.E when we convert the
		// server.
		return verror.New(verror.ErrBadProtocol, fc.ctx, fc.response.Error)
	}
	if fc.response.EndStreamResults {
		// Return EOF to indicate to the caller that there are no more stream
		// results.  Any error sent by the server is kept in fc.response.Error, and
		// returned to the user in Finish.
		return io.EOF
	}
	// Decode the streaming result.
	if err := fc.dec.Decode(itemptr); err != nil {
		berr := verror.New(decodeNetError(fc.ctx, err), fc.ctx, verror.New(errResponseDecoding, fc.ctx, err))
		// TODO(cnicolaou): should we be caching this?
		fc.response.Error = berr
		return fc.close(berr)
	}
	return nil
}

func (fc *flowClient) CloseSend() error {
	defer vlog.LogCall()()
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
	defer vlog.LogCall()()
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
			berr := verror.New(decodeNetError(fc.ctx, err), fc.ctx, verror.New(errResponseDecoding, fc.ctx, err))
			return fc.close(berr)
		}
		// The response header must indicate the streaming results have ended.
		if fc.response.Error == nil && !fc.response.EndStreamResults {
			berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errRemainingStreamResults, fc.ctx))
			return fc.close(berr)
		}
	}
	if fc.response.AckBlessings {
		clientAckBlessings(fc.flow.VCDataCache(), fc.blessings)
	}
	// Incorporate any VTrace info that was returned.
	vtrace.GetStore(fc.ctx).Merge(fc.response.TraceResponse)
	if fc.response.Error != nil {
		// TODO(cnicolaou): remove verror.ErrNoAccess with verror version
		// when rpc.Server is converted.
		if verror.ErrorID(fc.response.Error) == verror.ErrNoAccess.ID && fc.dc != nil {
			// In case the error was caused by a bad discharge, we do not want to get stuck
			// with retrying again and again with this discharge. As there is no direct way
			// to detect it, we conservatively flush all discharges we used from the cache.
			// TODO(ataly,andreser): add verror.BadDischarge and handle it explicitly?
			vlog.VI(3).Infof("Discarding %d discharges as RPC failed with %v", len(fc.discharges), fc.response.Error)
			fc.dc.Invalidate(fc.discharges...)
		}
		return fc.close(verror.Convert(verror.ErrInternal, fc.ctx, fc.response.Error))
	}
	if got, want := fc.response.NumPosResults, uint64(len(resultptrs)); got != want {
		berr := verror.New(verror.ErrBadProtocol, fc.ctx, verror.New(errMismatchedResults, fc.ctx, got, want))
		return fc.close(berr)
	}
	for ix, r := range resultptrs {
		if err := fc.dec.Decode(r); err != nil {
			berr := verror.New(decodeNetError(fc.ctx, err), fc.ctx, verror.New(errResultDecoding, fc.ctx, ix, err))
			return fc.close(berr)
		}
	}
	return fc.close(nil)
}

func (fc *flowClient) RemoteBlessings() ([]string, security.Blessings) {
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
