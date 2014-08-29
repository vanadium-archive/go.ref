package ipc

import (
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"veyron/lib/netstate"

	"veyron/runtimes/google/lib/publisher"
	inaming "veyron/runtimes/google/naming"
	isecurity "veyron/runtimes/google/security"
	vsecurity "veyron/security"

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
	errServerStopped = verror.Abortedf("ipc: server is stopped")
)

func errNotAuthorized(err error) verror.E {
	return verror.NotAuthorizedf("ipc: not authorized(%v)", err)
}

type server struct {
	sync.Mutex
	ctx              context.T                // context used by the server to make internal RPCs.
	streamMgr        stream.Manager           // stream manager to listen for new flows.
	publisher        publisher.Publisher      // publisher to publish mounttable mounts.
	listenerOpts     []stream.ListenerOpt     // listener opts passed to Listen.
	listeners        map[stream.Listener]bool // listeners created by Listen.
	disp             ipc.Dispatcher           // dispatcher to serve RPCs
	active           sync.WaitGroup           // active goroutines we've spawned.
	stopped          bool                     // whether the server has been stopped.
	stoppedChan      chan struct{}            // closed when the server has been stopped.
	ns               naming.Namespace
	preferredAddress func(network string, addrs []net.Addr) (net.Addr, error)
	servesMountTable bool
	stats            *ipcStats // stats for this server.
}

func InternalNewServer(ctx context.T, streamMgr stream.Manager, ns naming.Namespace, opts ...ipc.ServerOpt) (ipc.Server, error) {
	s := &server{
		ctx:              ctx,
		streamMgr:        streamMgr,
		publisher:        publisher.New(ctx, ns, publishPeriod),
		listeners:        make(map[stream.Listener]bool),
		stoppedChan:      make(chan struct{}),
		preferredAddress: preferredIPAddress,
		ns:               ns,
		stats:            newIPCStats(naming.Join("ipc", "server", streamMgr.RoutingID().String())),
	}
	for _, opt := range opts {
		switch opt := opt.(type) {
		case veyron2.PreferredAddressOpt:
			s.preferredAddress = opt
		case stream.ListenerOpt:
			// Collect all ServerOpts that are also ListenerOpts.
			s.listenerOpts = append(s.listenerOpts, opt)
		case veyron2.ServesMountTableOpt:
			s.servesMountTable = bool(opt)
		}
	}
	return s, nil
}

func (s *server) Published() ([]string, error) {
	s.Lock()
	defer s.Unlock()
	if s.stopped {
		return nil, errServerStopped
	}
	return s.publisher.Published(), nil
}

// resolveToAddress will try to resolve the input to an address using the
// mount table, if the input is not already an address.
func (s *server) resolveToAddress(address string) (string, error) {
	if _, err := inaming.NewEndpoint(address); err == nil {
		return address, nil
	}
	var names []string
	if s.ns != nil {
		var err error
		if names, err = s.ns.Resolve(s.ctx, address); err != nil {
			return "", err
		}
	} else {
		names = append(names, address)
	}
	for _, n := range names {
		address, suffix := naming.SplitAddressName(n)
		if suffix != "" && suffix != "//" {
			continue
		}
		if _, err := inaming.NewEndpoint(address); err == nil {
			return address, nil
		}
	}
	return "", fmt.Errorf("unable to resolve %q to an endpoint", address)
}

// preferredIPAddress returns the preferred IP address, which is,
// a public IPv4 address, then any non-loopback IPv4, then a public
// IPv6 address and finally any non-loopback/link-local IPv6
func preferredIPAddress(network string, addrs []net.Addr) (net.Addr, error) {
	if !netstate.IsIPNetwork(network) {
		return nil, fmt.Errorf("can't support network %q", network)
	}
	al := netstate.AddrList(addrs)
	for _, predicate := range []netstate.Predicate{netstate.IsPublicUnicastIPv4,
		netstate.IsUnicastIPv4, netstate.IsPublicUnicastIPv6} {
		if a := al.First(predicate); a != nil {
			return a, nil
		}
	}
	return nil, fmt.Errorf("failed to find any usable address for %q", network)
}

func (s *server) Listen(protocol, address string) (naming.Endpoint, error) {
	s.Lock()
	// Shortcut if the server is stopped, to avoid needlessly creating a
	// listener.
	if s.stopped {
		s.Unlock()
		return nil, errServerStopped
	}
	s.Unlock()
	var proxyName string
	if protocol == inaming.Network {
		proxyName = address
		var err error
		if address, err = s.resolveToAddress(address); err != nil {
			return nil, err
		}
	}
	ln, ep, err := s.streamMgr.Listen(protocol, address, s.listenerOpts...)
	if err != nil {
		vlog.Errorf("ipc: Listen on %v %v failed: %v", protocol, address, err)
		return nil, err
	}
	iep, ok := ep.(*inaming.Endpoint)
	if !ok {
		return nil, fmt.Errorf("ipc: Listen on %v %v failed translating internal endpoint data types", protocol, address)
	}

	// We know the endpoint format, so we crack it open...
	switch iep.Protocol {
	case "tcp", "tcp4", "tcp6":
		host, port, err := net.SplitHostPort(iep.Address)
		if err != nil {
			return nil, err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, fmt.Errorf("ipc: Listen(%q, %q) failed to parse IP address from address", protocol, address)
		}
		if ip.IsUnspecified() && s.preferredAddress != nil {
			// Need to find a usable IP address.
			addrs, err := netstate.GetAccessibleIPs()
			if err == nil {
				if a, err := s.preferredAddress(protocol, addrs); err == nil {
					if ip := netstate.AsIP(a); ip != nil {
						// a may be an IPNet or an IPAddr under the covers,
						// but we really want the IP portion without any
						// netmask so we use AsIP to ensure that.
						iep.Address = net.JoinHostPort(ip.String(), port)
					}
				}
			}
		}
	}

	s.Lock()
	if s.stopped {
		s.Unlock()
		// Ignore error return since we can't really do much about it.
		ln.Close()
		return nil, errServerStopped
	}
	s.listeners[ln] = true
	// We have a single goroutine per listener to accept new flows.
	// Each flow is served from its own goroutine.
	s.active.Add(1)
	if protocol == inaming.Network {
		go func(ln stream.Listener, ep naming.Endpoint, proxy string) {
			s.proxyListenLoop(ln, ep, proxy)
			s.active.Done()
		}(ln, ep, proxyName)
	} else {
		go func(ln stream.Listener, ep naming.Endpoint) {
			s.listenLoop(ln, ep)
			s.active.Done()
		}(ln, ep)
	}
	s.Unlock()
	s.publisher.AddServer(s.publishEP(ep))
	return ep, nil
}

func (s *server) publishEP(ep naming.Endpoint) string {
	var name string
	if !s.servesMountTable {
		// Make sure that client MountTable code doesn't try and
		// ResolveStep past this final address.
		name = "//"
	}
	return naming.JoinAddressName(ep.String(), name)
}

func (s *server) proxyListenLoop(ln stream.Listener, ep naming.Endpoint, proxy string) {
	const (
		min = 5 * time.Millisecond
		max = 5 * time.Minute
	)
	for {
		s.listenLoop(ln, ep)
		// The listener is done, so:
		// (1) Unpublish its name
		s.publisher.RemoveServer(s.publishEP(ep))
		// (2) Reconnect to the proxy unless the server has been stopped
		backoff := min
		ln = nil
		for ln == nil {
			select {
			case <-time.After(backoff):
				resolved, err := s.resolveToAddress(proxy)
				if err != nil {
					vlog.VI(1).Infof("Failed to resolve proxy %q (%v), will retry in %v", proxy, err, backoff)
					break
				}
				ln, ep, err = s.streamMgr.Listen(inaming.Network, resolved, s.listenerOpts...)
				if err == nil {
					vlog.VI(1).Infof("Reconnected to proxy at %q listener: (%v, %v)", proxy, ln, ep)
					break
				}
				if backoff = backoff * 2; backoff > max {
					backoff = max
				}
				vlog.VI(1).Infof("Proxy reconnection failed, will retry in %v", backoff)
			case <-s.stoppedChan:
				return
			}
		}
		// (3) reconnected, publish new address
		s.publisher.AddServer(s.publishEP(ep))
		s.Lock()
		s.listeners[ln] = true
		s.Unlock()
	}
}

func (s *server) listenLoop(ln stream.Listener, ep naming.Endpoint) {
	defer vlog.VI(1).Infof("ipc: Stopped listening on %v", ep)
	defer func() {
		s.Lock()
		delete(s.listeners, ln)
		s.Unlock()
	}()
	for {
		flow, err := ln.Accept()
		if err != nil {
			vlog.VI(10).Infof("ipc: Accept on %v failed: %v", ln, err)
			return
		}
		s.active.Add(1)
		go func(flow stream.Flow) {
			if err := newFlowServer(flow, s).serve(); err != nil {
				// TODO(caprita): Logging errors here is
				// too spammy. For example, "not
				// authorized" errors shouldn't be
				// logged as server errors.
				vlog.Errorf("Flow serve on %v failed: %v", ln, err)
			}
			s.active.Done()
		}(flow)
	}
}

func (s *server) Serve(name string, disp ipc.Dispatcher) error {
	s.Lock()
	defer s.Unlock()
	if s.stopped {
		return errServerStopped
	}
	if s.disp != nil && disp != nil && s.disp != disp {
		return fmt.Errorf("attempt to change dispatcher")
	}
	if disp != nil {
		s.disp = disp
	}
	if len(name) > 0 {
		s.publisher.AddName(name)
	}
	return nil
}

func (s *server) Stop() error {
	s.Lock()
	if s.stopped {
		s.Unlock()
		return nil
	}
	s.stopped = true
	close(s.stoppedChan)
	s.Unlock()

	// Note, It's safe to Stop/WaitForStop on the publisher outside of the
	// server lock, since publisher is safe for concurrent access.

	// Stop the publisher, which triggers unmounting of published names.
	s.publisher.Stop()
	// Wait for the publisher to be done unmounting before we can proceed to
	// close the listeners (to minimize the number of mounted names pointing
	// to endpoint that are no longer serving).
	//
	// TODO(caprita): See if make sense to fail fast on rejecting
	// connections once listeners are closed, and parallelize the publisher
	// and listener shutdown.
	s.publisher.WaitForStop()

	s.Lock()
	// Close all listeners.  No new flows will be accepted, while in-flight
	// flows will continue until they terminate naturally.
	nListeners := len(s.listeners)
	errCh := make(chan error, nListeners)
	for ln, _ := range s.listeners {
		go func(ln stream.Listener) {
			errCh <- ln.Close()
		}(ln)
	}
	s.Unlock()
	var firstErr error
	for i := 0; i < nListeners; i++ {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// At this point, we are guaranteed that no new requests are going to be
	// accepted.

	// Wait for the publisher and active listener + flows to finish.
	s.active.Wait()
	s.Lock()
	s.disp = nil
	s.Unlock()
	return firstErr
}

// flowServer implements the RPC server-side protocol for a single RPC, over a
// flow that's already connected to the client.
type flowServer struct {
	context.T
	server *server        // ipc.Server that this flow server belongs to
	disp   ipc.Dispatcher // ipc.Dispatcher that will serve RPCs on this flow
	dec    *vom.Decoder   // to decode requests and args from the client
	enc    *vom.Encoder   // to encode responses and results to the client
	flow   stream.Flow    // underlying flow
	// Fields filled in during the server invocation.

	// authorizedRemoteID is the PublicID obtained after authorizing the remoteID
	// of the underlying flow for the current request context.
	authorizedRemoteID security.PublicID
	blessing           security.PublicID
	method, suffix     string
	label              security.Label
	discharges         security.CaveatDischargeMap
	deadline           time.Time
	endStreamArgs      bool // are the stream args at EOF?
}

func newFlowServer(flow stream.Flow, server *server) *flowServer {
	server.Lock()
	disp := server.disp
	server.Unlock()
	return &flowServer{
		server: server,
		disp:   disp,
		// TODO(toddw): Support different codecs
		dec:        vom.NewDecoder(flow),
		enc:        vom.NewEncoder(flow),
		flow:       flow,
		discharges: make(security.CaveatDischargeMap),
	}
}

// Vom does not encode untyped nils.
// Consequently, the ipc system does not allow nil results with an interface
// type from server methods.  The one exception being errors.
//
// For now, the following hacky assumptions are made, which will be revisited when
// a decision is made on how untyped nils should be encoded/decoded in
// vom/vom2:
//
// - Server methods return 0 or more results
// - Any values returned by the server that have an interface type are either
//   non-nil or of type error.
func result2vom(res interface{}) vom.Value {
	v := vom.ValueOf(res)
	if !v.IsValid() {
		// Untyped nils are assumed to be nil-errors.
		var boxed verror.E
		return vom.ValueOf(&boxed).Elem()
	}
	if err, iserr := res.(error); iserr {
		// Convert errors to verror since errors are often not
		// serializable via vom/gob (errors.New and fmt.Errorf return a
		// type with no exported fields).
		return vom.ValueOf(verror.Convert(err))
	}
	return v
}

func defaultACL(id security.PublicID) security.ACL {
	if id == nil {
		return security.ACL{}
	}
	in := map[security.BlessingPattern]security.LabelSet{}
	for _, n := range id.Names() {
		in[security.BlessingPattern(n+security.ChainSeparator+string(security.AllPrincipals))] = security.AllLabels
	}
	return security.ACL{In: in}
}

func (fs *flowServer) serve() error {
	defer fs.flow.Close()
	results, err := fs.processRequest()
	// Respond to the client with the response header and positional results.
	response := ipc.Response{
		Error:            err,
		EndStreamResults: true,
		NumPosResults:    uint64(len(results)),
	}
	if err := fs.enc.Encode(response); err != nil {
		return verror.BadProtocolf("ipc: response encoding failed: %v", err)
	}
	if response.Error != nil {
		return response.Error
	}
	for ix, res := range results {
		if err := fs.enc.EncodeValue(result2vom(res)); err != nil {
			return verror.BadProtocolf("ipc: result #%d [%T=%v] encoding failed: %v", ix, res, res, err)
		}
	}
	// TODO(ashankar): Should unread data from the flow be drained?
	//
	// Reason to do so:
	// The common stream.Flow implementation (veyron/runtimes/google/ipc/stream/vc/reader.go)
	// uses iobuf.Slices backed by an iobuf.Pool. If the stream is not drained, these
	// slices will not be returned to the pool leading to possibly increased memory usage.
	//
	// Reason to not do so:
	// Draining here will conflict with any Reads on the flow in a separate goroutine
	// (for example, see TestStreamReadTerminatedByServer in full_test.go).
	//
	// For now, go with the reason to not do so as having unread data in the stream
	// should be a rare case.
	return nil
}

func (fs *flowServer) processRequest() ([]interface{}, verror.E) {
	start := time.Now()
	// Set a default timeout before reading from the flow. Without this timeout,
	// a client that sends no request or a partial request will retain the flow
	// indefinitely (and lock up server resources).
	deadline := start.Add(defaultCallTimeout)
	if verr := fs.setDeadline(deadline); verr != nil {
		return nil, verr
	}
	// Decode the initial request.
	var req ipc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return nil, verror.BadProtocolf("ipc: request decoding failed: %v", err)
	}
	fs.method = req.Method

	// Set the appropriate deadline, if specified.
	if req.Timeout == ipc.NoTimeout {
		deadline = time.Time{}
	} else if req.Timeout > 0 {
		deadline = start.Add(time.Duration(req.Timeout))
	}
	if verr := fs.setDeadline(deadline); verr != nil {
		return nil, verr
	}

	runtime := veyron2.RuntimeFromContext(fs.server.ctx)
	var cancel context.CancelFunc
	if !deadline.IsZero() {
		fs.T, cancel = InternalNewContext(runtime).WithDeadline(deadline)
	} else {
		fs.T, cancel = InternalNewContext(runtime).WithCancel()
	}

	// Notify the context when the channel is closed.
	go func() {
		<-fs.flow.Closed()
		cancel()
	}()

	// If additional credentials are provided, make them available in the context
	if req.HasBlessing {
		if err := fs.dec.Decode(&fs.blessing); err != nil {
			return nil, verror.BadProtocolf("ipc: blessing decoding failed: %v", err)
		}
		// Detect unusable blessings now, rather then discovering they are unusable on first use.
		if !reflect.DeepEqual(fs.blessing.PublicKey(), fs.flow.LocalID().PublicKey()) {
			return nil, verror.BadProtocolf("ipc: blessing provided not bound to this server")
		}
		// TODO(ashankar,ataly): Potential confused deputy attack: The client provides the
		// server's identity as the blessing. Figure out what we want to do about this -
		// should servers be able to assume that a blessing is something that does not
		// have the authorizations that the server's own identity has?
	}
	// Receive third party caveat discharges the client sent
	for i := uint64(0); i < req.NumDischarges; i++ {
		var d security.ThirdPartyDischarge
		if err := fs.dec.Decode(&d); err != nil {
			return nil, verror.BadProtocolf("ipc: decoding discharge %d of %d failed: %v", i, req.NumDischarges, err)
		}
		fs.discharges[d.CaveatID()] = d
	}
	// Lookup the invoker.
	invoker, auth, suffix, verr := fs.lookup(req.Suffix, req.Method)
	fs.suffix = suffix // with leading /'s stripped
	if verr != nil {
		return nil, verr
	}
	// Prepare invoker and decode args.
	numArgs := int(req.NumPosArgs)
	argptrs, label, err := invoker.Prepare(req.Method, numArgs)
	fs.label = label
	if err != nil {
		return nil, verror.Makef(verror.ErrorID(err), "%s: name: %q", err, req.Suffix)
	}
	if len(argptrs) != numArgs {
		return nil, verror.BadProtocolf(fmt.Sprintf("ipc: wrong number of input arguments for method %q, name %q (called with %d args, expected %d)", req.Method, req.Suffix, numArgs, len(argptrs)))
	}
	for ix, argptr := range argptrs {
		if err := fs.dec.Decode(argptr); err != nil {
			return nil, verror.BadProtocolf("ipc: arg %d decoding failed: %v", ix, err)
		}
	}
	// Authorize the PublicID at the remote end of the flow for the request context.
	if remoteID := fs.flow.RemoteID(); remoteID != nil {
		if fs.authorizedRemoteID, err = remoteID.Authorize(isecurity.NewContext(
			isecurity.ContextArgs{
				LocalID:    fs.flow.LocalID(),
				RemoteID:   fs.flow.RemoteID(),
				Method:     fs.method,
				Suffix:     fs.suffix,
				Discharges: fs.discharges,
				Label:      fs.label})); err != nil {
			return nil, errNotAuthorized(err)
		}
	}
	// Check application's authorization policy and invoke the method.
	if err := fs.authorize(auth); err != nil {
		// TODO(ataly, ashankar): For privacy reasons, should we hide the authorizer error (err)?
		return nil, errNotAuthorized(fmt.Errorf("%q not authorized for method %q: %v", fs.RemoteID(), fs.Method(), err))
	}
	results, err := invoker.Invoke(req.Method, fs, argptrs)
	fs.server.stats.record(req.Method, time.Since(start))
	return results, verror.Convert(err)
}

// lookup returns the invoker and authorizer responsible for serving the given
// name and method.  The name is stripped of any leading slashes, and the
// invoker is looked up in the server's dispatcher.  The (stripped) name
// and dispatch suffix are also returned.
func (fs *flowServer) lookup(name, method string) (ipc.Invoker, security.Authorizer, string, verror.E) {
	name = strings.TrimLeft(name, "/")
	if fs.disp != nil {
		invoker, auth, err := fs.disp.Lookup(name, method)
		switch {
		case err != nil:
			return nil, nil, "", verror.Convert(err)
		case invoker != nil:
			return invoker, auth, name, nil
		}
	}
	return nil, nil, "", verror.NotFoundf(fmt.Sprintf("ipc: dispatcher not found for %q", name))
}

func (fs *flowServer) authorize(auth security.Authorizer) error {
	if auth != nil {
		return auth.Authorize(fs)
	}
	// Since the provided authorizer is nil we create a default IDAuthorizer
	// for the local identity of the flow. This authorizer only authorizes
	// remote identities that have either been blessed by the local identity
	// or have blessed the local identity. (See vsecurity.NewACLAuthorizer)
	return vsecurity.NewACLAuthorizer(defaultACL(fs.flow.LocalID())).Authorize(fs)
}

// setDeadline sets a deadline on the flow. The flow will be cancelled if it
// is not closed by the specified deadline.
// A zero deadline (time.Time.IsZero) implies that no cancellation is desired.
func (fs *flowServer) setDeadline(deadline time.Time) verror.E {
	if err := fs.flow.SetDeadline(deadline); err != nil {
		return verror.Internalf("ipc: flow SetDeadline failed: %v", err)
	}
	return nil
}

// Send implements the ipc.Stream method.
func (fs *flowServer) Send(item interface{}) error {
	// The empty response header indicates what follows is a streaming result.
	if err := fs.enc.Encode(ipc.Response{}); err != nil {
		return err
	}
	return fs.enc.Encode(item)
}

// Recv implements the ipc.Stream method.
func (fs *flowServer) Recv(itemptr interface{}) error {
	var req ipc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return err
	}
	if req.EndStreamArgs {
		fs.endStreamArgs = true
		return io.EOF
	}
	return fs.dec.Decode(itemptr)
}

// Implementations of ipc.ServerContext methods.

func (fs *flowServer) CaveatDischarges() security.CaveatDischargeMap { return fs.discharges }

func (fs *flowServer) Server() ipc.Server { return fs.server }
func (fs *flowServer) Method() string     { return fs.method }

// TODO(cnicolaou): remove Name from ipc.ServerContext and all of
// its implementations
func (fs *flowServer) Name() string          { return fs.suffix }
func (fs *flowServer) Suffix() string        { return fs.suffix }
func (fs *flowServer) Label() security.Label { return fs.label }

func (fs *flowServer) LocalID() security.PublicID      { return fs.flow.LocalID() }
func (fs *flowServer) RemoteID() security.PublicID     { return fs.authorizedRemoteID }
func (fs *flowServer) Blessing() security.PublicID     { return fs.blessing }
func (fs *flowServer) LocalEndpoint() naming.Endpoint  { return fs.flow.LocalEndpoint() }
func (fs *flowServer) RemoteEndpoint() naming.Endpoint { return fs.flow.RemoteEndpoint() }
