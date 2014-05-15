package ipc

import (
	"io"
	"sync"
	"time"

	"veyron/runtimes/google/ipc/version"
	inaming "veyron/runtimes/google/naming"
	isecurity "veyron/runtimes/google/security"

	"veyron2"
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
	streamMgr   stream.Manager
	mt          naming.MountTable
	vcOpts      []stream.VCOpt // vc opts passed to dial
	callTimeout time.Duration  // call timeout

	// We support concurrent calls to StartCall and Close, so we must protect the
	// vcMap.  Everything else is initialized upon client construction, and safe
	// to use concurrently.
	vcMapMu sync.Mutex
	// TODO(ashankar): Additionally, should vcMap be keyed with other options also?
	vcMap map[string]*vcInfo // map from endpoint.String() to vc info
}

type vcInfo struct {
	vc       stream.VC
	remoteEP naming.Endpoint
	// TODO(toddw): Add type and cancel flows.
}

func InternalNewClient(streamMgr stream.Manager, mt naming.MountTable, opts ...ipc.ClientOpt) (ipc.Client, error) {
	c := &client{
		streamMgr:   streamMgr,
		mt:          mt,
		vcMap:       make(map[string]*vcInfo),
		callTimeout: defaultCallTimeout,
	}
	for _, opt := range opts {
		// Collect all client opts that are also vc opts.
		if vcOpt, ok := opt.(stream.VCOpt); ok {
			c.vcOpts = append(c.vcOpts, vcOpt)
		}
		// Now handle individual opts.
		switch topt := opt.(type) {
		case veyron2.CallTimeout:
			c.callTimeout = time.Duration(topt)
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

func (c *client) StartCall(name, method string, args []interface{}, opts ...ipc.ClientCallOpt) (ipc.ClientCall, error) {
	return c.startCall(name, method, args, opts...)
}

// startCall ensures StartCall always returns verror.E.
func (c *client) startCall(name, method string, args []interface{}, opts ...ipc.ClientCallOpt) (ipc.ClientCall, verror.E) {
	servers, err := c.mt.Resolve(name)
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
		timeout := c.getCallTimeout(opts)
		if err := flow.SetDeadline(time.Now().Add(timeout)); err != nil {
			lastErr = verror.Internalf("ipc: flow.SetDeadline failed: %v", err)
			continue
		}

		// Validate caveats on the server's identity for the context associated with this call.
		remoteID := flow.RemoteID()
		if remoteID == nil {
			lastErr = verror.NotAuthorizedf("ipc: server identity cannot be nil")
			continue
		}
		// TODO(ataly): Fetch third-party discharges from the server.
		// TODO(ataly): What should the label be for the context? Typically the label is the security.Label
		// of the method but we don't have that information here at the client.
		authorizedRemoteID, err := remoteID.Authorize(isecurity.NewContext(
			isecurity.ContextArgs{
				LocalID:  flow.LocalID(),
				RemoteID: remoteID,
			}))
		if err != nil {
			lastErr = verror.NotAuthorizedf("ipc: server identity %q has one or more invalid caveats: %v", remoteID, err)
			continue
		}

		if lastErr = matchServerID(authorizedRemoteID, opts); lastErr != nil {
			continue
		}

		// remoteID is authorized for the context associated with this call.
		lastErr = nil
		fc := newFlowClient(flow)
		if verr := fc.start(suffix, method, args, timeout); verr != nil {
			return nil, verr
		}
		return fc, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errNoServers
}

func (c *client) getCallTimeout(opts []ipc.ClientCallOpt) time.Duration {
	timeout := c.callTimeout
	for _, opt := range opts {
		if ct, ok := opt.(veyron2.CallTimeout); ok {
			timeout = time.Duration(ct)
		}
	}
	return timeout
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
	dec        *vom.Decoder // to decode responses and results from the server
	enc        *vom.Encoder // to encode requests and args to the server
	flow       stream.Flow  // the underlying flow
	response   ipc.Response // each decoded response message is kept here
	sendClosed bool         // is the send side already closed?
}

func newFlowClient(flow stream.Flow) *flowClient {
	return &flowClient{
		// TODO(toddw): Support different codecs
		dec:  vom.NewDecoder(flow),
		enc:  vom.NewEncoder(flow),
		flow: flow,
	}
}

func (fc *flowClient) close(verr verror.E) verror.E {
	if err := fc.flow.Close(); err != nil && verr == nil {
		verr = verror.Internalf("ipc: flow close failed: %v", err)
	}
	return verr
}

func (fc *flowClient) start(suffix, method string, args []interface{}, timeout time.Duration) verror.E {
	req := ipc.Request{
		Suffix:     suffix,
		Method:     method,
		NumPosArgs: uint64(len(args)),
		Timeout:    int64(timeout),
	}
	if err := fc.enc.Encode(req); err != nil {
		return fc.close(verror.BadProtocolf("ipc: request encoding failed: %v", err))
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
		return fc.close(verror.BadProtocolf("ipc: streaming request encoding failed: %v", err))
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
		return fc.close(verror.BadProtocolf("ipc: response decoding failed: %v", err))
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
	if fc.sendClosed {
		return errFlowClosed
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
	// Decode the response header, if it hasn't already been decoded by Recv.
	if fc.response.Error == nil && !fc.response.EndStreamResults {
		if err := fc.dec.Decode(&fc.response); err != nil {
			return fc.close(verror.BadProtocolf("ipc: response decoding failed: %v", err))
		}
		// The response header must indicate the streaming results have ended.
		if fc.response.Error == nil && !fc.response.EndStreamResults {
			return fc.close(errRemainingStreamResults)
		}
	}
	if fc.response.Error != nil {
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

func matchServerID(id security.PublicID, opts []ipc.ClientCallOpt) verror.E {
	for _, opt := range opts {
		if pattern, ok := opt.(veyron2.RemoteID); ok && !id.Match(security.PrincipalPattern(pattern)) {
			return verror.NotAuthorizedf("ipc: server identity %q does not have a name matching the provided pattern %q", id, pattern)
		}
	}
	return nil
}
