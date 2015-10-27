// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"v.io/x/lib/netstate"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"

	"v.io/x/ref/lib/apilog"
	"v.io/x/ref/lib/pubsub"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/runtime/internal/lib/publisher"
	"v.io/x/ref/runtime/internal/lib/roaming"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
)

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errResponseEncoding          = reg(".errResponseEncoding", "failed to encode RPC response {3} <-> {4}{:5}")
	errResultEncoding            = reg(".errResultEncoding", "failed to encode result #{3} [{4}]{:5}")
	errFailedToResolveToEndpoint = reg(".errFailedToResolveToEndpoint", "failed to resolve {3} to an endpoint")
	errFailedToResolveProxy      = reg(".errFailedToResolveProxy", "failed to resolve proxy {3}{:4}")
	errFailedToListenForProxy    = reg(".errFailedToListenForProxy", "failed to listen on {3}{:4}")
	errInternalTypeConversion    = reg(".errInternalTypeConversion", "failed to convert {3} to v.io/x/ref/runtime/internal/naming.Endpoint")
	errFailedToParseIP           = reg(".errFailedToParseIP", "failed to parse {3} as an IP host")
	errUnexpectedSuffix          = reg(".errUnexpectedSuffix", "suffix {3} was not expected because either server has the option IsLeaf set to true or it served an object and not a dispatcher")
	errNoListeners               = reg(".errNoListeners", "failed to ceate any listeners{:3}")
	errBlessingsNotBound         = reg(".errBlessingNotBound", "blessing granted not bound to this server({3} vs {4})")
)

type DeprecatedServer interface {
	// Listen creates a listening network endpoint for the Server
	// as specified by its ListenSpec parameter. If any of the listen
	// addresses passed in the ListenSpec are 'unspecified' (e.g. don't
	// include a fixed address such as in ":0") and the ListenSpec includes
	// a Publisher, then 'roaming' support will be enabled. In this mode
	// the server will listen for changes in the network configuration
	// using a Stream created on the supplied Publisher and change the
	// set of Endpoints it publishes to the mount table accordingly.
	// The set of expected Settings received over the Stream is defined
	// by the New<setting>Functions above. The Publisher is ignored if
	// all of the addresses are specified.
	//
	// Listen may be called multiple times, but it must be called before
	// Serve or ServeDispatcher.
	//
	// Listen returns the set of endpoints that can be used to reach
	// this server. A single listen address in the ListenSpec can lead
	// to multiple such endpoints (e.g. :0 on a device with multiple interfaces
	// or that is being proxied). In the case where multiple listen addresses
	// are used it is not possible to tell which listen address supports which
	// Endpoint. If there is need to associate endpoints with specific
	// listen addresses then Listen should be called separately for each one.
	//
	// Any non-nil value of error can be converted to a verror.E.  If
	// error is nil and at least one address was supplied in the ListenSpec
	// then ListenEndpoints will include at least one Endpoint.
	Listen(spec rpc.ListenSpec) ([]naming.Endpoint, error)

	// Serve associates object with name by publishing the address of this
	// server with the mount table under the supplied name and using
	// authorizer to authorize access to it. RPCs invoked on the supplied
	// name will be delivered to methods implemented by the supplied object.
	//
	// Reflection is used to match requests to the object's method set.  As
	// a special-case, if the object implements the Invoker interface, the
	// Invoker is used to invoke methods directly, without reflection.
	//
	// If name is an empty string, no attempt will made to publish that
	// name to a mount table.
	//
	// It is an error to call Serve if ServeDispatcher has already been
	// called. It is also an error to call Serve multiple times.
	// It is considered an error to call Listen after Serve.
	Serve(name string, object interface{}, auth security.Authorizer) error

	// ServeDispatcher associates dispatcher with the portion of the mount
	// table's name space for which name is a prefix, by publishing the
	// address of this dispatcher with the mount table under the supplied
	// name.
	//
	// If name is an empty string, no attempt will made to publish that name
	// to a mount table.
	//
	// RPCs invoked on the supplied name will be delivered to the supplied
	// Dispatcher's Lookup method which will in turn return the object
	// and security.Authorizer used to serve the actual RPC call.
	// If name is an empty string, no attempt will made to publish that
	// name to a mount table.
	//
	// It is an error to call ServeDispatcher if Serve has already been
	// called. It is also an error to call ServeDispatcher multiple times.
	// It is considered an error to call Listen after ServeDispatcher.
	ServeDispatcher(name string, disp rpc.Dispatcher) error

	rpc.Server
}

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
	publisher *pubsub.Publisher
	stream    *pubsub.Stream
	ch        chan pubsub.Setting // channel to receive dhcp settings over
	err       error               // error status.
	watchers  map[chan<- NetworkChange]struct{}
	change    chan struct{}
}

type NetworkChange struct {
	Time         time.Time         // Time of last change.
	State        rpc.ServerState   // The current state of the server.
	AddedAddrs   []net.Addr        // Addresses added sinced the last change.
	RemovedAddrs []net.Addr        // Addresses removed since the last change.
	Changed      []naming.Endpoint // The set of endpoints added/removed as a result of this change.
	Error        error             // Any error that encountered.
}

type server struct {
	sync.Mutex
	// context used by the server to make internal RPCs, error messages etc.
	ctx               *context.T
	cancel            context.CancelFunc   // function to cancel the above context.
	state             serverState          // track state of the server.
	streamMgr         stream.Manager       // stream manager to listen for new flows.
	publisher         publisher.Publisher  // publisher to publish mounttable mounts.
	dc                vc.DischargeClient   // fetches discharges of blessings
	listenerOpts      []stream.ListenerOpt // listener opts for Listen.
	settingsPublisher *pubsub.Publisher    // pubsub publisher for dhcp
	settingsName      string               // pubwsub stream name for dhcp
	dhcpState         *dhcpState           // dhcpState, nil if not using dhcp
	principal         security.Principal
	blessings         security.Blessings

	// maps that contain state on listeners.
	listenState map[*listenState]struct{}
	listeners   map[stream.Listener]struct{}

	// state of proxies keyed by the name of the proxy
	proxies map[string]proxyState

	disp               rpc.Dispatcher // dispatcher to serve RPCs
	dispReserved       rpc.Dispatcher // dispatcher for reserved methods
	active             sync.WaitGroup // active goroutines we've spawned.
	stoppedChan        chan struct{}  // closed when the server has been stopped.
	preferredProtocols []string       // protocols to use when resolving proxy name to endpoint.
	// We cache the IP networks on the device since it is not that cheap to read
	// network interfaces through os syscall.
	// TODO(jhahn): Add monitoring the network interface changes.
	ns               namespace.T
	servesMountTable bool
	isLeaf           bool

	// TODO(cnicolaou): add roaming stats to rpcStats
	stats *rpcStats // stats for this server.
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

	externalStates = map[serverState]rpc.ServerState{
		initialized: rpc.ServerActive,
		listening:   rpc.ServerActive,
		serving:     rpc.ServerActive,
		publishing:  rpc.ServerActive,
		stopping:    rpc.ServerStopping,
		stopped:     rpc.ServerStopped,
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

var _ DeprecatedServer = (*server)(nil)

func DeprecatedNewServer(
	ctx *context.T,
	streamMgr stream.Manager,
	ns namespace.T,
	settingsPublisher *pubsub.Publisher,
	settingsName string,
	client rpc.Client,
	opts ...rpc.ServerOpt) (DeprecatedServer, error) {
	ctx, cancel := context.WithRootCancel(ctx)
	ctx, _ = vtrace.WithNewSpan(ctx, "NewServer")
	statsPrefix := naming.Join("rpc", "server", "routing-id", streamMgr.RoutingID().String())
	s := &server{
		ctx:         ctx,
		cancel:      cancel,
		streamMgr:   streamMgr,
		publisher:   publisher.New(ctx, ns, publishPeriod),
		listenState: make(map[*listenState]struct{}),
		listeners:   make(map[stream.Listener]struct{}),
		proxies:     make(map[string]proxyState),
		stoppedChan: make(chan struct{}),

		ns:                ns,
		stats:             newRPCStats(statsPrefix),
		settingsPublisher: settingsPublisher,
		settingsName:      settingsName,
	}
	var (
		dischargeExpiryBuffer = vc.DefaultServerDischargeExpiryBuffer
		securityLevel         options.SecurityLevel
	)

	for _, opt := range opts {
		switch opt := opt.(type) {
		case stream.ListenerOpt:
			// Collect all ServerOpts that are also ListenerOpts.
			s.listenerOpts = append(s.listenerOpts, opt)
			switch opt := opt.(type) {
			case vc.DischargeExpiryBuffer:
				dischargeExpiryBuffer = time.Duration(opt)
			}
		case options.ServerBlessings:
			s.blessings = opt.Blessings
		case options.ServesMountTable:
			s.servesMountTable = bool(opt)
		case options.IsLeaf:
			s.isLeaf = bool(opt)
		case ReservedNameDispatcher:
			s.dispReserved = opt.Dispatcher
		case PreferredServerResolveProtocols:
			s.preferredProtocols = []string(opt)
		case options.SecurityLevel:
			securityLevel = opt
		case options.ChannelTimeout:
			s.listenerOpts = append(s.listenerOpts,
				vc.ChannelTimeout(time.Duration(opt)))
		}
	}

	authenticateVC := true

	if securityLevel == options.SecurityNone {
		authenticateVC = false
		s.blessings = security.Blessings{}
		s.dispReserved = nil
	}
	if authenticateVC {
		s.principal = v23.GetPrincipal(ctx)
		if s.blessings.IsZero() && s.principal != nil {
			s.blessings = s.principal.BlessingStore().Default()
		}
	}

	// Make dischargeExpiryBuffer shorter than the VC discharge buffer to ensure we have fetched
	// the discharges by the time the VC asks for them.
	s.dc = InternalNewDischargeClient(ctx, client, dischargeExpiryBuffer-(5*time.Second))
	s.listenerOpts = append(s.listenerOpts, s.dc)
	s.listenerOpts = append(s.listenerOpts, stream.AuthenticatedVC(authenticateVC))
	blessingsStatsName := naming.Join(statsPrefix, "security", "blessings")
	// TODO(caprita): revist printing the blessings with %s, and
	// instead expose them as a list.
	stats.NewString(blessingsStatsName).Set(fmt.Sprintf("%s", s.blessings))
	if s.principal != nil {
		stats.NewStringFunc(blessingsStatsName, func() string {
			return fmt.Sprintf("%s (default)", s.principal.BlessingStore().Default())
		})
	}
	return s, nil
}

func (s *server) Status() rpc.ServerStatus {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	status := rpc.ServerStatus{}
	s.Lock()
	defer s.Unlock()
	status.State = externalStates[s.state]
	status.ServesMountTable = s.servesMountTable
	status.Mounts = s.publisher.Status()
	status.Endpoints = []naming.Endpoint{}
	if s.dhcpState != nil {
		status.Valid = s.dhcpState.change
	}
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
	status.Proxies = make([]rpc.ProxyStatus, 0, len(s.proxies))
	for k, v := range s.proxies {
		status.Proxies = append(status.Proxies, rpc.ProxyStatus{
			Proxy:    k,
			Endpoint: v.endpoint,
			Error:    v.err,
		})
	}
	return status
}

func (s *server) WatchNetwork(ch chan<- NetworkChange) {
	defer apilog.LogCallf(nil, "ch=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	s.Lock()
	defer s.Unlock()
	if s.dhcpState != nil {
		s.dhcpState.watchers[ch] = struct{}{}
	}
}

func (s *server) UnwatchNetwork(ch chan<- NetworkChange) {
	defer apilog.LogCallf(nil, "ch=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
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
	if resolved.Servers, err = filterAndOrderServers(resolved.Servers, s.preferredProtocols); err != nil {
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
	return "", verror.New(errFailedToResolveToEndpoint, s.ctx, address)
}

// createEndpoints creates appropriate inaming.Endpoint instances for
// all of the externally accessible network addresses that can be used
// to reach this server.
func (s *server) createEndpoints(lep naming.Endpoint, chooser netstate.AddressChooser) ([]*inaming.Endpoint, string, bool, error) {
	iep, ok := lep.(*inaming.Endpoint)
	if !ok {
		return nil, "", false, verror.New(errInternalTypeConversion, nil, fmt.Sprintf("%T", lep))
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
	addrs, unspecified, err := netstate.PossibleAddresses(iep.Protocol, host, chooser)
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
		n.Address = net.JoinHostPort(addr.String(), port)
		ieps = append(ieps, n)
	}
	return ieps, port, unspecified, nil
}

func (s *server) Listen(listenSpec rpc.ListenSpec) ([]naming.Endpoint, error) {
	defer apilog.LogCallf(nil, "listenSpec=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
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
		// Pre-emptively fetch discharges on the blessings (they will be cached
		// within s.dc for future calls).
		// This shouldn't be required, but is a hack to reduce flakiness in
		// JavaScript browser integration tests.
		// See https://v.io/i/392
		s.dc.PrepareDischarges(s.ctx, s.blessings.ThirdPartyCaveats(), security.DischargeImpetus{})
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
			ls.ln, ls.lep, ls.lnerr = s.streamMgr.Listen(s.ctx, addr.Protocol, addr.Address, s.blessings, s.listenerOpts...)
			lnState = append(lnState, ls)
			if ls.lnerr != nil {
				s.ctx.VI(2).Infof("Listen(%q, %q, ...) failed: %v", addr.Protocol, addr.Address, ls.lnerr)
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
	var lastErr error
	for _, ls := range lnState {
		if ls.ln != nil {
			found = true
			break
		}
		if ls.lnerr != nil {
			lastErr = ls.lnerr
		}
	}
	if !found && !useProxy {
		return nil, verror.New(verror.ErrBadArg, s.ctx, verror.New(errNoListeners, s.ctx, lastErr))
	}

	if roaming && s.dhcpState == nil && s.settingsPublisher != nil {
		// Create a dhcp listener if we haven't already done so.
		dhcp := &dhcpState{
			name:      s.settingsName,
			publisher: s.settingsPublisher,
			watchers:  make(map[chan<- NetworkChange]struct{}),
			change:    make(chan struct{}),
		}
		s.dhcpState = dhcp
		dhcp.ch = make(chan pubsub.Setting, 10)
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
			eps = append(eps, iep)
		}
	}

	return eps, nil
}

func (s *server) reconnectAndPublishProxy(proxy string) (*inaming.Endpoint, stream.Listener, error) {
	resolved, err := s.resolveToEndpoint(proxy)
	if err != nil {
		return nil, nil, verror.New(errFailedToResolveProxy, s.ctx, proxy, err)
	}
	opts := append([]stream.ListenerOpt{proxyAuth{s}}, s.listenerOpts...)
	ln, ep, err := s.streamMgr.Listen(s.ctx, inaming.Network, resolved, s.blessings, opts...)
	if err != nil {
		return nil, nil, verror.New(errFailedToListenForProxy, s.ctx, resolved, err)
	}
	iep, ok := ep.(*inaming.Endpoint)
	if !ok {
		ln.Close()
		return nil, nil, verror.New(errInternalTypeConversion, s.ctx, fmt.Sprintf("%T", ep))
	}
	s.Lock()
	s.proxies[proxy] = proxyState{iep, nil}
	s.Unlock()
	iep.IsMountTable = s.servesMountTable
	iep.IsLeaf = s.isLeaf
	s.publisher.AddServer(iep.String())
	return iep, ln, nil
}

func (s *server) proxyListenLoop(proxy string) {
	const (
		min = 5 * time.Millisecond
		max = 5 * time.Minute
	)

	iep, ln, err := s.reconnectAndPublishProxy(proxy)
	if err != nil {
		s.ctx.Errorf("Failed to connect to proxy: %s", err)
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
				// err will be nil if we're stopping.
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
				s.ctx.Errorf("Failed to reconnect to proxy %q: %s", proxy, err)
			} else {
				s.ctx.VI(1).Infof("Reconnected to proxy %q, %s", proxy, iep)
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
	defer s.ctx.VI(1).Infof("rpc: Stopped listening on %s", ep)
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
			s.ctx.VI(10).Infof("rpc: Accept on %v failed: %v", ep, err)
			return err
		}
		calls.Add(1)
		go func(flow stream.Flow) {
			defer calls.Done()
			fs, err := newFlowServer(flow, s)
			if err != nil {
				s.ctx.VI(1).Infof("newFlowServer on %v failed: %v", ep, err)
				return
			}
			if err := fs.serve(); err != nil {
				// TODO(caprita): Logging errors here is too spammy. For example, "not
				// authorized" errors shouldn't be logged as server errors.
				// TODO(cnicolaou): revisit this when verror2 transition is
				// done.
				if err != io.EOF {
					s.ctx.VI(2).Infof("Flow.serve on %v failed: %v", ep, err)
				}
			}
		}(flow)
	}
}

func (s *server) dhcpLoop(ch chan pubsub.Setting) {
	defer s.ctx.VI(1).Infof("rpc: Stopped listen for dhcp changes")
	s.ctx.VI(2).Infof("rpc: dhcp loop")
	for setting := range ch {
		if setting == nil {
			return
		}
		switch v := setting.Value().(type) {
		case []net.Addr:
			s.Lock()
			if s.isStopState() {
				s.Unlock()
				return
			}
			change := NetworkChange{
				Time:  time.Now(),
				State: externalStates[s.state],
			}
			switch setting.Name() {
			case roaming.UpdateAddrsSetting:
				change.AddedAddrs, change.Changed = s.updateAddresses(v)
			case roaming.RmAddrsSetting:
				change.RemovedAddrs, change.Changed = s.removeAddresses(v)
			}
			s.ctx.VI(2).Infof("rpc: dhcp: change %v", change)
			for ch, _ := range s.dhcpState.watchers {
				select {
				case ch <- change:
				default:
				}
			}
			if s.dhcpState.change != nil {
				close(s.dhcpState.change)
				s.dhcpState.change = make(chan struct{})
			}
			s.Unlock()
		default:
			s.ctx.Errorf("rpc: dhcpLoop: unhandled setting type %T", v)
		}
	}
}

func getHost(address net.Addr) string {
	host, _, err := net.SplitHostPort(address.String())
	if err == nil {
		return host
	}
	return address.String()
}

// findEndpoint returns the index of the first endpoint in ieps with the given network address.
func findEndpoint(ieps []*inaming.Endpoint, host string) int {
	for i, iep := range ieps {
		if getHost(iep.Addr()) == host {
			return i
		}
	}
	return -1
}

// removeAddresses removes all endpoints that have the same host address as
// the supplied address parameter.
func (s *server) removeAddresses(addrs []net.Addr) ([]net.Addr, []naming.Endpoint) {
	var removedAddrs []net.Addr
	var removedEps []naming.Endpoint
	for _, addr := range addrs {
		host := getHost(addr)
		removed := false
		for ls, _ := range s.listenState {
			if ls == nil || !ls.roaming {
				continue
			}
			if i := findEndpoint(ls.ieps, host); i >= 0 {
				oiep := ls.ieps[i]
				s.ctx.VI(2).Infof("rpc: dhcp removing: %s", oiep)
				n := len(ls.ieps) - 1
				ls.ieps[i], ls.ieps[n] = ls.ieps[n], nil
				ls.ieps = ls.ieps[:n]
				s.publisher.RemoveServer(oiep.String())
				removedEps = append(removedEps, oiep)
				removed = true
			}
		}
		if removed {
			removedAddrs = append(removedAddrs, addr)
		}
	}
	return removedAddrs, removedEps
}

// updateAddresses updates endpoints with the given addresses. There is no way to
// know with 100% confidence which new endpoints to publish without shutting down
// all network connections and reinitializing everything from scratch. Instead, we
// find all roaming listeners with at least one endpoint and create a new endpoint
// with the same port as the existing ones but with the new address supplied to us
// to by the dhcp code. As an additional safeguard we reject the new address if it
// is not externally accessible.
//
// This places the onus on the dhcp/roaming code that sends us addresses to ensure
// that those addresses are externally reachable.
func (s *server) updateAddresses(addrs []net.Addr) ([]net.Addr, []naming.Endpoint) {
	var addedAddrs []net.Addr
	var addedEPs []naming.Endpoint
	for _, addr := range netstate.ConvertToAddresses(addrs) {
		if !netstate.IsAccessibleIP(addr) {
			continue
		}
		host := getHost(addr)
		added := false
		for ls, _ := range s.listenState {
			if ls == nil || !ls.roaming {
				continue
			}
			if i := findEndpoint(ls.ieps, host); i < 0 {
				niep := ls.protoIEP
				niep.Address = net.JoinHostPort(host, ls.port)
				niep.IsMountTable = s.servesMountTable
				niep.IsLeaf = s.isLeaf
				s.ctx.VI(2).Infof("rpc: dhcp adding: %s", niep)
				ls.ieps = append(ls.ieps, &niep)
				s.publisher.AddServer(niep.String())
				addedEPs = append(addedEPs, &niep)
				added = true
			}
		}
		if added {
			addedAddrs = append(addedAddrs, addr)
		}
	}
	return addedAddrs, addedEPs
}

type leafDispatcher struct {
	invoker rpc.Invoker
	auth    security.Authorizer
}

func (d leafDispatcher) Lookup(ctx *context.T, suffix string) (interface{}, security.Authorizer, error) {
	defer apilog.LogCallf(nil, "suffix=%.10s...", suffix)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if suffix != "" {
		return nil, nil, verror.New(verror.ErrUnknownSuffix, nil, suffix)
	}
	return d.invoker, d.auth, nil
}

func (s *server) Serve(name string, obj interface{}, authorizer security.Authorizer) error {
	defer apilog.LogCallf(nil, "name=%.10s...,obj=,authorizer=", name)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if obj == nil {
		return verror.New(verror.ErrBadArg, s.ctx, "nil object")
	}
	invoker, err := objectToInvoker(obj)
	if err != nil {
		return verror.New(verror.ErrBadArg, s.ctx, fmt.Sprintf("bad object: %v", err))
	}
	s.setLeaf(true)
	return s.ServeDispatcher(name, &leafDispatcher{invoker, authorizer})
}

func (s *server) setLeaf(value bool) {
	s.Lock()
	defer s.Unlock()
	s.isLeaf = value
	for ls, _ := range s.listenState {
		for i := range ls.ieps {
			ls.ieps[i].IsLeaf = s.isLeaf
		}
	}
}

func (s *server) ServeDispatcher(name string, disp rpc.Dispatcher) error {
	defer apilog.LogCallf(nil, "name=%.10s...,disp=", name)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
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
		for ls, _ := range s.listenState {
			for _, iep := range ls.ieps {
				s.publisher.AddServer(iep.String())
			}
		}
		s.publisher.AddName(name, s.servesMountTable, s.isLeaf)
	}
	return nil
}

func (s *server) AddName(name string) error {
	defer apilog.LogCallf(nil, "name=%.10s...", name)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if len(name) == 0 {
		return verror.New(verror.ErrBadArg, s.ctx, "name is empty")
	}
	s.Lock()
	defer s.Unlock()
	if err := s.allowed(publishing, "AddName"); err != nil {
		return err
	}
	vtrace.GetSpan(s.ctx).Annotate("Serving under name: " + name)
	s.publisher.AddName(name, s.servesMountTable, s.isLeaf)
	return nil
}

func (s *server) RemoveName(name string) {
	defer apilog.LogCallf(nil, "name=%.10s...", name)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	s.Lock()
	defer s.Unlock()
	if err := s.allowed(publishing, "RemoveName"); err != nil {
		return
	}
	vtrace.GetSpan(s.ctx).Annotate("Removed name: " + name)
	s.publisher.RemoveName(name)
}

func (s *server) Stop() error {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	serverDebug := fmt.Sprintf("Dispatcher: %T, Status:[%v]", s.disp, s.Status())
	s.ctx.VI(1).Infof("Stop: %s", serverDebug)
	defer s.ctx.VI(1).Infof("Stop done: %s", serverDebug)
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

	drain := func(ch chan pubsub.Setting) {
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
		if dhcp.change != nil {
			close(dhcp.change)
			dhcp.change = nil
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
	case <-time.After(5 * time.Second):
		s.ctx.Errorf("%s: Listener Close Error: %v", serverDebug, firstErr)
		s.ctx.Errorf("%s: Timedout waiting for goroutines to stop: listeners: %d (currently: %d)", serverDebug, nListeners, len(s.listeners))
		for ln, _ := range s.listeners {
			s.ctx.Errorf("%s: Listener: %v", serverDebug, ln)
		}
		for ls, _ := range s.listenState {
			s.ctx.Errorf("%s: ListenState: %v", serverDebug, ls)
		}
		<-done
		s.ctx.Infof("%s: Done waiting.", serverDebug)
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

func (s *server) Closed() <-chan struct{} {
	return s.ctx.Done()
}

// flowServer implements the RPC server-side protocol for a single RPC, over a
// flow that's already connected to the client.
type flowServer struct {
	ctx    *context.T     // context associated with the RPC
	server *server        // rpc.Server that this flow server belongs to
	disp   rpc.Dispatcher // rpc.Dispatcher that will serve RPCs on this flow
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
}

var (
	_ rpc.StreamServerCall = (*flowServer)(nil)
	_ security.Call        = (*flowServer)(nil)
)

func newFlowServer(flow stream.Flow, server *server) (*flowServer, error) {
	server.Lock()
	disp := server.disp
	server.Unlock()

	fs := &flowServer{
		ctx:        server.ctx,
		server:     server,
		disp:       disp,
		flow:       flow,
		discharges: make(map[string]security.Discharge),
	}
	typeenc := flow.VCDataCache().Get(vc.TypeEncoderKey{})
	if typeenc == nil {
		fs.enc = vom.NewEncoder(flow)
		fs.dec = vom.NewDecoder(flow)
	} else {
		fs.enc = vom.NewEncoderWithTypeEncoder(flow, typeenc.(*vom.TypeEncoder))
		typedec := flow.VCDataCache().Get(vc.TypeDecoderKey{})
		fs.dec = vom.NewDecoderWithTypeDecoder(flow, typedec.(*vom.TypeDecoder))
	}
	return fs, nil
}

// authorizeVtrace works by simulating a call to __debug/vtrace.Trace.  That
// rpc is essentially equivalent in power to the data we are attempting to
// attach here.
func (fs *flowServer) authorizeVtrace(ctx *context.T) error {
	// Set up a context as though we were calling __debug/vtrace.
	params := &security.CallParams{}
	params.Copy(fs)
	params.Method = "Trace"
	params.MethodTags = []*vdl.Value{vdl.ValueOf(access.Debug)}
	params.Suffix = "__debug/vtrace"

	var auth security.Authorizer
	if fs.server.dispReserved != nil {
		_, auth, _ = fs.server.dispReserved.Lookup(ctx, params.Suffix)
	}
	return authorize(fs.ctx, security.NewCall(params), auth)
}

func (fs *flowServer) serve() error {
	defer fs.flow.Close()

	results, err := fs.processRequest()

	vtrace.GetSpan(fs.ctx).Finish()

	var traceResponse vtrace.Response
	// Check if the caller is permitted to view vtrace data.
	if fs.authorizeVtrace(fs.ctx) == nil {
		traceResponse = vtrace.GetResponse(fs.ctx)
	}

	// Respond to the client with the response header and positional results.
	response := rpc.Response{
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
		return verror.New(errResponseEncoding, fs.ctx, fs.LocalEndpoint().String(), fs.RemoteEndpoint().String(), err)
	}
	if response.Error != nil {
		return response.Error
	}
	for ix, res := range results {
		if err := fs.enc.Encode(res); err != nil {
			if err == io.EOF {
				return err
			}
			return verror.New(errResultEncoding, fs.ctx, ix, fmt.Sprintf("%T=%v", res, res), err)
		}
	}
	// TODO(ashankar): Should unread data from the flow be drained?
	//
	// Reason to do so:
	// The common stream.Flow implementation (v.io/x/ref/runtime/internal/rpc/stream/vc/reader.go)
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

func (fs *flowServer) readRPCRequest() (*rpc.Request, error) {
	// Set a default timeout before reading from the flow. Without this timeout,
	// a client that sends no request or a partial request will retain the flow
	// indefinitely (and lock up server resources).
	initTimer := newTimer(defaultCallTimeout)
	defer initTimer.Stop()
	fs.flow.SetDeadline(initTimer.C)

	// Decode the initial request.
	var req rpc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return nil, verror.New(verror.ErrBadProtocol, fs.ctx, newErrBadRequest(fs.ctx, err))
	}
	return &req, nil
}

func (fs *flowServer) processRequest() ([]interface{}, error) {
	fs.starttime = time.Now()
	req, err := fs.readRPCRequest()
	if err != nil {
		// We don't know what the rpc call was supposed to be, but we'll create
		// a placeholder span so we can capture annotations.
		fs.ctx, _ = vtrace.WithNewSpan(fs.ctx, fmt.Sprintf("\"%s\".UNKNOWN", fs.suffix))
		return nil, err
	}
	// We must call fs.drainDecoderArgs for any error that occurs
	// after this point, and before we actually decode the arguments.
	fs.method = req.Method
	fs.suffix = strings.TrimLeft(req.Suffix, "/")

	if req.Language != "" {
		fs.ctx = i18n.WithLangID(fs.ctx, i18n.LangID(req.Language))
	}

	// TODO(mattr): Currently this allows users to trigger trace collection
	// on the server even if they will not be allowed to collect the
	// results later.  This might be considered a DOS vector.
	spanName := fmt.Sprintf("\"%s\".%s", fs.suffix, fs.method)
	fs.ctx, _ = vtrace.WithContinuedTrace(fs.ctx, spanName, req.TraceRequest)

	var cancel context.CancelFunc
	if !req.Deadline.IsZero() {
		fs.ctx, cancel = context.WithDeadline(fs.ctx, req.Deadline.Time)
	} else {
		fs.ctx, cancel = context.WithCancel(fs.ctx)
	}
	fs.flow.SetDeadline(fs.ctx.Done())
	go fs.cancelContextOnClose(cancel)

	// Initialize security: blessings, discharges, etc.
	if err := fs.initSecurity(req); err != nil {
		fs.drainDecoderArgs(int(req.NumPosArgs))
		return nil, err
	}
	// Lookup the invoker.
	invoker, auth, err := fs.lookup(fs.suffix, fs.method)
	if err != nil {
		fs.drainDecoderArgs(int(req.NumPosArgs))
		return nil, err
	}

	// Note that we strip the reserved prefix when calling the invoker so
	// that __Glob will call Glob.  Note that we've already assigned a
	// special invoker so that we never call the wrong method by mistake.
	strippedMethod := naming.StripReserved(fs.method)

	// Prepare invoker and decode args.
	numArgs := int(req.NumPosArgs)
	argptrs, tags, err := invoker.Prepare(fs.ctx, strippedMethod, numArgs)
	fs.tags = tags
	if err != nil {
		fs.drainDecoderArgs(numArgs)
		return nil, err
	}
	if called, want := req.NumPosArgs, uint64(len(argptrs)); called != want {
		fs.drainDecoderArgs(numArgs)
		return nil, newErrBadNumInputArgs(fs.ctx, fs.suffix, fs.method, called, want)
	}
	for ix, argptr := range argptrs {
		if err := fs.dec.Decode(argptr); err != nil {
			return nil, newErrBadInputArg(fs.ctx, fs.suffix, fs.method, uint64(ix), err)
		}
	}

	// Check application's authorization policy.
	if err := authorize(fs.ctx, fs, auth); err != nil {
		return nil, err
	}

	// Invoke the method.
	results, err := invoker.Invoke(fs.ctx, fs, strippedMethod, argptrs)
	fs.server.stats.record(fs.method, time.Since(fs.starttime))
	return results, err
}

// drainDecoderArgs drains the next n arguments encoded onto the flows decoder.
// This is needed to ensure that the client is able to encode all of its args
// before the server closes its flow. This guarantees that the client will
// consistently get the server's error response.
// TODO(suharshs): Figure out a better way to solve this race condition without
// unnecessarily reading all arguments.
func (fs *flowServer) drainDecoderArgs(n int) error {
	for i := 0; i < n; i++ {
		if err := fs.dec.Ignore(); err != nil {
			return err
		}
	}
	return nil
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
	case <-fs.ctx.Done():
	}
}

// lookup returns the invoker and authorizer responsible for serving the given
// name and method.  The suffix is stripped of any leading slashes. If it begins
// with rpc.DebugKeyword, we use the internal debug dispatcher to look up the
// invoker. Otherwise, and we use the server's dispatcher. The suffix and method
// value may be modified to match the actual suffix and method to use.
func (fs *flowServer) lookup(suffix string, method string) (rpc.Invoker, security.Authorizer, error) {
	if naming.IsReserved(method) {
		return reservedInvoker(fs.disp, fs.server.dispReserved), security.AllowEveryone(), nil
	}
	disp := fs.disp
	if naming.IsReserved(suffix) {
		disp = fs.server.dispReserved
	} else if fs.server.isLeaf && suffix != "" {
		innerErr := verror.New(errUnexpectedSuffix, fs.ctx, suffix)
		return nil, nil, verror.New(verror.ErrUnknownSuffix, fs.ctx, suffix, innerErr)
	}
	if disp != nil {
		obj, auth, err := disp.Lookup(fs.ctx, suffix)
		switch {
		case err != nil:
			return nil, nil, err
		case obj != nil:
			invoker, err := objectToInvoker(obj)
			if err != nil {
				return nil, nil, verror.New(verror.ErrInternal, fs.ctx, "invalid received object", err)
			}
			return invoker, auth, nil
		}
	}
	return nil, nil, verror.New(verror.ErrUnknownSuffix, fs.ctx, suffix)
}

func objectToInvoker(obj interface{}) (rpc.Invoker, error) {
	if obj == nil {
		return nil, errors.New("nil object")
	}
	if invoker, ok := obj.(rpc.Invoker); ok {
		return invoker, nil
	}
	return rpc.ReflectInvoker(obj)
}

func (fs *flowServer) initSecurity(req *rpc.Request) error {
	// LocalPrincipal is nil which means we are operating under
	// SecurityNone.
	if fs.LocalPrincipal() == nil {
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
	if got, want := req.GrantedBlessings.PublicKey(), fs.LocalPrincipal().PublicKey(); got != nil && !reflect.DeepEqual(got, want) {
		return verror.New(verror.ErrNoAccess, fs.ctx, verror.New(errBlessingsNotBound, fs.ctx, got, want))
	}
	fs.grantedBlessings = req.GrantedBlessings

	var err error
	if fs.clientBlessings, err = serverDecodeBlessings(fs.flow.VCDataCache(), req.Blessings, fs.server.stats); err != nil {
		// When the server can't access the blessings cache, the client is not following
		// protocol, so the server closes the VCs corresponding to the client endpoint.
		// TODO(suharshs,toddw): Figure out a way to only shutdown the current VC, instead
		// of all VCs connected to the RemoteEndpoint.
		fs.server.streamMgr.ShutdownEndpoint(fs.RemoteEndpoint())
		return verror.New(verror.ErrBadProtocol, fs.ctx, newErrBadBlessingsCache(fs.ctx, err))
	}
	// Verify that the blessings sent by the client in the request have the same public
	// key as those sent by the client during VC establishment.
	if got, want := fs.clientBlessings.PublicKey(), fs.flow.RemoteBlessings().PublicKey(); got != nil && !reflect.DeepEqual(got, want) {
		return verror.New(verror.ErrNoAccess, fs.ctx, fmt.Sprintf("blessings sent with the request are bound to a different public key (%v) from the blessing used during VC establishment (%v)", got, want))
	}
	fs.ackBlessings = true

	for _, d := range req.Discharges {
		fs.discharges[d.ID()] = d
	}
	return nil
}

func authorize(ctx *context.T, call security.Call, auth security.Authorizer) error {
	if call.LocalPrincipal() == nil {
		// LocalPrincipal is nil means that the server wanted to avoid
		// authentication, and thus wanted to skip authorization as well.
		return nil
	}
	if auth == nil {
		auth = security.DefaultAuthorizer()
	}
	if err := auth.Authorize(ctx, call); err != nil {
		return verror.New(verror.ErrNoAccess, ctx, newErrBadAuth(ctx, call.Suffix(), call.Method(), err))
	}
	return nil
}

// Send implements the rpc.Stream method.
func (fs *flowServer) Send(item interface{}) error {
	defer apilog.LogCallf(nil, "item=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	// The empty response header indicates what follows is a streaming result.
	if err := fs.enc.Encode(rpc.Response{}); err != nil {
		return err
	}
	return fs.enc.Encode(item)
}

// Recv implements the rpc.Stream method.
func (fs *flowServer) Recv(itemptr interface{}) error {
	defer apilog.LogCallf(nil, "itemptr=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	var req rpc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return err
	}
	if req.EndStreamArgs {
		fs.endStreamArgs = true
		return io.EOF
	}
	return fs.dec.Decode(itemptr)
}

// Implementations of rpc.ServerCall and security.Call methods.

func (fs *flowServer) Security() security.Call {
	//nologcall
	return fs
}
func (fs *flowServer) LocalDischarges() map[string]security.Discharge {
	//nologcall
	return fs.flow.LocalDischarges()
}
func (fs *flowServer) RemoteDischarges() map[string]security.Discharge {
	//nologcall
	return fs.discharges
}
func (fs *flowServer) Server() rpc.Server {
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

type proxyAuth struct {
	s *server
}

func (a proxyAuth) RPCStreamListenerOpt() {}

func (a proxyAuth) Login(proxy stream.Flow) (security.Blessings, []security.Discharge, error) {
	var (
		principal = a.s.principal
		dc        = a.s.dc
		ctx       = a.s.ctx
	)
	if principal == nil {
		return security.Blessings{}, nil, nil
	}
	proxyNames, _ := security.RemoteBlessingNames(ctx, security.NewCall(&security.CallParams{
		LocalPrincipal:   principal,
		RemoteBlessings:  proxy.RemoteBlessings(),
		RemoteDischarges: proxy.RemoteDischarges(),
		RemoteEndpoint:   proxy.RemoteEndpoint(),
		LocalEndpoint:    proxy.LocalEndpoint(),
	}))
	blessings := principal.BlessingStore().ForPeer(proxyNames...)
	tpc := blessings.ThirdPartyCaveats()
	if len(tpc) == 0 {
		return blessings, nil, nil
	}
	// Set DischargeImpetus.Server = proxyNames.
	// See https://v.io/i/392
	discharges := dc.PrepareDischarges(ctx, tpc, security.DischargeImpetus{})
	return blessings, discharges, nil
}

var _ manager.ProxyAuthenticator = proxyAuth{}
