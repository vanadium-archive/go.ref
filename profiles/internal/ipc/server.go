package ipc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"v.io/v23/config"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
	"v.io/x/ref/profiles/internal/ipc/stream"

	"v.io/x/ref/lib/netstate"
	"v.io/x/ref/lib/publisher"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/profiles/internal/ipc/stream/vc"
	inaming "v.io/x/ref/profiles/internal/naming"
	ivtrace "v.io/x/ref/profiles/internal/vtrace"

	// TODO(cnicolaou): finish verror2 -> verror transition, in particular
	// for communicating from server to client.
)

// state for each requested listen address
type listenState struct {
	protocol, address string
	ln                stream.Listener
	lep               naming.Endpoint
	lnerr, eperr      error
	roaming           bool
	// We keep track of all of the endpoints, the port and a copy of
	// the original listen endpoint for use with roaming network changes.
	ieps     []*inaming.Endpoint // list of currently active eps
	port     string              // port to use for creating new eps
	protoIEP inaming.Endpoint    // endpoint to use as template for new eps (includes rid, versions etc)
}

// state for each requested proxy
type proxyState struct {
	endpoint naming.Endpoint
	err      error
}

type dhcpState struct {
	name      string
	publisher *config.Publisher
	stream    *config.Stream
	ch        chan config.Setting // channel to receive dhcp settings over
	err       error               // error status.
	watchers  map[chan<- ipc.NetworkChange]struct{}
}

type server struct {
	sync.Mutex
	// context used by the server to make internal RPCs, error messages etc.
	ctx          *context.T
	cancel       context.CancelFunc   // function to cancel the above context.
	state        serverState          // track state of the server.
	streamMgr    stream.Manager       // stream manager to listen for new flows.
	publisher    publisher.Publisher  // publisher to publish mounttable mounts.
	listenerOpts []stream.ListenerOpt // listener opts for Listen.
	dhcpState    *dhcpState           // dhcpState, nil if not using dhcp

	// maps that contain state on listeners.
	listenState map[*listenState]struct{}
	listeners   map[stream.Listener]struct{}

	// state of proxies keyed by the name of the proxy
	proxies map[string]proxyState

	// all endpoints generated and returned by this server
	endpoints []naming.Endpoint

	disp               ipc.Dispatcher // dispatcher to serve RPCs
	dispReserved       ipc.Dispatcher // dispatcher for reserved methods
	active             sync.WaitGroup // active goroutines we've spawned.
	stoppedChan        chan struct{}  // closed when the server has been stopped.
	preferredProtocols []string       // protocols to use when resolving proxy name to endpoint.
	// We cache the IP networks on the device since it is not that cheap to read
	// network interfaces through os syscall.
	// TODO(jhahn): Add monitoring the network interface changes.
	ipNets           []*net.IPNet
	ns               ns.Namespace
	servesMountTable bool

	// TODO(cnicolaou): add roaming stats to ipcStats
	stats *ipcStats // stats for this server.
}

type serverState int

const (
	initialized serverState = iota
	listening
	serving
	publishing
	stopping
	stopped
)

// Simple state machine for the server implementation.
type next map[serverState]bool
type transitions map[serverState]next

var (
	states = transitions{
		initialized: next{listening: true, stopping: true},
		listening:   next{listening: true, serving: true, stopping: true},
		serving:     next{publishing: true, stopping: true},
		publishing:  next{publishing: true, stopping: true},
		stopping:    next{},
		stopped:     next{},
	}

	externalStates = map[serverState]ipc.ServerState{
		initialized: ipc.ServerInit,
		listening:   ipc.ServerActive,
		serving:     ipc.ServerActive,
		publishing:  ipc.ServerActive,
		stopping:    ipc.ServerStopping,
		stopped:     ipc.ServerStopped,
	}
)

func (s *server) allowed(next serverState, method string) error {
	if states[s.state][next] {
		s.state = next
		return nil
	}
	return verror.New(verror.ErrBadState, s.ctx, fmt.Sprintf("%s called out of order or more than once", method))
}

func (s *server) isStopState() bool {
	return s.state == stopping || s.state == stopped
}

var _ ipc.Server = (*server)(nil)

func InternalNewServer(ctx *context.T, streamMgr stream.Manager, ns ns.Namespace, client ipc.Client, opts ...ipc.ServerOpt) (ipc.Server, error) {
	ctx, cancel := context.WithRootCancel(ctx)
	ctx, _ = vtrace.SetNewSpan(ctx, "NewServer")
	statsPrefix := naming.Join("ipc", "server", "routing-id", streamMgr.RoutingID().String())
	s := &server{

		ctx:         ctx,
		cancel:      cancel,
		streamMgr:   streamMgr,
		publisher:   publisher.New(ctx, ns, publishPeriod),
		listenState: make(map[*listenState]struct{}),
		listeners:   make(map[stream.Listener]struct{}),
		proxies:     make(map[string]proxyState),
		stoppedChan: make(chan struct{}),
		ipNets:      ipNetworks(),
		ns:          ns,
		stats:       newIPCStats(statsPrefix),
	}
	var (
		principal             security.Principal
		blessings             security.Blessings
		dischargeExpiryBuffer = vc.DefaultServerDischargeExpiryBuffer
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
			case vc.DischargeExpiryBuffer:
				dischargeExpiryBuffer = time.Duration(opt)
			}
		case options.ServesMountTable:
			s.servesMountTable = bool(opt)
		case ReservedNameDispatcher:
			s.dispReserved = opt.Dispatcher
		case PreferredServerResolveProtocols:
			s.preferredProtocols = []string(opt)
		}
	}
	// Make dischargeExpiryBuffer shorter than the VC discharge buffer to ensure we have fetched
	// the discharges by the time the VC asks for them.`
	dc := InternalNewDischargeClient(ctx, client, dischargeExpiryBuffer-(5*time.Second))
	s.listenerOpts = append(s.listenerOpts, dc)
	s.listenerOpts = append(s.listenerOpts, vc.DialContext{ctx})
	blessingsStatsName := naming.Join(statsPrefix, "security", "blessings")
	// TODO(caprita): revist printing the blessings with %s, and
	// instead expose them as a list.
	stats.NewString(blessingsStatsName).Set(fmt.Sprintf("%s", blessings))
	if principal != nil { // principal should have been passed in, but just in case.
		stats.NewStringFunc(blessingsStatsName, func() string {
			return fmt.Sprintf("%s (default)", principal.BlessingStore().Default())
		})
	}
	return s, nil
}

func (s *server) Status() ipc.ServerStatus {
	status := ipc.ServerStatus{}
	defer vlog.LogCall()()
	s.Lock()
	defer s.Unlock()
	status.State = externalStates[s.state]
	status.ServesMountTable = s.servesMountTable
	status.Mounts = s.publisher.Status()
	status.Endpoints = []naming.Endpoint{}
	for ls, _ := range s.listenState {
		if ls.eperr != nil {
			status.Errors = append(status.Errors, ls.eperr)
		}
		if ls.lnerr != nil {
			status.Errors = append(status.Errors, ls.lnerr)
		}
		for _, iep := range ls.ieps {
			status.Endpoints = append(status.Endpoints, iep)
		}
	}
	status.Proxies = make([]ipc.ProxyStatus, 0, len(s.proxies))
	for k, v := range s.proxies {
		status.Proxies = append(status.Proxies, ipc.ProxyStatus{k, v.endpoint, v.err})
	}
	return status
}

func (s *server) WatchNetwork(ch chan<- ipc.NetworkChange) {
	defer vlog.LogCall()()
	s.Lock()
	defer s.Unlock()
	if s.dhcpState != nil {
		s.dhcpState.watchers[ch] = struct{}{}
	}
}

func (s *server) UnwatchNetwork(ch chan<- ipc.NetworkChange) {
	defer vlog.LogCall()()
	s.Lock()
	defer s.Unlock()
	if s.dhcpState != nil {
		delete(s.dhcpState.watchers, ch)
	}
}

// resolveToEndpoint resolves an object name or address to an endpoint.
func (s *server) resolveToEndpoint(address string) (string, error) {
	var resolved *naming.MountEntry
	var err error
	if s.ns != nil {
		if resolved, err = s.ns.Resolve(s.ctx, address); err != nil {
			return "", err
		}
	} else {
		// Fake a namespace resolution
		resolved = &naming.MountEntry{Servers: []naming.MountedServer{
			{Server: address},
		}}
	}
	// An empty set of protocols means all protocols...
	if resolved.Servers, err = filterAndOrderServers(resolved.Servers, s.preferredProtocols, s.ipNets); err != nil {
		return "", err
	}
	for _, n := range resolved.Names() {
		address, suffix := naming.SplitAddressName(n)
		if suffix != "" {
			continue
		}
		if ep, err := inaming.NewEndpoint(address); err == nil {
			return ep.String(), nil
		}
	}
	return "", fmt.Errorf("unable to resolve %q to an endpoint", address)
}

// getPossbileAddrs returns an appropriate set of addresses that could be used
// to contact the supplied protocol, host, port parameters using the supplied
// chooser function. It returns an indication of whether the supplied address
// was fully specified or not, returning false if the address was fully
// specified, and true if it was not.
func getPossibleAddrs(protocol, host, port string, chooser ipc.AddressChooser) ([]ipc.Address, bool, error) {

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, false, fmt.Errorf("failed to parse %q as an IP host", host)
	}

	addrFromIP := func(ip net.IP) ipc.Address {
		return &netstate.AddrIfc{
			Addr: &net.IPAddr{IP: ip},
		}
	}

	if ip.IsUnspecified() {
		if chooser != nil {
			// Need to find a usable IP address since the call to listen
			// didn't specify one.
			if addrs, err := netstate.GetAccessibleIPs(); err == nil {
				a, err := chooser(protocol, addrs)
				if err == nil && len(a) > 0 {
					return a, true, nil
				}
			}
		}
		// We don't have a chooser, so we just return the address the
		// underlying system has chosen.
		return []ipc.Address{addrFromIP(ip)}, true, nil
	}
	return []ipc.Address{addrFromIP(ip)}, false, nil
}

// createEndpoints creates appropriate inaming.Endpoint instances for
// all of the externally accessible network addresses that can be used
// to reach this server.
func (s *server) createEndpoints(lep naming.Endpoint, chooser ipc.AddressChooser) ([]*inaming.Endpoint, string, bool, error) {
	iep, ok := lep.(*inaming.Endpoint)
	if !ok {
		return nil, "", false, fmt.Errorf("internal type conversion error for %T", lep)
	}
	if !strings.HasPrefix(iep.Protocol, "tcp") &&
		!strings.HasPrefix(iep.Protocol, "ws") {
		// If not tcp, ws, or wsh, just return the endpoint we were given.
		return []*inaming.Endpoint{iep}, "", false, nil
	}

	host, port, err := net.SplitHostPort(iep.Address)
	if err != nil {
		return nil, "", false, err
	}
	addrs, unspecified, err := getPossibleAddrs(iep.Protocol, host, port, chooser)
	if err != nil {
		return nil, port, false, err
	}
	ieps := make([]*inaming.Endpoint, 0, len(addrs))
	for _, addr := range addrs {
		n, err := inaming.NewEndpoint(lep.String())
		if err != nil {
			return nil, port, false, err
		}
		n.IsMountTable = s.servesMountTable
		n.Address = net.JoinHostPort(addr.Address().String(), port)
		ieps = append(ieps, n)
	}
	return ieps, port, unspecified, nil
}

func (s *server) Listen(listenSpec ipc.ListenSpec) ([]naming.Endpoint, error) {
	defer vlog.LogCall()()
	useProxy := len(listenSpec.Proxy) > 0
	if !useProxy && len(listenSpec.Addrs) == 0 {
		return nil, verror.New(verror.ErrBadArg, s.ctx, "ListenSpec contains no proxy or addresses to listen on")
	}

	s.Lock()
	defer s.Unlock()

	if err := s.allowed(listening, "Listen"); err != nil {
		return nil, err
	}

	// Start the proxy as early as possible, ignore duplicate requests
	// for the same proxy.
	if _, inuse := s.proxies[listenSpec.Proxy]; useProxy && !inuse {
		// We have a goroutine for listening on proxy connections.
		s.active.Add(1)
		go func() {
			s.proxyListenLoop(listenSpec.Proxy)
			s.active.Done()
		}()
	}

	roaming := false
	lnState := make([]*listenState, 0, len(listenSpec.Addrs))
	for _, addr := range listenSpec.Addrs {
		if len(addr.Address) > 0 {
			// Listen if we have a local address to listen on.
			ls := &listenState{
				protocol: addr.Protocol,
				address:  addr.Address,
			}
			ls.ln, ls.lep, ls.lnerr = s.streamMgr.Listen(addr.Protocol, addr.Address, s.listenerOpts...)
			lnState = append(lnState, ls)
			if ls.lnerr != nil {
				continue
			}
			ls.ieps, ls.port, ls.roaming, ls.eperr = s.createEndpoints(ls.lep, listenSpec.AddressChooser)
			if ls.roaming && ls.eperr == nil {
				ls.protoIEP = *ls.lep.(*inaming.Endpoint)
				roaming = true
			}
		}
	}

	found := false
	for _, ls := range lnState {
		if ls.ln != nil {
			found = true
			break
		}
	}
	if !found && !useProxy {
		return nil, verror.New(verror.ErrBadArg, s.ctx, "failed to create any listeners")
	}

	if roaming && s.dhcpState == nil && listenSpec.StreamPublisher != nil {
		// Create a dhcp listener if we haven't already done so.
		dhcp := &dhcpState{
			name:      listenSpec.StreamName,
			publisher: listenSpec.StreamPublisher,
			watchers:  make(map[chan<- ipc.NetworkChange]struct{}),
		}
		s.dhcpState = dhcp
		dhcp.ch = make(chan config.Setting, 10)
		dhcp.stream, dhcp.err = dhcp.publisher.ForkStream(dhcp.name, dhcp.ch)
		if dhcp.err == nil {
			// We have a goroutine to listen for dhcp changes.
			s.active.Add(1)
			go func() {
				s.dhcpLoop(dhcp.ch)
				s.active.Done()
			}()
		}
	}

	eps := make([]naming.Endpoint, 0, 10)
	for _, ls := range lnState {
		s.listenState[ls] = struct{}{}
		if ls.ln != nil {
			// We have a goroutine per listener to accept new flows.
			// Each flow is served from its own goroutine.
			s.active.Add(1)
			go func(ln stream.Listener, ep naming.Endpoint) {
				s.listenLoop(ln, ep)
				s.active.Done()
			}(ls.ln, ls.lep)
		}

		for _, iep := range ls.ieps {
			s.publisher.AddServer(iep.String(), s.servesMountTable)
			eps = append(eps, iep)
		}
	}

	return eps, nil
}

func (s *server) reconnectAndPublishProxy(proxy string) (*inaming.Endpoint, stream.Listener, error) {
	resolved, err := s.resolveToEndpoint(proxy)
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
	s.proxies[proxy] = proxyState{iep, nil}
	s.Unlock()
	iep.IsMountTable = s.servesMountTable
	s.publisher.AddServer(iep.String(), s.servesMountTable)
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
	if s.isStopState() {
		s.Unlock()
		return
	}
	s.Unlock()

	for {
		if ln != nil && iep != nil {
			err := s.listenLoop(ln, iep)
			// The listener is done, so:
			// (1) Unpublish its name
			s.publisher.RemoveServer(iep.String())
			s.Lock()
			if err != nil {
				s.proxies[proxy] = proxyState{iep, verror.New(verror.ErrNoServers, s.ctx, err)}
			} else {
				// err will be nill if we're stopping.
				s.proxies[proxy] = proxyState{iep, nil}
				s.Unlock()
				return
			}
			s.Unlock()
		}

		s.Lock()
		if s.isStopState() {
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

// addListener adds the supplied listener taking care to
// check to see if we're already stopping. It returns true
// if the listener was added.
func (s *server) addListener(ln stream.Listener) bool {
	s.Lock()
	defer s.Unlock()
	if s.isStopState() {
		return false
	}
	s.listeners[ln] = struct{}{}
	return true
}

// rmListener removes the supplied listener taking care to
// check if we're already stopping. It returns true if the
// listener was removed.
func (s *server) rmListener(ln stream.Listener) bool {
	s.Lock()
	defer s.Unlock()
	if s.isStopState() {
		return false
	}
	delete(s.listeners, ln)
	return true
}

func (s *server) listenLoop(ln stream.Listener, ep naming.Endpoint) error {
	defer vlog.VI(1).Infof("ipc: Stopped listening on %s", ep)
	var calls sync.WaitGroup

	if !s.addListener(ln) {
		// We're stopping.
		return nil
	}

	defer func() {
		calls.Wait()
		s.rmListener(ln)
	}()
	for {
		flow, err := ln.Accept()
		if err != nil {
			vlog.VI(10).Infof("ipc: Accept on %v failed: %v", ep, err)
			return err
		}
		calls.Add(1)
		go func(flow stream.Flow) {
			defer calls.Done()
			fs, err := newFlowServer(flow, s)
			if err != nil {
				vlog.Errorf("newFlowServer on %v failed: %v", ep, err)
				return
			}
			if err := fs.serve(); err != nil {
				// TODO(caprita): Logging errors here is too spammy. For example, "not
				// authorized" errors shouldn't be logged as server errors.
				// TODO(cnicolaou): revisit this when verror2 transition is
				// done.
				if err != io.EOF {
					vlog.VI(2).Infof("Flow serve on %v failed: %v", ep, err)
				}
			}
		}(flow)
	}
}

func (s *server) dhcpLoop(ch chan config.Setting) {
	defer vlog.VI(1).Infof("ipc: Stopped listen for dhcp changes")
	vlog.VI(2).Infof("ipc: dhcp loop")
	for setting := range ch {
		if setting == nil {
			return
		}
		switch v := setting.Value().(type) {
		case []ipc.Address:
			s.Lock()
			if s.isStopState() {
				s.Unlock()
				return
			}
			var err error
			var changed []naming.Endpoint
			switch setting.Name() {
			case ipc.NewAddrsSetting:
				changed = s.addAddresses(v)
			case ipc.RmAddrsSetting:
				changed, err = s.removeAddresses(v)
			}
			change := ipc.NetworkChange{
				Time:    time.Now(),
				State:   externalStates[s.state],
				Setting: setting,
				Changed: changed,
				Error:   err,
			}
			vlog.VI(2).Infof("ipc: dhcp: change %v", change)
			for ch, _ := range s.dhcpState.watchers {
				select {
				case ch <- change:
				default:
				}
			}
			s.Unlock()
		default:
			vlog.Errorf("ipc: dhcpLoop: unhandled setting type %T", v)
		}
	}
}

func getHost(address ipc.Address) string {
	host, _, err := net.SplitHostPort(address.Address().String())
	if err == nil {
		return host
	}
	return address.Address().String()

}

// Remove all endpoints that have the same host address as the supplied
// address parameter.
func (s *server) removeAddresses(addresses []ipc.Address) ([]naming.Endpoint, error) {
	var removed []naming.Endpoint
	for _, address := range addresses {
		host := getHost(address)
		for ls, _ := range s.listenState {
			if ls != nil && ls.roaming && len(ls.ieps) > 0 {
				remaining := make([]*inaming.Endpoint, 0, len(ls.ieps))
				for _, iep := range ls.ieps {
					lnHost, _, err := net.SplitHostPort(iep.Address)
					if err != nil {
						lnHost = iep.Address
					}
					if lnHost == host {
						vlog.VI(2).Infof("ipc: dhcp removing: %s", iep)
						removed = append(removed, iep)
						s.publisher.RemoveServer(iep.String())
						continue
					}
					remaining = append(remaining, iep)
				}
				ls.ieps = remaining
			}
		}
	}
	return removed, nil
}

// Add new endpoints for the new address. There is no way to know with
// 100% confidence which new endpoints to publish without shutting down
// all network connections and reinitializing everything from scratch.
// Instead, we find all roaming listeners with at least one endpoint
// and create a new endpoint with the same port as the existing ones
// but with the new address supplied to us to by the dhcp code. As
// an additional safeguard we reject the new address if it is not
// externally accessible.
// This places the onus on the dhcp/roaming code that sends us addresses
// to ensure that those addresses are externally reachable.
func (s *server) addAddresses(addresses []ipc.Address) []naming.Endpoint {
	var added []naming.Endpoint
	for _, address := range addresses {
		if !netstate.IsAccessibleIP(address) {
			return added
		}
		host := getHost(address)
		for ls, _ := range s.listenState {
			if ls != nil && ls.roaming {
				niep := ls.protoIEP
				niep.Address = net.JoinHostPort(host, ls.port)
				ls.ieps = append(ls.ieps, &niep)
				vlog.VI(2).Infof("ipc: dhcp adding: %s", niep)
				s.publisher.AddServer(niep.String(), s.servesMountTable)
				added = append(added, &niep)
			}
		}
	}
	return added
}

type leafDispatcher struct {
	invoker ipc.Invoker
	auth    security.Authorizer
}

func (d leafDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if suffix != "" {
		return nil, nil, ipc.NewErrUnknownSuffix(nil, suffix)
	}
	return d.invoker, d.auth, nil
}

func (s *server) Serve(name string, obj interface{}, authorizer security.Authorizer) error {
	defer vlog.LogCall()()
	if obj == nil {
		return verror.New(verror.ErrBadArg, s.ctx, "nil object")
	}
	invoker, err := objectToInvoker(obj)
	if err != nil {
		return verror.New(verror.ErrBadArg, s.ctx, fmt.Sprintf("bad object: %v", err))
	}
	return s.ServeDispatcher(name, &leafDispatcher{invoker, authorizer})
}

func (s *server) ServeDispatcher(name string, disp ipc.Dispatcher) error {
	defer vlog.LogCall()()
	if disp == nil {
		return verror.New(verror.ErrBadArg, s.ctx, "nil dispatcher")
	}
	s.Lock()
	defer s.Unlock()
	if err := s.allowed(serving, "Serve or ServeDispatcher"); err != nil {
		return err
	}
	vtrace.GetSpan(s.ctx).Annotate("Serving under name: " + name)
	s.disp = disp
	if len(name) > 0 {
		s.publisher.AddName(name)
	}
	return nil
}

func (s *server) AddName(name string) error {
	defer vlog.LogCall()()
	if len(name) == 0 {
		return verror.New(verror.ErrBadArg, s.ctx, "name is empty")
	}
	s.Lock()
	defer s.Unlock()
	if err := s.allowed(publishing, "AddName"); err != nil {
		return err
	}
	vtrace.GetSpan(s.ctx).Annotate("Serving under name: " + name)
	s.publisher.AddName(name)
	return nil
}

func (s *server) RemoveName(name string) {
	defer vlog.LogCall()()
	s.Lock()
	defer s.Unlock()
	if err := s.allowed(publishing, "RemoveName"); err != nil {
		return
	}
	vtrace.GetSpan(s.ctx).Annotate("Removed name: " + name)
	s.publisher.RemoveName(name)
}

func (s *server) Stop() error {
	defer vlog.LogCall()()
	s.Lock()
	if s.isStopState() {
		s.Unlock()
		return nil
	}
	s.state = stopping
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

	for ln, _ := range s.listeners {
		go func(ln stream.Listener) {
			errCh <- ln.Close()
		}(ln)
	}

	drain := func(ch chan config.Setting) {
		for {
			select {
			case v := <-ch:
				if v == nil {
					return
				}
			default:
				close(ch)
				return
			}
		}
	}

	if dhcp := s.dhcpState; dhcp != nil {
		// TODO(cnicolaou,caprita): investigate not having to close and drain
		// the channel here. It's a little awkward right now since we have to
		// be careful to not close the channel in two places, i.e. here and
		// and from the publisher's Shutdown method.
		if err := dhcp.publisher.CloseFork(dhcp.name, dhcp.ch); err == nil {
			drain(dhcp.ch)
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
	done := make(chan struct{}, 1)
	go func() { s.active.Wait(); done <- struct{}{} }()

	select {
	case <-done:
	case <-time.After(5 * time.Minute):
		vlog.Errorf("Listener Close Error: %v", firstErr)
		vlog.Errorf("Timedout waiting for goroutines to stop: listeners: %d", nListeners, len(s.listeners))
		for ln, _ := range s.listeners {
			vlog.Errorf("Listener: %p", ln)
		}
		for ls, _ := range s.listenState {
			vlog.Errorf("ListenState: %v", ls)
		}
		<-done
	}

	s.Lock()
	defer s.Unlock()
	s.disp = nil
	if firstErr != nil {
		return verror.New(verror.ErrInternal, s.ctx, firstErr)
	}
	s.state = stopped
	s.cancel()
	return nil
}

// flowServer implements the RPC server-side protocol for a single RPC, over a
// flow that's already connected to the client.
type flowServer struct {
	*context.T
	server *server        // ipc.Server that this flow server belongs to
	disp   ipc.Dispatcher // ipc.Dispatcher that will serve RPCs on this flow
	dec    *vom.Decoder   // to decode requests and args from the client
	enc    *vom.Encoder   // to encode responses and results to the client
	flow   stream.Flow    // underlying flow

	// Fields filled in during the server invocation.
	clientBlessings  security.Blessings
	ackBlessings     bool
	grantedBlessings security.Blessings
	method, suffix   string
	tags             []*vdl.Value
	discharges       map[string]security.Discharge
	starttime        time.Time
	endStreamArgs    bool // are the stream args at EOF?
	allowDebug       bool // true if the caller is permitted to view debug information.
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
	var err error
	if fs.dec, err = vom.NewDecoder(flow); err != nil {
		flow.Close()
		return nil, err
	}
	if fs.enc, err = vom.NewEncoder(flow); err != nil {
		flow.Close()
		return nil, err
	}
	return fs, nil
}

func (fs *flowServer) serve() error {
	defer fs.flow.Close()

	results, err := fs.processRequest()

	vtrace.GetSpan(fs.T).Finish()

	var traceResponse vtrace.Response
	if fs.allowDebug {
		traceResponse = ivtrace.Response(fs.T)
	}

	// Respond to the client with the response header and positional results.
	response := ipc.Response{
		Error:            err,
		EndStreamResults: true,
		NumPosResults:    uint64(len(results)),
		TraceResponse:    traceResponse,
		AckBlessings:     fs.ackBlessings,
	}
	if err := fs.enc.Encode(response); err != nil {
		if err == io.EOF {
			return err
		}
		return fmt.Errorf("ipc: response encoding failed: %v", err)
	}
	if response.Error != nil {
		return response.Error
	}
	for ix, res := range results {
		if err := fs.enc.Encode(res); err != nil {
			if err == io.EOF {
				return err
			}
			return fmt.Errorf("ipc: result #%d [%T=%v] encoding failed: %v", ix, res, res, err)
		}
	}
	// TODO(ashankar): Should unread data from the flow be drained?
	//
	// Reason to do so:
	// The common stream.Flow implementation (veyron/profiles/internal/ipc/stream/vc/reader.go)
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

func (fs *flowServer) readIPCRequest() (*ipc.Request, error) {
	// Set a default timeout before reading from the flow. Without this timeout,
	// a client that sends no request or a partial request will retain the flow
	// indefinitely (and lock up server resources).
	initTimer := newTimer(defaultCallTimeout)
	defer initTimer.Stop()
	fs.flow.SetDeadline(initTimer.C)

	// Decode the initial request.
	var req ipc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return nil, verror.New(verror.ErrBadProtocol, fs.T, newErrBadRequest(fs.T, err))
	}
	return &req, nil
}

func (fs *flowServer) processRequest() ([]interface{}, error) {
	fs.starttime = time.Now()
	req, err := fs.readIPCRequest()
	if err != nil {
		// We don't know what the ipc call was supposed to be, but we'll create
		// a placeholder span so we can capture annotations.
		fs.T, _ = vtrace.SetNewSpan(fs.T, fmt.Sprintf("\"%s\".UNKNOWN", fs.Name()))
		return nil, err
	}
	fs.method = req.Method
	fs.suffix = strings.TrimLeft(req.Suffix, "/")

	// TODO(mattr): Currently this allows users to trigger trace collection
	// on the server even if they will not be allowed to collect the
	// results later.  This might be considered a DOS vector.
	spanName := fmt.Sprintf("\"%s\".%s", fs.Name(), fs.Method())
	fs.T, _ = vtrace.SetContinuedTrace(fs.T, spanName, req.TraceRequest)

	var cancel context.CancelFunc
	if !req.Deadline.IsZero() {
		fs.T, cancel = context.WithDeadline(fs.T, req.Deadline.Time)
	} else {
		fs.T, cancel = context.WithCancel(fs.T)
	}
	fs.flow.SetDeadline(fs.Done())
	go fs.cancelContextOnClose(cancel)

	// Initialize security: blessings, discharges, etc.
	if err := fs.initSecurity(req); err != nil {
		return nil, err
	}
	// Lookup the invoker.
	invoker, auth, err := fs.lookup(fs.suffix, &fs.method)
	if err != nil {
		return nil, err
	}
	// Prepare invoker and decode args.
	numArgs := int(req.NumPosArgs)
	argptrs, tags, err := invoker.Prepare(fs.method, numArgs)
	fs.tags = tags
	if err != nil {
		return nil, err
	}
	if called, want := req.NumPosArgs, uint64(len(argptrs)); called != want {
		return nil, verror.New(verror.ErrBadProtocol, fs.T, newErrBadNumInputArgs(fs.T, fs.suffix, fs.method, called, want))
	}
	for ix, argptr := range argptrs {
		if err := fs.dec.Decode(argptr); err != nil {
			return nil, verror.New(verror.ErrBadProtocol, fs.T, newErrBadInputArg(fs.T, fs.suffix, fs.method, uint64(ix), err))
		}
	}
	// Check application's authorization policy.
	if err := authorize(fs, auth); err != nil {
		return nil, err
	}
	// Check if the caller is permitted to view debug information.
	// TODO(mattr): Is access.Debug the right thing to check?
	fs.allowDebug = authorize(debugContext{fs}, auth) == nil
	// Invoke the method.
	results, err := invoker.Invoke(fs.method, fs, argptrs)
	fs.server.stats.record(fs.method, time.Since(fs.starttime))
	return results, err
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
func (fs *flowServer) lookup(suffix string, method *string) (ipc.Invoker, security.Authorizer, error) {
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
			return nil, nil, err
		case obj != nil:
			invoker, err := objectToInvoker(obj)
			if err != nil {
				return nil, nil, verror.New(verror.ErrInternal, fs.T, "invalid received object", err)
			}
			return invoker, auth, nil
		}
	}
	return nil, nil, ipc.NewErrUnknownSuffix(nil, suffix)
}

func objectToInvoker(obj interface{}) (ipc.Invoker, error) {
	if obj == nil {
		return nil, errors.New("nil object")
	}
	if invoker, ok := obj.(ipc.Invoker); ok {
		return invoker, nil
	}
	return ipc.ReflectInvoker(obj)
}

func (fs *flowServer) initSecurity(req *ipc.Request) error {
	// LocalPrincipal is nil which means we are operating under
	// VCSecurityNone.
	if fs.flow.LocalPrincipal() == nil {
		return nil
	}

	// If additional credentials are provided, make them available in the context
	// Detect unusable blessings now, rather then discovering they are unusable on
	// first use.
	//
	// TODO(ashankar,ataly): Potential confused deputy attack: The client provides
	// the server's identity as the blessing. Figure out what we want to do about
	// this - should servers be able to assume that a blessing is something that
	// does not have the authorizations that the server's own identity has?
	if got, want := req.GrantedBlessings.PublicKey(), fs.flow.LocalPrincipal().PublicKey(); got != nil && !reflect.DeepEqual(got, want) {
		return verror.New(verror.ErrNoAccess, fs.T, fmt.Sprintf("blessing granted not bound to this server(%v vs %v)", got, want))
	}
	fs.grantedBlessings = req.GrantedBlessings

	var err error
	if fs.clientBlessings, err = serverDecodeBlessings(fs.flow.VCDataCache(), req.Blessings, fs.server.stats); err != nil {
		// When the server can't access the blessings cache, the client is not following
		// protocol, so the server closes the VCs corresponding to the client endpoint.
		// TODO(suharshs,toddw): Figure out a way to only shutdown the current VC, instead
		// of all VCs connected to the RemoteEndpoint.
		fs.server.streamMgr.ShutdownEndpoint(fs.RemoteEndpoint())
		return verror.New(verror.ErrBadProtocol, fs.T, newErrBadBlessingsCache(fs.T, err))
	}
	// Verify that the blessings sent by the client in the request have the same public
	// key as those sent by the client during VC establishment.
	if got, want := fs.clientBlessings.PublicKey(), fs.flow.RemoteBlessings().PublicKey(); got != nil && !reflect.DeepEqual(got, want) {
		return verror.New(verror.ErrNoAccess, fs.T, fmt.Sprintf("blessings sent with the request are bound to a different public key (%v) from the blessing used during VC establishment (%v)", got, want))
	}
	fs.ackBlessings = true

	for _, d := range req.Discharges {
		fs.discharges[d.ID()] = d
	}
	return nil
}

type acceptAllAuthorizer struct{}

func (acceptAllAuthorizer) Authorize(security.Call) error {
	return nil
}

func authorize(call ipc.ServerCall, auth security.Authorizer) error {
	if call.LocalPrincipal() == nil {
		// LocalPrincipal is nil means that the server wanted to avoid
		// authentication, and thus wanted to skip authorization as well.
		return nil
	}
	if auth == nil {
		auth = defaultAuthorizer{}
	}
	if err := auth.Authorize(call); err != nil {
		// TODO(ataly, ashankar): For privacy reasons, should we hide the authorizer error?
		return verror.New(verror.ErrNoAccess, call.Context(), newErrBadAuth(call.Context(), call.Suffix(), call.Method(), err))
	}
	return nil
}

// debugContext is a context which wraps another context but always returns
// the debug tag.
type debugContext struct {
	ipc.ServerCall
}

func (debugContext) MethodTags() []*vdl.Value {
	return []*vdl.Value{vdl.ValueOf(access.Debug)}
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

// Implementations of ipc.ServerCall methods.

func (fs *flowServer) LocalDischarges() map[string]security.Discharge {
	//nologcall
	return fs.flow.LocalDischarges()
}
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
func (fs *flowServer) MethodTags() []*vdl.Value {
	//nologcall
	return fs.tags
}
func (fs *flowServer) Context() *context.T {
	return fs.T
}

func (fs *flowServer) VanadiumContext() *context.T {
	return fs.T
}

// TODO(cnicolaou): remove Name from ipc.ServerCall and all of
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
	if !fs.clientBlessings.IsZero() {
		return fs.clientBlessings
	}
	return fs.flow.RemoteBlessings()
}
func (fs *flowServer) GrantedBlessings() security.Blessings {
	//nologcall
	return fs.grantedBlessings
}
func (fs *flowServer) LocalEndpoint() naming.Endpoint {
	//nologcall
	return fs.flow.LocalEndpoint()
}
func (fs *flowServer) RemoteEndpoint() naming.Endpoint {
	//nologcall
	return fs.flow.RemoteEndpoint()
}
