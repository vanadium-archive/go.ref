package ipc

import (
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/security/access"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
	"veyron.io/veyron/veyron2/vom2"
	"veyron.io/veyron/veyron2/vtrace"

	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	"veyron.io/veyron/veyron/runtimes/google/lib/publisher"
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
	ivtrace "veyron.io/veyron/veyron/runtimes/google/vtrace"
)

var (
	// TODO(cnicolaou): this should be BadState in verror2.
	errServerStopped = verror.Abortedf("ipc: server is stopped")
)

type server struct {
	sync.Mutex
	ctx                context.T                         // context used by the server to make internal RPCs.
	streamMgr          stream.Manager                    // stream manager to listen for new flows.
	publisher          publisher.Publisher               // publisher to publish mounttable mounts.
	listenerOpts       []stream.ListenerOpt              // listener opts passed to Listen.
	listeners          map[stream.Listener]*dhcpListener // listeners created by Listen.
	disp               ipc.Dispatcher                    // dispatcher to serve RPCs
	dispReserved       ipc.Dispatcher                    // dispatcher for reserved methods
	active             sync.WaitGroup                    // active goroutines we've spawned.
	stopped            bool                              // whether the server has been stopped.
	stoppedChan        chan struct{}                     // closed when the server has been stopped.
	preferredProtocols []string                          // protocols to use when resolving proxy name to endpoint.
	ns                 naming.Namespace
	servesMountTable   bool
	// TODO(cnicolaou): remove this when the publisher tracks published names
	// and can return an appropriate error for RemoveName on a name that
	// wasn't 'Added' for this server.
	names map[string]struct{}
	// TODO(cnicolaou): add roaming stats to ipcStats
	stats      *ipcStats      // stats for this server.
	traceStore *ivtrace.Store // store for vtrace traces.
}

var _ ipc.Server = (*server)(nil)

type dhcpListener struct {
	sync.Mutex
	publisher *config.Publisher   // publisher used to fork the stream
	name      string              // name of the publisher stream
	ep        *inaming.Endpoint   // endpoint returned after listening
	pubAddrs  []ipc.Address       // addresses to publish
	pubPort   string              // port to use with the publish addresses
	ch        chan config.Setting // channel to receive settings over
}

// This option is used to sort and filter the endpoints when resolving the
// proxy name from a mounttable.
type PreferredServerResolveProtocols []string

func (PreferredServerResolveProtocols) IPCServerOpt() {}

func InternalNewServer(ctx context.T, streamMgr stream.Manager, ns naming.Namespace, store *ivtrace.Store, opts ...ipc.ServerOpt) (ipc.Server, error) {
	ctx, _ = ivtrace.WithNewSpan(ctx, "NewServer")
	statsPrefix := naming.Join("ipc", "server", "routing-id", streamMgr.RoutingID().String())
	s := &server{
		ctx:         ctx,
		streamMgr:   streamMgr,
		publisher:   publisher.New(ctx, ns, publishPeriod),
		listeners:   make(map[stream.Listener]*dhcpListener),
		stoppedChan: make(chan struct{}),
		ns:          ns,
		stats:       newIPCStats(statsPrefix),
		traceStore:  store,
	}
	var (
		principal security.Principal
		blessings security.Blessings
	)
	for _, opt := range opts {
		switch opt := opt.(type) {
		case stream.ListenerOpt:
			// Collect all ServerOpts that are also ListenerOpts.
			s.listenerOpts = append(s.listenerOpts, opt)
			switch opt := opt.(type) {
			case vc.LocalPrincipal:
				principal = opt.Principal
			case options.ServerBlessings:
				blessings = opt.Blessings
			}
		case options.ServesMountTable:
			s.servesMountTable = bool(opt)
		case options.ReservedNameDispatcher:
			s.dispReserved = opt.Dispatcher
		case PreferredServerResolveProtocols:
			s.preferredProtocols = []string(opt)
		}
	}
	blessingsStatsName := naming.Join(statsPrefix, "security", "blessings")
	if blessings != nil {
		// TODO(caprita): revist printing the blessings with %s, and
		// instead expose them as a list.
		stats.NewString(blessingsStatsName).Set(fmt.Sprintf("%s", blessings))
	} else if principal != nil { // principal should have been passed in, but just in case.
		stats.NewStringFunc(blessingsStatsName, func() string {
			return fmt.Sprintf("%s (default)", principal.BlessingStore().Default())
		})
	}
	return s, nil
}

func (s *server) Published() ([]string, error) {
	defer vlog.LogCall()()
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
	// An empty set of protocols means all protocols...
	ordered, err := filterAndOrderServers(names, s.preferredProtocols)
	if err != nil {
		return "", err
	}
	for _, n := range ordered {
		address, suffix := naming.SplitAddressName(n)
		if suffix != "" {
			continue
		}
		if _, err := inaming.NewEndpoint(address); err == nil {
			return address, nil
		}
	}
	return "", fmt.Errorf("unable to resolve %q to an endpoint", address)
}

func addrFromIP(ip net.IP) ipc.Address {
	return &netstate.AddrIfc{
		Addr: &net.IPAddr{IP: ip},
	}
}

// getIPRoamingAddrs finds an appropriate set of addresss to publish
// externally and also determines if it's sensible to allow roaming.
// It returns the host address of the first suitable address that
// can be used and the port number that can be used with all addresses.
// The host is required to allow the caller to construct an endpoint
// that can be returned to the caller of Listen.
func (s *server) getIPRoamingAddrs(chooser ipc.AddressChooser, iep *inaming.Endpoint) (addresses []ipc.Address, host string, port string, roaming bool, err error) {
	host, port, err = net.SplitHostPort(iep.Address)
	if err != nil {
		return nil, "", "", false, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, "", "", false, fmt.Errorf("failed to parse %q as an IP host", host)
	}
	if ip.IsUnspecified() && chooser != nil {
		// Need to find a usable IP address since the call to listen
		// didn't specify one.
		if addrs, err := netstate.GetAccessibleIPs(); err == nil {
			if a, err := chooser(iep.Protocol, addrs); err == nil && len(a) > 0 {
				phost := a[0].Address().String()
				iep.Address = net.JoinHostPort(phost, port)
				return a, phost, port, true, nil
			}
		}
		return []ipc.Address{addrFromIP(ip)}, host, port, true, nil
	}
	// Listen used a fixed IP address, which we take to mean that
	// roaming is not desired.
	return []ipc.Address{addrFromIP(ip)}, host, port, false, nil
}

// configureEPAndRoaming configures the endpoint and roaming. In particular,
// it fills in the Address portion of the endpoint with the appropriately
// selected network address and creates a dhcpListener struct if roaming
// is enabled.
func (s *server) configureEPAndRoaming(spec ipc.ListenSpec, ep naming.Endpoint) (*dhcpListener, *inaming.Endpoint, error) {
	iep, ok := ep.(*inaming.Endpoint)
	if !ok {
		return nil, nil, fmt.Errorf("internal type conversion error for %T", ep)
	}
	if !strings.HasPrefix(spec.Protocol, "tcp") {
		return nil, iep, nil
	}
	pubAddrs, pubHost, pubPort, roaming, err := s.getIPRoamingAddrs(spec.AddressChooser, iep)
	if err != nil {
		return nil, iep, err
	}
	iep.Address = net.JoinHostPort(pubHost, pubPort)
	if !roaming {
		vlog.VI(2).Infof("the address %q requested for listening contained a fixed IP address which disables roaming, use :0 instead", spec.Address)
	}
	publisher := spec.StreamPublisher
	if roaming && publisher != nil {
		streamName := spec.StreamName
		ch := make(chan config.Setting)
		if _, err := publisher.ForkStream(streamName, ch); err != nil {
			return nil, iep, fmt.Errorf("failed to fork stream %q: %s", streamName, err)
		}
		return &dhcpListener{ep: iep, pubAddrs: pubAddrs, pubPort: pubPort, ch: ch, name: streamName, publisher: publisher}, iep, nil
	}
	return nil, iep, nil
}

func (s *server) Listen(listenSpec ipc.ListenSpec) (naming.Endpoint, error) {
	defer vlog.LogCall()()
	s.Lock()
	// Shortcut if the server is stopped, to avoid needlessly creating a
	// listener.
	if s.stopped {
		s.Unlock()
		return nil, errServerStopped
	}
	s.Unlock()

	var iep *inaming.Endpoint
	var dhcpl *dhcpListener
	var ln stream.Listener

	if len(listenSpec.Address) > 0 {
		// Listen if we have a local address to listen on. Some situations
		// just need a proxy (e.g. a browser extension).
		tmpln, lep, err := s.streamMgr.Listen(listenSpec.Protocol, listenSpec.Address, s.listenerOpts...)
		if err != nil {
			vlog.Errorf("ipc: Listen on %s failed: %s", listenSpec, err)
			return nil, err
		}
		ln = tmpln
		if tmpdhcpl, tmpiep, err := s.configureEPAndRoaming(listenSpec, lep); err != nil {
			ln.Close()
			return nil, err
		} else {
			dhcpl = tmpdhcpl
			iep = tmpiep
		}
	}

	s.Lock()
	defer s.Unlock()
	if s.stopped {
		ln.Close()
		return nil, errServerStopped
	}

	if dhcpl != nil {
		// We have a goroutine to listen for dhcp changes.
		s.active.Add(1)
		go func() {
			s.dhcpLoop(dhcpl)
			s.active.Done()
		}()
		s.listeners[ln] = dhcpl
	} else if ln != nil {
		s.listeners[ln] = nil
	}

	if iep != nil {
		// We have a goroutine per listener to accept new flows.
		// Each flow is served from its own goroutine.
		s.active.Add(1)
		go func() {
			s.listenLoop(ln, iep)
			s.active.Done()
		}()
		s.publisher.AddServer(s.publishEP(iep, s.servesMountTable), s.servesMountTable)
		if strings.HasPrefix(iep.Protocol, "tcp") {
			epCopy := *iep
			epCopy.Protocol = "ws"
			s.publisher.AddServer(s.publishEP(&epCopy, s.servesMountTable), s.servesMountTable)
		}
	}

	if len(listenSpec.Proxy) > 0 {
		// We have a goroutine for listening on proxy connections.
		s.active.Add(1)
		go func() {
			s.proxyListenLoop(listenSpec.Proxy)
			s.active.Done()
		}()
	}
	return iep, nil
}

// TODO(cnicolaou): Take this out or make the ServesMountTable bit work in the endpoint.
func (s *server) publishEP(ep *inaming.Endpoint, servesMountTable bool) string {
	var name string
	ep.IsMountTable = servesMountTable
	return naming.JoinAddressName(ep.String(), name)
}

func (s *server) reconnectAndPublishProxy(proxy string) (*inaming.Endpoint, stream.Listener, error) {
	resolved, err := s.resolveToAddress(proxy)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to resolve proxy %q (%v)", proxy, err)
	}
	ln, ep, err := s.streamMgr.Listen(inaming.Network, resolved, s.listenerOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on %q: %s", resolved, err)
	}
	iep, ok := ep.(*inaming.Endpoint)
	if !ok {
		ln.Close()
		return nil, nil, fmt.Errorf("internal type conversion error for %T", ep)
	}
	s.Lock()
	s.listeners[ln] = nil
	s.Unlock()
	s.publisher.AddServer(s.publishEP(iep, s.servesMountTable), s.servesMountTable)

	if strings.HasPrefix(iep.Protocol, "tcp") {
		epCopy := *iep
		epCopy.Protocol = "ws"
		s.publisher.AddServer(s.publishEP(&epCopy, s.servesMountTable), s.servesMountTable)
	}

	return iep, ln, nil
}

func (s *server) proxyListenLoop(proxy string) {
	const (
		min = 5 * time.Millisecond
		max = 5 * time.Minute
	)

	iep, ln, err := s.reconnectAndPublishProxy(proxy)
	if err != nil {
		vlog.VI(1).Infof("Failed to connect to proxy: %s", err)
	}
	// the initial connection maybe have failed, but we enter the retry
	// loop anyway so that we will continue to try and connect to the
	// proxy.

	s.Lock()
	if s.stopped {
		s.Unlock()
		return
	}
	s.Unlock()

	for {
		if ln != nil && iep != nil {
			s.listenLoop(ln, iep)
			// The listener is done, so:
			// (1) Unpublish its name
			s.publisher.RemoveServer(s.publishEP(iep, s.servesMountTable))
			if strings.HasPrefix(iep.Protocol, "tcp") {
				iepCopy := *iep
				iepCopy.Protocol = "ws"
				s.publisher.RemoveServer(s.publishEP(&iepCopy, s.servesMountTable))
			}
		}

		s.Lock()
		if s.stopped {
			s.Unlock()
			return
		}
		s.Unlock()

		// (2) Reconnect to the proxy unless the server has been stopped
		backoff := min
		ln = nil
		for {
			select {
			case <-time.After(backoff):
				if backoff = backoff * 2; backoff > max {
					backoff = max
				}
			case <-s.stoppedChan:
				return
			}
			// (3) reconnect, publish new address
			if iep, ln, err = s.reconnectAndPublishProxy(proxy); err != nil {
				vlog.VI(1).Infof("Failed to reconnect to proxy %q: %s", proxy, err)
			} else {
				vlog.VI(1).Infof("Reconnected to proxy %q, %s", proxy, iep)
				break
			}
		}
	}
}

func (s *server) listenLoop(ln stream.Listener, ep naming.Endpoint) {
	defer vlog.VI(1).Infof("ipc: Stopped listening on %v", ep)
	var calls sync.WaitGroup
	defer func() {
		calls.Wait()
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
		calls.Add(1)
		go func(flow stream.Flow) {
			defer calls.Done()
			fs, err := newFlowServer(flow, s)
			if err != nil {
				vlog.Errorf("newFlowServer on %v failed: %v", ln, err)
				return
			}
			if err := fs.serve(); err != nil {
				// TODO(caprita): Logging errors here is too spammy. For example, "not
				// authorized" errors shouldn't be logged as server errors.
				vlog.Errorf("Flow serve on %v failed: %v", ln, err)
			}
		}(flow)
	}
}

func (s *server) applyChange(dhcpl *dhcpListener, addrs []net.Addr, fn func(string)) {
	dhcpl.Lock()
	defer dhcpl.Unlock()
	for _, a := range addrs {
		if ip := netstate.AsIP(a); ip != nil {
			dhcpl.ep.Address = net.JoinHostPort(ip.String(), dhcpl.pubPort)
			fn(s.publishEP(dhcpl.ep, s.servesMountTable))
		}
	}
}

func (s *server) dhcpLoop(dhcpl *dhcpListener) {
	defer vlog.VI(1).Infof("ipc: Stopped listen for dhcp changes on %v", dhcpl.ep)
	vlog.VI(2).Infof("ipc: dhcp loop")

	ep := *dhcpl.ep
	// Publish all of the addresses
	for _, pubAddr := range dhcpl.pubAddrs {
		ep.Address = net.JoinHostPort(pubAddr.Address().String(), dhcpl.pubPort)
		s.publisher.AddServer(s.publishEP(&ep, s.servesMountTable), s.servesMountTable)
	}

	for setting := range dhcpl.ch {
		if setting == nil {
			return
		}
		switch v := setting.Value().(type) {
		case bool:
			return
		case []net.Addr:
			s.Lock()
			if s.stopped {
				s.Unlock()
				return
			}
			publisher := s.publisher
			s.Unlock()
			switch setting.Name() {
			case ipc.NewAddrsSetting:
				vlog.Infof("Added some addresses: %q", v)
				s.applyChange(dhcpl, v, func(name string) { publisher.AddServer(name, s.servesMountTable) })
			case ipc.RmAddrsSetting:
				vlog.Infof("Removed some addresses: %q", v)
				s.applyChange(dhcpl, v, publisher.RemoveServer)
			}
		}
	}
}

func (s *server) Serve(name string, obj interface{}, authorizer security.Authorizer) error {
	if obj == nil {
		// The ReflectInvoker inside the LeafDispatcher will panic
		// if called for a nil value.
		return fmt.Errorf("A nil object is not allowed")
	}
	return s.ServeDispatcher(name, ipc.LeafDispatcher(obj, authorizer))
}

func (s *server) ServeDispatcher(name string, disp ipc.Dispatcher) error {
	s.Lock()
	defer s.Unlock()
	ivtrace.FromContext(s.ctx).Annotate("Serving under name: " + name)

	if s.stopped {
		return errServerStopped
	}
	if disp == nil {
		return fmt.Errorf("A nil dispacther is not allowed")
	}
	if s.disp != nil {
		return fmt.Errorf("Serve or ServeDispatcher has already been called")
	}
	s.disp = disp
	s.names = make(map[string]struct{})
	if len(name) > 0 {
		s.publisher.AddName(name)
		s.names[name] = struct{}{}
	}
	return nil
}

func (s *server) AddName(name string) error {
	s.Lock()
	defer s.Unlock()
	ivtrace.FromContext(s.ctx).Annotate("Serving under name: " + name)
	if len(name) == 0 {
		return fmt.Errorf("empty name")
	}
	if s.stopped {
		return errServerStopped
	}
	if s.disp == nil {
		return fmt.Errorf("Adding name before calling Serve or ServeDispatcher is not allowed")
	}
	s.publisher.AddName(name)
	// TODO(cnicolaou): remove this map when the publisher's RemoveName
	// method returns an error.
	s.names[name] = struct{}{}
	return nil
}

func (s *server) RemoveName(name string) error {
	s.Lock()
	defer s.Unlock()
	ivtrace.FromContext(s.ctx).Annotate("Removed name: " + name)
	if s.stopped {
		return errServerStopped
	}
	if s.disp == nil {
		return fmt.Errorf("Removing name before calling Serve or ServeDispatcher is not allowed")
	}
	if _, present := s.names[name]; !present {
		return fmt.Errorf("%q has not been previously used for this server", name)
	}
	s.publisher.RemoveName(name)
	delete(s.names, name)
	return nil
}

func (s *server) Stop() error {
	defer vlog.LogCall()()
	s.Lock()
	if s.stopped {
		s.Unlock()
		return nil
	}
	s.stopped = true
	close(s.stoppedChan)
	s.Unlock()

	// Delete the stats object.
	s.stats.stop()

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

	for ln, dhcpl := range s.listeners {
		go func(ln stream.Listener) {
			errCh <- ln.Close()
		}(ln)
		if dhcpl != nil {
			dhcpl.Lock()
			dhcpl.publisher.CloseFork(dhcpl.name, dhcpl.ch)
			dhcpl.ch <- config.NewBool("EOF", "stop", true)
			dhcpl.Unlock()
		}
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

// TODO(toddw): Remove these interfaces after the vom2 transition.
type vomEncoder interface {
	Encode(v interface{}) error
}

type vomDecoder interface {
	Decode(v interface{}) error
}

// flowServer implements the RPC server-side protocol for a single RPC, over a
// flow that's already connected to the client.
type flowServer struct {
	context.T
	server *server        // ipc.Server that this flow server belongs to
	disp   ipc.Dispatcher // ipc.Dispatcher that will serve RPCs on this flow
	dec    vomDecoder     // to decode requests and args from the client
	enc    vomEncoder     // to encode responses and results to the client
	flow   stream.Flow    // underlying flow

	// Fields filled in during the server invocation.
	blessings      security.Blessings
	method, suffix string
	tags           []interface{}
	discharges     map[string]security.Discharge
	starttime      time.Time
	endStreamArgs  bool // are the stream args at EOF?
	allowDebug     bool // true if the caller is permitted to view debug information.
}

var _ ipc.Stream = (*flowServer)(nil)

func newFlowServer(flow stream.Flow, server *server) (*flowServer, error) {
	server.Lock()
	disp := server.disp
	server.Unlock()

	fs := &flowServer{
		T:          server.ctx,
		server:     server,
		disp:       disp,
		flow:       flow,
		discharges: make(map[string]security.Discharge),
	}
	if vom2.IsEnabled() {
		var err error
		if fs.dec, err = vom2.NewDecoder(flow); err != nil {
			flow.Close()
			return nil, err
		}
		if fs.enc, err = vom2.NewBinaryEncoder(flow); err != nil {
			flow.Close()
			return nil, err
		}
	} else {
		fs.dec = vom.NewDecoder(flow)
		fs.enc = vom.NewEncoder(flow)
	}
	return fs, nil
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
func vomErrorHack(res interface{}) vom.Value {
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

// TODO(toddw): Remove this function and encodeValueHack after the vom2 transition.
func vom2ErrorHack(res interface{}) interface{} {
	if err, ok := res.(error); ok {
		return &err
	}
	return res
}

// TODO(toddw): Remove this function and vom2ErrorHack after the vom2 transition.
func (fs *flowServer) encodeValueHack(res interface{}) error {
	if vom2.IsEnabled() {
		return fs.enc.Encode(vom2ErrorHack(res))
	}
	return fs.enc.(*vom.Encoder).EncodeValue(vomErrorHack(res))
}

func (fs *flowServer) serve() error {
	defer fs.flow.Close()

	results, err := fs.processRequest()

	ivtrace.FromContext(fs).Finish()

	var traceResponse vtrace.Response
	if fs.allowDebug {
		traceResponse = ivtrace.Response(fs)
	}

	// Respond to the client with the response header and positional results.
	response := ipc.Response{
		Error:            err,
		EndStreamResults: true,
		NumPosResults:    uint64(len(results)),
		TraceResponse:    traceResponse,
	}
	if err := fs.enc.Encode(response); err != nil {
		return verror.BadProtocolf("ipc: response encoding failed: %v", err)
	}
	if response.Error != nil {
		return response.Error
	}
	for ix, res := range results {
		if err := fs.encodeValueHack(res); err != nil {
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

func (fs *flowServer) readIPCRequest() (*ipc.Request, verror.E) {
	// Set a default timeout before reading from the flow. Without this timeout,
	// a client that sends no request or a partial request will retain the flow
	// indefinitely (and lock up server resources).
	initTimer := newTimer(defaultCallTimeout)
	defer initTimer.Stop()
	fs.flow.SetDeadline(initTimer.C)

	// Decode the initial request.
	var req ipc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return nil, verror.BadProtocolf("ipc: request decoding failed: %v", err)
	}
	return &req, nil
}

func (fs *flowServer) processRequest() ([]interface{}, verror.E) {
	fs.starttime = time.Now()
	req, verr := fs.readIPCRequest()
	if verr != nil {
		// We don't know what the ipc call was supposed to be, but we'll create
		// a placeholder span so we can capture annotations.
		fs.T, _ = ivtrace.WithNewSpan(fs, fmt.Sprintf("\"%s\".UNKNOWN", fs.Name()))
		return nil, verr
	}
	fs.method = req.Method
	fs.suffix = strings.TrimLeft(req.Suffix, "/")

	// TODO(mattr): Currently this allows users to trigger trace collection
	// on the server even if they will not be allowed to collect the
	// results later.  This might be considered a DOS vector.
	spanName := fmt.Sprintf("\"%s\".%s", fs.Name(), fs.Method())
	fs.T, _ = ivtrace.WithContinuedSpan(fs, spanName, req.TraceRequest, fs.server.traceStore)

	var cancel context.CancelFunc
	if req.Timeout != ipc.NoTimeout {
		fs.T, cancel = fs.WithDeadline(fs.starttime.Add(time.Duration(req.Timeout)))
	} else {
		fs.T, cancel = fs.WithCancel()
	}
	fs.flow.SetDeadline(fs.Done())
	go fs.cancelContextOnClose(cancel)

	// Initialize security: blessings, discharges, etc.
	if verr := fs.initSecurity(req); verr != nil {
		return nil, verr
	}
	// Lookup the invoker.
	invoker, auth, verr := fs.lookup(fs.suffix, &fs.method)
	if verr != nil {
		return nil, verr
	}
	// Prepare invoker and decode args.
	numArgs := int(req.NumPosArgs)
	argptrs, tags, err := invoker.Prepare(fs.method, numArgs)
	fs.tags = tags
	if err != nil {
		return nil, verror.Makef(verror.ErrorID(err), "%s: name: %q", err, fs.suffix)
	}
	if len(argptrs) != numArgs {
		return nil, verror.BadProtocolf(fmt.Sprintf("ipc: wrong number of input arguments for method %q, name %q (called with %d args, expected %d)", fs.method, fs.suffix, numArgs, len(argptrs)))
	}
	for ix, argptr := range argptrs {
		if err := fs.dec.Decode(argptr); err != nil {
			return nil, verror.BadProtocolf("ipc: arg %d decoding failed: %v", ix, err)
		}
	}
	// Check application's authorization policy.
	if verr := authorize(fs, auth); verr != nil {
		return nil, verr
	}
	// Check if the caller is permitted to view debug information.
	// TODO(mattr): Is access.Debug the right thing to check?
	fs.allowDebug = authorize(debugContext{fs}, auth) == nil
	// Invoke the method.
	results, err := invoker.Invoke(fs.method, fs, argptrs)
	fs.server.stats.record(fs.method, time.Since(fs.starttime))
	return results, verror.Convert(err)
}

func (fs *flowServer) cancelContextOnClose(cancel context.CancelFunc) {
	// Ensure that the context gets cancelled if the flow is closed
	// due to a network error, or client cancellation.
	select {
	case <-fs.flow.Closed():
		// Here we remove the contexts channel as a deadline to the flow.
		// We do this to ensure clients get a consistent error when they read/write
		// after the flow is closed.  Since the flow is already closed, it doesn't
		// matter that the context is also cancelled.
		fs.flow.SetDeadline(nil)
		cancel()
	case <-fs.Done():
	}
}

// lookup returns the invoker and authorizer responsible for serving the given
// name and method.  The suffix is stripped of any leading slashes. If it begins
// with ipc.DebugKeyword, we use the internal debug dispatcher to look up the
// invoker. Otherwise, and we use the server's dispatcher. The suffix and method
// value may be modified to match the actual suffix and method to use.
func (fs *flowServer) lookup(suffix string, method *string) (ipc.Invoker, security.Authorizer, verror.E) {
	if naming.IsReserved(*method) {
		// All reserved methods are trapped and handled here, by removing the
		// reserved prefix and invoking them on reservedMethods.  E.g. "__Glob"
		// invokes reservedMethods.Glob.
		*method = naming.StripReserved(*method)
		return reservedInvoker(fs.disp, fs.server.dispReserved), &acceptAllAuthorizer{}, nil
	}
	disp := fs.disp
	if naming.IsReserved(suffix) {
		disp = fs.server.dispReserved
	}
	if disp != nil {
		obj, auth, err := disp.Lookup(suffix)
		switch {
		case err != nil:
			return nil, nil, verror.Convert(err)
		case obj != nil:
			return objectToInvoker(obj), auth, nil
		}
	}
	return nil, nil, verror.NoExistf("ipc: invoker not found for %q", suffix)
}

func objectToInvoker(obj interface{}) ipc.Invoker {
	if obj == nil {
		return nil
	}
	if invoker, ok := obj.(ipc.Invoker); ok {
		return invoker
	}
	return ipc.ReflectInvoker(obj)
}

func (fs *flowServer) initSecurity(req *ipc.Request) verror.E {
	// If additional credentials are provided, make them available in the context
	blessings, err := security.NewBlessings(req.GrantedBlessings)
	if err != nil {
		return verror.BadProtocolf("ipc: failed to decode granted blessings: %v", err)
	}
	fs.blessings = blessings
	// Detect unusable blessings now, rather then discovering they are unusable on
	// first use.
	//
	// TODO(ashankar,ataly): Potential confused deputy attack: The client provides
	// the server's identity as the blessing. Figure out what we want to do about
	// this - should servers be able to assume that a blessing is something that
	// does not have the authorizations that the server's own identity has?
	if blessings != nil && !reflect.DeepEqual(blessings.PublicKey(), fs.flow.LocalPrincipal().PublicKey()) {
		return verror.NoAccessf("ipc: blessing granted not bound to this server(%v vs %v)", blessings.PublicKey(), fs.flow.LocalPrincipal().PublicKey())
	}
	// Receive third party caveat discharges the client sent
	for i := uint64(0); i < req.NumDischarges; i++ {
		var d security.Discharge
		if err := fs.dec.Decode(&d); err != nil {
			return verror.BadProtocolf("ipc: decoding discharge %d of %d failed: %v", i, req.NumDischarges, err)
		}
		fs.discharges[d.ID()] = d
	}
	return nil
}

type acceptAllAuthorizer struct{}

func (acceptAllAuthorizer) Authorize(security.Context) error {
	return nil
}

func authorize(ctx security.Context, auth security.Authorizer) verror.E {
	if ctx.LocalPrincipal() == nil {
		// LocalPrincipal is nil means that the server wanted to avoid
		// authentication, and thus wanted to skip authorization as well.
		return nil
	}
	if auth == nil {
		auth = defaultAuthorizer{}
	}
	if err := auth.Authorize(ctx); err != nil {
		// TODO(ataly, ashankar): For privacy reasons, should we hide the authorizer error?
		return verror.NoAccessf("ipc: not authorized to call %q.%q (%v)", ctx.Suffix(), ctx.Method(), err)
	}
	return nil
}

// debugContext is a context which wraps another context but always returns
// the debug tag.
type debugContext struct {
	security.Context
}

func (debugContext) MethodTags() []interface{} {
	return []interface{}{access.Debug}
}

// Send implements the ipc.Stream method.
func (fs *flowServer) Send(item interface{}) error {
	defer vlog.LogCall()()
	// The empty response header indicates what follows is a streaming result.
	if err := fs.enc.Encode(ipc.Response{}); err != nil {
		return err
	}
	return fs.enc.Encode(item)
}

// Recv implements the ipc.Stream method.
func (fs *flowServer) Recv(itemptr interface{}) error {
	defer vlog.LogCall()()
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

func (fs *flowServer) RemoteDischarges() map[string]security.Discharge {
	//nologcall
	return fs.discharges
}
func (fs *flowServer) Server() ipc.Server {
	//nologcall
	return fs.server
}
func (fs *flowServer) Timestamp() time.Time {
	//nologcall
	return fs.starttime
}
func (fs *flowServer) Method() string {
	//nologcall
	return fs.method
}
func (fs *flowServer) MethodTags() []interface{} {
	//nologcall
	return fs.tags
}

// TODO(cnicolaou): remove Name from ipc.ServerContext and all of
// its implementations
func (fs *flowServer) Name() string {
	//nologcall
	return fs.suffix
}
func (fs *flowServer) Suffix() string {
	//nologcall
	return fs.suffix
}
func (fs *flowServer) LocalPrincipal() security.Principal {
	//nologcall
	return fs.flow.LocalPrincipal()
}
func (fs *flowServer) LocalBlessings() security.Blessings {
	//nologcall
	return fs.flow.LocalBlessings()
}
func (fs *flowServer) RemoteBlessings() security.Blessings {
	//nologcall
	return fs.flow.RemoteBlessings()
}
func (fs *flowServer) Blessings() security.Blessings {
	//nologcall
	return fs.blessings
}
func (fs *flowServer) LocalEndpoint() naming.Endpoint {
	//nologcall
	return fs.flow.LocalEndpoint()
}
func (fs *flowServer) RemoteEndpoint() naming.Endpoint {
	//nologcall
	return fs.flow.RemoteEndpoint()
}
