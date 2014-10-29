package ipc

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron/runtimes/google/ipc/version"
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
	isecurity "veyron.io/veyron/veyron/runtimes/google/security"
	"veyron.io/veyron/veyron/runtimes/google/vtrace"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
)

var (
	errNoServers              = verror.NoExistf("ipc: no servers")
	errFlowClosed             = verror.Abortedf("ipc: flow closed")
	errRemainingStreamResults = verror.BadProtocolf("ipc: Finish called with remaining streaming results")
	errNonRootedName          = verror.BadArgf("ipc: cannot connect to a non-rooted name")
)

type client struct {
	streamMgr stream.Manager
	ns        naming.Namespace
	vcOpts    []stream.VCOpt // vc opts passed to dial

	// We support concurrent calls to StartCall and Close, so we must protect the
	// vcMap.  Everything else is initialized upon client construction, and safe
	// to use concurrently.
	vcMapMu sync.Mutex
	// TODO(ashankar): The key should be a function of the blessings shared with the server?
	vcMap map[string]*vcInfo // map key is endpoint.String

	dischargeCache dischargeCache
}

var _ ipc.Client = (*client)(nil)
var _ ipc.BindOpt = (*client)(nil)

type vcInfo struct {
	vc       stream.VC
	remoteEP naming.Endpoint
}

func InternalNewClient(streamMgr stream.Manager, ns naming.Namespace, opts ...ipc.ClientOpt) (ipc.Client, error) {
	c := &client{
		streamMgr:      streamMgr,
		ns:             ns,
		vcMap:          make(map[string]*vcInfo),
		dischargeCache: dischargeCache{cache: make(map[string]security.Discharge)},
	}
	for _, opt := range opts {
		// Collect all client opts that are also vc opts.
		if vcOpt, ok := opt.(stream.VCOpt); ok {
			c.vcOpts = append(c.vcOpts, vcOpt)
		}
	}
	return c, nil
}

func (c *client) createFlow(ep naming.Endpoint) (stream.Flow, error) {
	c.vcMapMu.Lock()
	defer c.vcMapMu.Unlock()
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
	c.vcMapMu.Unlock()
	vc, err := c.streamMgr.Dial(ep, c.vcOpts...)
	c.vcMapMu.Lock()
	if err != nil {
		return nil, err
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
		call, err := c.startCall(ctx, name, method, args, opts...)
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

func getNoResolveOpt(opts []ipc.CallOpt) bool {
	for _, o := range opts {
		if r, ok := o.(options.NoResolve); ok {
			return bool(r)
		}
	}
	return false
}

// startCall ensures StartCall always returns verror.E.
func (c *client) startCall(ctx context.T, name, method string, args []interface{}, opts ...ipc.CallOpt) (ipc.Call, verror.E) {
	if ctx == nil {
		return nil, verror.BadArgf("ipc: %s.%s called with nil context", name, method)
	}
	ctx, _ = vtrace.WithNewSpan(ctx, fmt.Sprintf("Client Call: %s.%s", name, method))
	// Resolve name unless told not to.
	var servers []string
	if getNoResolveOpt(opts) {
		servers = []string{name}
	} else {
		var err error
		if servers, err = c.ns.Resolve(ctx, name); err != nil {
			return nil, verror.NoExistf("ipc: Resolve(%q) failed: %v", name, err)
		}
	}
	// Try all servers, and if none of them are authorized for the call then return the error of the last server
	// that was tried.
	var lastErr verror.E
	for _, server := range servers {
		flow, suffix, err := c.connectFlow(server)
		if err != nil {
			lastErr = verror.NoExistf("ipc: couldn't connect to server %v: %v", server, err)
			vlog.VI(2).Infof("ipc: couldn't connect to server %v: %v", server, err)
			continue // Try the next server.
		}
		flow.SetDeadline(ctx.Done())

		var serverB []string
		var grantedB security.Blessings
		var discharges []security.Discharge

		// LocalPrincipal is nil means that the client wanted to avoid authentication,
		// and thus wanted to skip authorization as well.
		// TODO(suharshs,ataly,ashankar): Remove flow.LocalID() after the old security model is dead.
		if flow.LocalPrincipal() != nil || flow.LocalID() != nil {
			// Validate caveats on the server's identity for the context associated with this call.
			if serverB, grantedB, err = c.authorizeServer(flow, name, suffix, method, opts); err != nil {
				lastErr = verror.NoAccessf("ipc: client unwilling to invoke %q.%q on server %v: %v", name, method, flow.RemoteBlessings(), err)
				flow.Close()
				continue
			}
			// Fetch any discharges for third-party caveats on the client's blessings.
			if self := flow.LocalBlessings(); self != nil {
				if tpcavs := self.ThirdPartyCaveats(); len(tpcavs) > 0 {
					discharges = c.prepareDischarges(ctx, tpcavs, mkDischargeImpetus(serverB, method, args), opts)
				}
			}
		}

		lastErr = nil
		fc := newFlowClient(ctx, serverB, flow, &c.dischargeCache, discharges)

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
	if lastErr != nil {
		// If there was any problem starting the call, flush the cache entry under the
		// assumption that it was caused by stale data.
		c.ns.FlushCacheEntry(name)
		return nil, lastErr
	}
	return nil, errNoServers
}

// authorizeServer validates that the server (remote end of flow) has the credentials to serve
// the RPC name.method for the client (local end of the flow). It returns the blessings at the
// server that are authorized for this purpose and any blessings that are to be granted to
// the server (via ipc.Granter implementations in opts.)
func (c *client) authorizeServer(flow stream.Flow, name, suffix, method string, opts []ipc.CallOpt) (serverBlessings []string, grantedBlessings security.Blessings, err error) {
	if flow.RemoteID() == nil && flow.RemoteBlessings() == nil {
		return nil, nil, fmt.Errorf("server has not presented any blessings")
	}
	authctx := isecurity.NewContext(isecurity.ContextArgs{
		LocalID:         flow.LocalID(),
		RemoteID:        flow.RemoteID(),
		Debug:           "ClientAuthorizingServer",
		LocalPrincipal:  flow.LocalPrincipal(),
		LocalBlessings:  flow.LocalBlessings(),
		RemoteBlessings: flow.RemoteBlessings(),
		/* TODO(ashankar,ataly): Uncomment this! This is disabled till the hack to skip third-party caveat
		validation on a server's blessings are disabled. Commenting out the next three lines affects more
		than third-party caveats, so yeah, have to remove this soon!
		Method:          method,
		Name:            name,
		Suffix:          suffix, */
		LocalEndpoint:  flow.LocalEndpoint(),
		RemoteEndpoint: flow.RemoteEndpoint(),
	})
	if serverID := flow.RemoteID(); flow.RemoteBlessings() == nil && serverID != nil {
		serverID, err = serverID.Authorize(authctx)
		if err != nil {
			return nil, nil, err
		}
		serverBlessings = serverID.Names()
	}
	if server := flow.RemoteBlessings(); server != nil {
		serverBlessings = server.ForContext(authctx)
	}
	for _, o := range opts {
		switch v := o.(type) {
		case options.RemoteID:
			if !security.BlessingPattern(v).MatchedBy(serverBlessings...) {
				return nil, nil, fmt.Errorf("server %v does not match the provided pattern %q", serverBlessings, v)
			}
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

	discharges     []security.Discharge // discharges used for this request
	dischargeCache *dischargeCache      // client-global discharge cache reference type

	sendClosedMu sync.Mutex
	sendClosed   bool // is the send side already closed? GUARDED_BY(sendClosedMu)

	finished bool // has Finish() already been called?
}

var _ ipc.Call = (*flowClient)(nil)
var _ ipc.Stream = (*flowClient)(nil)

func newFlowClient(ctx context.T, server []string, flow stream.Flow, dischargeCache *dischargeCache, discharges []security.Discharge) *flowClient {
	return &flowClient{
		ctx:            ctx,
		dec:            vom.NewDecoder(flow),
		enc:            vom.NewEncoder(flow),
		server:         server,
		flow:           flow,
		discharges:     discharges,
		dischargeCache: dischargeCache,
	}
}

func (fc *flowClient) close(verr verror.E) verror.E {
	if err := fc.flow.Close(); err != nil && verr == nil {
		verr = verror.Internalf("ipc: flow close failed: %v", err)
	}
	return verr
}

func (fc *flowClient) start(suffix, method string, args []interface{}, timeout time.Duration, blessings security.Blessings) verror.E {
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
		if verror.Is(fc.response.Error, verror.NoAccess) && fc.dischargeCache != nil {
			// In case the error was caused by a bad discharge, we do not want to get stuck
			// with retrying again and again with this discharge. As there is no direct way
			// to detect it, we conservatively flush all discharges we used from the cache.
			// TODO(ataly,andreser): add verror.BadDischarge and handle it explicitly?
			vlog.VI(3).Infof("Discarging %d discharges as RPC failed with %v", len(fc.discharges), fc.response.Error)
			fc.dischargeCache.Invalidate(fc.discharges...)
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
