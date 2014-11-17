package ipc

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	"veyron.io/veyron/veyron/runtimes/google/ipc/version"
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
	"veyron.io/veyron/veyron/runtimes/google/vtrace"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
)

var (
	errNoServers              = verror.NoExistf("ipc: no servers")
	errNoAccess               = verror.NoAccessf("ipc: client unwilling to access to server")
	errFlowClosed             = verror.Abortedf("ipc: flow closed")
	errRemainingStreamResults = verror.BadProtocolf("ipc: Finish called with remaining streaming results")
	errNonRootedName          = verror.BadArgf("ipc: cannot connect to a non-rooted name")
)

var serverPatternRegexp = regexp.MustCompile("^\\[([^\\]]+)\\](.*)")

// TODO(ribrdb): Flip this to true once everything is updated.
const enableSecureServerAuth = false

type client struct {
	streamMgr          stream.Manager
	ns                 naming.Namespace
	vcOpts             []stream.VCOpt // vc opts passed to dial
	preferredProtocols []string

	// We support concurrent calls to StartCall and Close, so we must protect the
	// vcMap.  Everything else is initialized upon client construction, and safe
	// to use concurrently.
	vcMapMu sync.Mutex
	// TODO(ashankar): The key should be a function of the blessings shared with the server?
	vcMap map[string]*vcInfo // map key is endpoint.String

	dc vc.DischargeClient
}

var _ ipc.Client = (*client)(nil)
var _ ipc.BindOpt = (*client)(nil)

type vcInfo struct {
	vc       stream.VC
	remoteEP naming.Endpoint
}

func InternalNewClient(streamMgr stream.Manager, ns naming.Namespace, opts ...ipc.ClientOpt) (ipc.Client, error) {

	c := &client{
		streamMgr: streamMgr,
		ns:        ns,
		vcMap:     make(map[string]*vcInfo),
	}
	for _, opt := range opts {
		if dc, ok := opt.(vc.DischargeClient); ok {
			c.dc = dc
		}
		// Collect all client opts that are also vc opts.
		switch v := opt.(type) {
		case stream.VCOpt:
			c.vcOpts = append(c.vcOpts, v)
		case options.PreferredProtocols:
			c.preferredProtocols = v
		}
	}

	return c, nil
}

func (c *client) createFlow(ep naming.Endpoint) (stream.Flow, error) {
	c.vcMapMu.Lock()
	defer c.vcMapMu.Unlock()
	if c.vcMap == nil {
		return nil, fmt.Errorf("client has been closed")
	}
	if vcinfo := c.vcMap[ep.String()]; vcinfo != nil {
		if flow, err := vcinfo.vc.Connect(); err == nil {
			return flow, nil
		}
		// If the vc fails to establish a new flow, we assume it's
		// broken, remove it from the map, and proceed to establishing
		// a new vc.
		// TODO(caprita): Should we distinguish errors due to vc being
		// closed from other errors?  If not, should we call vc.Close()
		// before removing the vc from the map?
		delete(c.vcMap, ep.String())
	}
	sm := c.streamMgr
	c.vcMapMu.Unlock()
	vc, err := sm.Dial(ep, c.vcOpts...)
	c.vcMapMu.Lock()
	if err != nil {
		return nil, err
	}
	if c.vcMap == nil {
		sm.ShutdownEndpoint(ep)
		return nil, fmt.Errorf("client has been closed")
	}
	if othervc, exists := c.vcMap[ep.String()]; exists {
		vc = othervc.vc
		// TODO(ashankar,toddw): Figure out how to close up the VC that
		// is discarded. vc.Close?
	} else {
		c.vcMap[ep.String()] = &vcInfo{vc: vc, remoteEP: ep}
	}
	return vc.Connect()
}

// connectFlow parses an endpoint and a suffix out of the server and establishes
// a flow to the endpoint, returning the parsed suffix.
// The server name passed in should be a rooted name, of the form "/ep/suffix" or
// "/ep//suffix", or just "/ep".
func (c *client) connectFlow(server string) (stream.Flow, string, error) {
	address, suffix := naming.SplitAddressName(server)
	if len(address) == 0 {
		return nil, "", errNonRootedName
	}
	ep, err := inaming.NewEndpoint(address)
	if err != nil {
		return nil, "", err
	}
	if err = version.CheckCompatibility(ep); err != nil {
		return nil, "", err
	}
	flow, err := c.createFlow(ep)
	if err != nil {
		return nil, "", err
	}
	return flow, suffix, nil
}

// A randomized exponential backoff.  The randomness deters error convoys from forming.
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

// TODO(p): replace these checks with m3b's retry bit when it exists.  This is currently a colossal hack.
func retriable(err error) bool {
	e := err.Error()
	// Authentication errors are permanent.
	if strings.Contains(e, "authorized") {
		return false
	}
	// Resolution errors are retriable.
	if strings.Contains(e, "ipc: Resolve") {
		return true
	}
	// Kernel level errors are retriable.
	if strings.Contains(e, "errno") {
		return true
	}
	// Connection refused is retriable.
	if strings.Contains(e, "connection refused") {
		return true
	}

	return false
}

func getRetryTimeoutOpt(opts []ipc.CallOpt) (time.Duration, bool) {
	for _, o := range opts {
		if r, ok := o.(options.RetryTimeout); ok {
			return time.Duration(r), true
		}
	}
	return 0, false
}

func (c *client) StartCall(ctx context.T, name, method string, args []interface{}, opts ...ipc.CallOpt) (ipc.Call, error) {
	defer vlog.LogCall()()
	return c.startCall(ctx, name, method, args, opts)
}

func getNoResolveOpt(opts []ipc.CallOpt) bool {
	for _, o := range opts {
		if r, ok := o.(options.NoResolve); ok {
			return bool(r)
		}
	}
	return false
}

func mkDischargeImpetus(serverBlessings []string, method string, args []interface{}) security.DischargeImpetus {
	var impetus security.DischargeImpetus
	if len(serverBlessings) > 0 {
		impetus.Server = make([]security.BlessingPattern, len(serverBlessings))
		for i, b := range serverBlessings {
			impetus.Server[i] = security.BlessingPattern(b)
		}
	}
	impetus.Method = method
	if len(args) > 0 {
		impetus.Arguments = make([]vdlutil.Any, len(args))
		for i, a := range args {
			impetus.Arguments[i] = vdlutil.Any(a)
		}
	}
	return impetus
}

// startCall ensures StartCall always returns verror.E.
func (c *client) startCall(ctx context.T, name, method string, args []interface{}, opts []ipc.CallOpt) (ipc.Call, verror.E) {
	if ctx == nil {
		return nil, verror.BadArgf("ipc: %s.%s called with nil context", name, method)
	}

	// Context specified deadline.
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		// Default deadline.
		deadline = time.Now().Add(defaultCallTimeout)
	}
	if r, ok := getRetryTimeoutOpt(opts); ok {
		// Caller specified deadline.
		deadline = time.Now().Add(time.Duration(r))
	}
	var lastErr verror.E
	for retries := 0; ; retries++ {
		if retries != 0 {
			if !backoff(retries, deadline) {
				break
			}
		}
		call, err := c.tryCall(ctx, name, method, args, opts)
		if err == nil {
			return call, nil
		}
		lastErr = err
		if time.Now().After(deadline) || !retriable(err) {
			break
		}
	}
	return nil, lastErr
}

type serverStatus struct {
	index     int
	suffix    string
	flow      stream.Flow
	processed bool
	err       verror.E
}

func (c *client) tryServer(index int, server string, ch chan<- *serverStatus, done <-chan struct{}) {
	select {
	case <-done:
		return
	default:
	}
	status := &serverStatus{index: index}
	flow, suffix, err := c.connectFlow(server)
	if err != nil {
		vlog.VI(2).Infof("ipc: couldn't connect to server %v: %v", server, err)
		status.err = verror.NoExistf("ipc: %q: %s", server, err)
		ch <- status
		return
	}
	status.suffix = suffix
	status.flow = flow
	select {
	case <-done:
		flow.Close()
	default:
		ch <- status
	}
}

// tryCall makes a single attempt at a call, against possibly multiple servers.
func (c *client) tryCall(ctx context.T, name, method string, args []interface{}, opts []ipc.CallOpt) (ipc.Call, verror.E) {
	ctx, _ = vtrace.WithNewSpan(ctx, fmt.Sprintf("<client>\"%s\".%s", name, method))
	_, serverPattern, name := splitObjectName(name)
	// Resolve name unless told not to.
	var servers []string
	if getNoResolveOpt(opts) {
		servers = []string{name}
	} else {
		if resolved, err := c.ns.Resolve(ctx, name); err != nil {
			return nil, verror.NoExistf("ipc: Resolve(%q) failed: %v", name, err)
		} else {
			// An empty set of protocols means all protocols...
			ordered, err := filterAndOrderServers(resolved, c.preferredProtocols)
			if len(ordered) == 0 {
				return nil, verror.NoExistf("ipc: %q: %s", name, err)
			}
			servers = ordered
		}
	}
	// servers is now orderd by the priority heurestic implemented in
	// filterAndOrderServers.
	attempts := len(servers)
	if attempts == 0 {
		return nil, errNoServers
	}

	// Try to connect to all servers in parallel.
	responses := make([]*serverStatus, attempts)

	// Provide sufficient buffering for all of the connections to finish
	// instantaneously. This is important because we want to process
	// the responses in priority order; that order is indicated by the
	// order of entries in servers. So, if two respones come in at the
	// same 'instant', we prefer the first in the slice.
	ch := make(chan *serverStatus, attempts)

	// Read as many responses as we can before we would block.
	gatherResponses := func() {
		for {
			select {
			default:
				return
			case s := <-ch:
				responses[s.index] = s
			}
		}
	}

	delay := time.Duration(ipc.NoTimeout)
	if dl, set := ctx.Deadline(); set {
		delay = dl.Sub(time.Now())
	}
	timeoutChan := time.After(delay)

	// We'll close this channel when an RPC has been started and we've
	// irrevocably selected a server.
	done := make(chan struct{})
	// Try all of the servers in parallel.
	for i, server := range servers {
		go c.tryServer(i, server, ch, done)
	}

	select {
	case <-timeoutChan:
		// All calls failed if we get here.
		close(done)
		c.ns.FlushCacheEntry(name)
		return nil, verror.NoExistf("ipc: couldn't connect to server %v", name)
	case s := <-ch:
		responses[s.index] = s
		gatherResponses()
	}

	accessErrs := []error{}
	connErrs := []error{}
	for {

		for _, r := range responses {
			if r == nil || r.err != nil {
				if r != nil && r.err != nil && !r.processed {
					connErrs = append(connErrs, r.err)
					r.processed = true
				}
				continue
			}

			flow := r.flow
			suffix := r.suffix
			flow.SetDeadline(ctx.Done())

			var (
				serverB  []string
				grantedB security.Blessings
			)

			// LocalPrincipal is nil means that the client wanted to avoid
			// authentication, and thus wanted to skip authorization as well.
			if flow.LocalPrincipal() != nil {
				// Validate caveats on the server's identity for the context associated with this call.
				var err error
				if serverB, grantedB, err = c.authorizeServer(flow, name, method, serverPattern, opts); err != nil {
					vlog.VI(2).Infof("ipc: client unwilling to invoke %q.%q on server %v: %v", name, method, flow.RemoteBlessings(), err)
					if !r.processed {
						accessErrs = append(accessErrs, err)
						r.err = verror.NoAccessf("ipc: unwilling to invoke %q.%q on server %v: %v", name, method, flow.RemoteBlessings(), err)
						r.processed = true
					}
					flow.Close()
					continue
				}
			}

			// This is the 'point of no return', so we tell the tryServer
			// goroutines to not bother sending us any more flows.
			// Once the RPC is started (fc.start below) we can't be sure
			// if it makes it to the server or not so, this code will
			// never call fc.start more than once to ensure that we
			// provide 'at-most-once' rpc semantics at this level. Retrying
			// the network connections (i.e. creating flows) is fine since
			// we can cleanup that state if we abort a call (i.e. close the
			// flow).
			close(done)

			fc := newFlowClient(ctx, serverB, flow, c.dc)

			if doneChan := ctx.Done(); doneChan != nil {
				go func() {
					select {
					case <-ctx.Done():
						fc.Cancel()
					case <-fc.flow.Closed():
					}
				}()
			}

			timeout := time.Duration(ipc.NoTimeout)
			if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
				timeout = deadline.Sub(time.Now())
			}
			if verr := fc.start(suffix, method, args, timeout, grantedB); verr != nil {
				return nil, verr
			}
			return fc, nil
		}

		// Quit if we've seen an error from all parallel connection attempts
		handled := 0
		for _, r := range responses {
			if r != nil && r.err != nil {
				handled++
			}
		}
		if handled == len(responses) {
			break
		}

		select {
		case <-timeoutChan:
			// All remaining calls failed if we get here.
			vlog.VI(2).Infof("ipc: couldn't connect to server %v", name)
			goto quit
		case s := <-ch:
			responses[s.index] = s
			gatherResponses()
		}
	}
quit:
	close(done)
	c.ns.FlushCacheEntry(name)
	// TODO(cnicolaou): introduce a third error code here for mixed
	// conn/access errors.
	return nil, verror.NoExistf("ipc: client failed to invoke  %q.%q: on %v", name, method, servers, append(connErrs, accessErrs...))
}

// authorizeServer validates that the server (remote end of flow) has the credentials to serve
// the RPC name.method for the client (local end of the flow). It returns the blessings at the
// server that are authorized for this purpose and any blessings that are to be granted to
// the server (via ipc.Granter implementations in opts.)
func (c *client) authorizeServer(flow stream.Flow, name, method string, serverPattern security.BlessingPattern, opts []ipc.CallOpt) (serverBlessings []string, grantedBlessings security.Blessings, err error) {
	if flow.RemoteBlessings() == nil {
		return nil, nil, fmt.Errorf("server has not presented any blessings")
	}
	ctxt := security.NewContext(&security.ContextParams{
		LocalPrincipal:   flow.LocalPrincipal(),
		LocalBlessings:   flow.LocalBlessings(),
		RemoteBlessings:  flow.RemoteBlessings(),
		LocalEndpoint:    flow.LocalEndpoint(),
		RemoteEndpoint:   flow.RemoteEndpoint(),
		RemoteDischarges: flow.RemoteDischarges(),
		Method:           method,
		Name:             name})
	serverBlessings = flow.RemoteBlessings().ForContext(ctxt)
	if serverPattern != "" {
		if !serverPattern.MatchedBy(serverBlessings...) {
			return nil, nil, fmt.Errorf("server %v does not match the provided pattern %q", serverBlessings, serverPattern)
		}
	} else if enableSecureServerAuth {
		if err := (defaultAuthorizer{}).Authorize(ctxt); err != nil {
			return nil, nil, fmt.Errorf("default authorization precludes talking to server %v", serverBlessings)
		}
	}
	for _, o := range opts {
		switch v := o.(type) {
		case ipc.Granter:
			if b, err := v.Grant(flow.RemoteBlessings()); err != nil {
				return nil, nil, fmt.Errorf("failed to grant blessing to server %v: %v", serverBlessings, err)
			} else if grantedBlessings, err = security.UnionOfBlessings(grantedBlessings, b); err != nil {
				return nil, nil, fmt.Errorf("failed to add blessing granted to server %v: %v", serverBlessings, err)
			}
		}
	}
	return serverBlessings, grantedBlessings, nil
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

// IPCBindOpt makes client implement BindOpt.
func (c *client) IPCBindOpt() {
	//nologcall
}

// flowClient implements the RPC client-side protocol for a single RPC, over a
// flow that's already connected to the server.
type flowClient struct {
	ctx      context.T    // context to annotate with call details
	dec      *vom.Decoder // to decode responses and results from the server
	enc      *vom.Encoder // to encode requests and args to the server
	server   []string     // Blessings bound to the server that authorize it to receive the IPC request from the client.
	flow     stream.Flow  // the underlying flow
	response ipc.Response // each decoded response message is kept here

	discharges []security.Discharge // discharges used for this request
	dc         vc.DischargeClient   // client-global discharge-client

	sendClosedMu sync.Mutex
	sendClosed   bool // is the send side already closed? GUARDED_BY(sendClosedMu)

	finished bool // has Finish() already been called?
}

var _ ipc.Call = (*flowClient)(nil)
var _ ipc.Stream = (*flowClient)(nil)

func newFlowClient(ctx context.T, server []string, flow stream.Flow, dc vc.DischargeClient) *flowClient {
	return &flowClient{
		ctx:    ctx,
		dec:    vom.NewDecoder(flow),
		enc:    vom.NewEncoder(flow),
		server: server,
		flow:   flow,
		dc:     dc,
	}
}

func (fc *flowClient) close(verr verror.E) verror.E {
	if err := fc.flow.Close(); err != nil && verr == nil {
		verr = verror.Internalf("ipc: flow close failed: %v", err)
	}
	return verr
}

func (fc *flowClient) start(suffix, method string, args []interface{}, timeout time.Duration, blessings security.Blessings) verror.E {
	// Fetch any discharges for third-party caveats on the client's blessings
	// if this client owns a discharge-client.
	if self := fc.flow.LocalBlessings(); self != nil && fc.dc != nil {
		fc.discharges = fc.dc.PrepareDischarges(self.ThirdPartyCaveats(), mkDischargeImpetus(fc.server, method, args))
	}
	req := ipc.Request{
		Suffix:           suffix,
		Method:           method,
		NumPosArgs:       uint64(len(args)),
		Timeout:          int64(timeout),
		GrantedBlessings: security.MarshalBlessings(blessings),
		NumDischarges:    uint64(len(fc.discharges)),
		TraceRequest:     vtrace.Request(fc.ctx),
	}
	if err := fc.enc.Encode(req); err != nil {
		return fc.close(verror.BadProtocolf("ipc: request encoding failed: %v", err))
	}
	for _, d := range fc.discharges {
		if err := fc.enc.Encode(d); err != nil {
			return fc.close(verror.BadProtocolf("ipc: failed to encode discharge for %x: %v", d.ID(), err))
		}
	}
	for ix, arg := range args {
		if err := fc.enc.Encode(arg); err != nil {
			return fc.close(verror.BadProtocolf("ipc: arg %d encoding failed: %v", ix, err))
		}
	}
	return nil
}

func (fc *flowClient) Send(item interface{}) error {
	defer vlog.LogCall()()
	if fc.sendClosed {
		return errFlowClosed
	}

	// The empty request header indicates what follows is a streaming arg.
	if err := fc.enc.Encode(ipc.Request{}); err != nil {
		return fc.close(verror.BadProtocolf("ipc: streaming request header encoding failed: %v", err))
	}
	if err := fc.enc.Encode(item); err != nil {
		return fc.close(verror.BadProtocolf("ipc: streaming arg encoding failed: %v", err))
	}
	return nil
}

func (fc *flowClient) Recv(itemptr interface{}) error {
	defer vlog.LogCall()()
	switch {
	case fc.response.Error != nil:
		return fc.response.Error
	case fc.response.EndStreamResults:
		return io.EOF
	}

	// Decode the response header and handle errors and EOF.
	if err := fc.dec.Decode(&fc.response); err != nil {
		return fc.close(verror.BadProtocolf("ipc: response header decoding failed: %v", err))
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
		return fc.close(verror.BadProtocolf("ipc: streaming result decoding failed: %v", err))
	}
	return nil
}

func (fc *flowClient) CloseSend() error {
	defer vlog.LogCall()()
	return fc.closeSend()
}

// closeSend ensures CloseSend always returns verror.E.
func (fc *flowClient) closeSend() verror.E {
	fc.sendClosedMu.Lock()
	defer fc.sendClosedMu.Unlock()
	if fc.sendClosed {
		return nil
	}
	if err := fc.enc.Encode(ipc.Request{EndStreamArgs: true}); err != nil {
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
		//   return fc.close(verror.BadProtocolf("ipc: end stream args encoding failed: %v", err))
	}
	fc.sendClosed = true
	return nil
}

func (fc *flowClient) Finish(resultptrs ...interface{}) error {
	defer vlog.LogCall()()
	err := fc.finish(resultptrs...)
	vtrace.FromContext(fc.ctx).Finish()
	return err
}

// finish ensures Finish always returns verror.E.
func (fc *flowClient) finish(resultptrs ...interface{}) verror.E {
	if fc.finished {
		return fc.close(verror.BadProtocolf("ipc: multiple calls to Finish not allowed"))
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
			return fc.close(verror.BadProtocolf("ipc: response header decoding failed: %v", err))
		}
		// The response header must indicate the streaming results have ended.
		if fc.response.Error == nil && !fc.response.EndStreamResults {
			return fc.close(errRemainingStreamResults)
		}
	}

	// Incorporate any VTrace info that was returned.
	vtrace.MergeResponse(fc.ctx, &fc.response.TraceResponse)

	if fc.response.Error != nil {
		if verror.Is(fc.response.Error, verror.NoAccess) && fc.dc != nil {
			// In case the error was caused by a bad discharge, we do not want to get stuck
			// with retrying again and again with this discharge. As there is no direct way
			// to detect it, we conservatively flush all discharges we used from the cache.
			// TODO(ataly,andreser): add verror.BadDischarge and handle it explicitly?
			vlog.VI(3).Infof("Discarging %d discharges as RPC failed with %v", len(fc.discharges), fc.response.Error)
			fc.dc.Invalidate(fc.discharges...)
		}
		return fc.close(verror.ConvertWithDefault(verror.Internal, fc.response.Error))
	}
	if got, want := fc.response.NumPosResults, uint64(len(resultptrs)); got != want {
		return fc.close(verror.BadProtocolf("ipc: server sent %d results, client expected %d (%#v)", got, want, resultptrs))
	}
	for ix, r := range resultptrs {
		if err := fc.dec.Decode(r); err != nil {
			return fc.close(verror.BadProtocolf("ipc: result #%d decoding failed: %v", ix, err))
		}
	}
	return fc.close(nil)
}

func (fc *flowClient) Cancel() {
	defer vlog.LogCall()()
	vtrace.FromContext(fc.ctx).Annotate("Cancelled")
	fc.flow.Cancel()
}

func (fc *flowClient) RemoteBlessings() ([]string, security.Blessings) {
	return fc.server, fc.flow.RemoteBlessings()
}

func splitObjectName(name string) (mtPattern, serverPattern security.BlessingPattern, objectName string) {
	objectName = name
	match := serverPatternRegexp.FindSubmatch([]byte(name))
	if match != nil {
		objectName = string(match[2])
		if naming.Rooted(objectName) {
			mtPattern = security.BlessingPattern(match[1])
		} else {
			serverPattern = security.BlessingPattern(match[1])
			return
		}
	}
	if !naming.Rooted(objectName) {
		return
	}

	address, relative := naming.SplitAddressName(objectName)
	match = serverPatternRegexp.FindSubmatch([]byte(relative))
	if match != nil {
		serverPattern = security.BlessingPattern(match[1])
		objectName = naming.JoinAddressName(address, string(match[2]))
	}
	return
}
