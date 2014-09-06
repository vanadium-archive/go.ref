package ipc

import (
	"fmt"
	"io"
	"sync"
	"time"

	"veyron/runtimes/google/ipc/version"
	inaming "veyron/runtimes/google/naming"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"
)

var (
	errNoServers              = verror.NotFoundf("ipc: no servers")
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
	// TODO(ashankar): Additionally, should vcMap be keyed with other options also?
	vcMap map[string]*vcInfo // map from endpoint.String() to vc info

	dischargeCache dischargeCache
}

type vcInfo struct {
	vc       stream.VC
	remoteEP naming.Endpoint
	// TODO(toddw): Add type and cancel flows.
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
	vc, err := c.streamMgr.Dial(ep, c.vcOpts...)
	if err != nil {
		return nil, err
	}
	// TODO(toddw): Add connections for the type and cancel flows.
	c.vcMap[ep.String()] = &vcInfo{vc: vc, remoteEP: ep}
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

func (c *client) StartCall(ctx context.T, name, method string, args []interface{}, opts ...ipc.CallOpt) (ipc.Call, error) {
	return c.startCall(ctx, name, method, args, opts...)
}

// startCall ensures StartCall always returns verror.E.
func (c *client) startCall(ctx context.T, name, method string, args []interface{}, opts ...ipc.CallOpt) (ipc.Call, verror.E) {
	if ctx == nil {
		return nil, verror.BadArgf("ipc: %s.%s called with nil context", name, method)
	}
	servers, err := c.ns.Resolve(ctx, name)
	if err != nil {
		return nil, verror.NotFoundf("ipc: Resolve(%q) failed: %v", name, err)
	}
	// Try all servers, and if none of them are authorized for the call then return the error of the last server
	// that was tried.
	var lastErr verror.E
	for _, server := range servers {
		flow, suffix, err := c.connectFlow(server)
		if err != nil {
			lastErr = verror.NotFoundf("ipc: couldn't connect to server %v: %v", server, err)
			vlog.VI(2).Infof("ipc: couldn't connect to server %v: %v", server, err)
			continue // Try the next server.
		}
		timeout := time.Duration(ipc.NoTimeout)
		if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
			timeout = deadline.Sub(time.Now())
			if err := flow.SetDeadline(deadline); err != nil {
				lastErr = verror.Internalf("ipc: flow.SetDeadline failed: %v", err)
				continue
			}
		}

		// Validate caveats on the server's identity for the context associated with this call.
		blessing, err := authorizeServer(flow.LocalID(), flow.RemoteID(), opts)
		if err != nil {
			lastErr = verror.NotAuthorizedf("ipc: client unwilling to talk to server %q: %v", flow.RemoteID(), err)
			flow.Close()
			continue
		}

		discharges := c.prepareDischarges(ctx, flow.LocalID(), flow.RemoteID(), method, args, opts)

		lastErr = nil
		fc := newFlowClient(flow, &c.dischargeCache, discharges)

		go func() {
			<-ctx.Done()
			fc.Cancel()
		}()

		if verr := fc.start(suffix, method, args, timeout, blessing); verr != nil {
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

// authorizeServer validates that server has an identity that the client is willing to converse
// with, and if so returns a blessing to be provided to the server. This blessing can be nil,
// which indicates that the client does wish to talk to the server but not provide any blessings.
func authorizeServer(client, server security.PublicID, opts []ipc.CallOpt) (security.PublicID, error) {
	if server == nil {
		return nil, fmt.Errorf("server identity cannot be nil")
	}

	// TODO(ataly): What should the label be for the context? Typically the label is the
	// security.Label of the method but we don't have that information here at the client.
	// TODO(ataly,andreser): Replace this statement with the commented code just below
	// it once we have a mechanism for servers to send discharges for any third-party caveats
	// on its PublicID.
	authID, err := server, error(nil)
	// authID, err := server.Authorize(isecurity.NewContext(isecurity.ContextArgs{
	// 	LocalID:  client,
	//	RemoteID: server,
	// }))
	if err != nil {
		return nil, err
	}
	var granter ipc.Granter
	for _, o := range opts {
		switch v := o.(type) {
		case veyron2.RemoteID:
			if !security.BlessingPattern(v).MatchedBy(authID.Names()...) {
				return nil, fmt.Errorf("server %q does not match the provided pattern %q", authID, v)
			}
		case ipc.Granter:
			// Later Granters take precedence over earlier ones.
			// Or should fail if there are multiple provided?
			granter = v
		}
	}
	var blessing security.PublicID
	if granter != nil {
		if blessing, err = granter.Grant(authID); err != nil {
			return nil, fmt.Errorf("failed to grant credentials to server %q: %v", authID, err)
		}
	}
	return blessing, nil
}

func (c *client) Close() {
	c.vcMapMu.Lock()
	for _, v := range c.vcMap {
		c.streamMgr.ShutdownEndpoint(v.remoteEP)
	}
	c.vcMap = nil
	c.vcMapMu.Unlock()
}

// IPCBindOpt makes client implement BindOpt.
func (c *client) IPCBindOpt() {}

var _ ipc.BindOpt = (*client)(nil)

// flowClient implements the RPC client-side protocol for a single RPC, over a
// flow that's already connected to the server.
type flowClient struct {
	dec      *vom.Decoder // to decode responses and results from the server
	enc      *vom.Encoder // to encode requests and args to the server
	flow     stream.Flow  // the underlying flow
	response ipc.Response // each decoded response message is kept here

	discharges     []security.Discharge // discharges used for this request
	dischargeCache *dischargeCache      // client-global discharge cache reference type

	sendClosedMu sync.Mutex
	sendClosed   bool // is the send side already closed? GUARDED_BY(sendClosedMu)
}

func newFlowClient(flow stream.Flow, dischargeCache *dischargeCache, discharges []security.Discharge) *flowClient {
	return &flowClient{
		// TODO(toddw): Support different codecs
		dec:            vom.NewDecoder(flow),
		enc:            vom.NewEncoder(flow),
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

func (fc *flowClient) start(suffix, method string, args []interface{}, timeout time.Duration, blessing security.PublicID) verror.E {
	req := ipc.Request{
		Suffix:        suffix,
		Method:        method,
		NumPosArgs:    uint64(len(args)),
		Timeout:       int64(timeout),
		HasBlessing:   blessing != nil,
		NumDischarges: uint64(len(fc.discharges)),
	}
	if err := fc.enc.Encode(req); err != nil {
		return fc.close(verror.BadProtocolf("ipc: request encoding failed: %v", err))
	}
	if blessing != nil {
		if err := fc.enc.Encode(blessing); err != nil {
			return fc.close(verror.BadProtocolf("ipc: blessing encoding failed: %v", err))
		}
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
		return fc.close(verror.BadProtocolf("ipc: end stream args encoding failed: %v", err))
	}
	fc.sendClosed = true
	return nil
}

func (fc *flowClient) Finish(resultptrs ...interface{}) error {
	return fc.finish(resultptrs...)
}

// finish ensures Finish always returns verror.E.
func (fc *flowClient) finish(resultptrs ...interface{}) verror.E {
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
	if fc.response.Error != nil {
		if verror.Is(fc.response.Error, verror.NotAuthorized) && fc.dischargeCache != nil {
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
		return fc.close(verror.BadProtocolf("ipc: server sent %d results, client expected %d", got, want))
	}
	for ix, r := range resultptrs {
		if err := fc.dec.Decode(r); err != nil {
			return fc.close(verror.BadProtocolf("ipc: result #%d decoding failed: %v", ix, err))
		}
	}
	return fc.close(nil)
}

func (fc *flowClient) Cancel() {
	fc.flow.Cancel()
}
