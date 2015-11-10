// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/pubsub"
	iflow "v.io/x/ref/runtime/internal/flow"
	"v.io/x/ref/runtime/internal/flow/conn"
	"v.io/x/ref/runtime/internal/flow/protocols/bidi"
	"v.io/x/ref/runtime/internal/lib/roaming"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/version"
)

const (
	reapCacheInterval = 5 * time.Minute
	handshakeTimeout  = time.Minute
)

type manager struct {
	rid                   naming.RoutingID
	closed                chan struct{}
	cache                 *ConnCache
	ls                    *listenState
	ctx                   *context.T
	serverBlessings       security.Blessings
	serverAuthorizedPeers []security.BlessingPattern // empty list implies all peers are authorized to see the server's blessings.
	serverNames           []string
	acceptChannelTimeout  time.Duration
}

type listenState struct {
	q             *upcqueue.T
	listenLoops   sync.WaitGroup
	dhcpPublisher *pubsub.Publisher

	mu             sync.Mutex
	stopProxy      chan struct{}
	listeners      []flow.Listener
	endpoints      []*endpointState
	proxyEndpoints []naming.Endpoint
	notifyWatchers chan struct{}
	roaming        bool
	roamingClosed  <-chan struct{}
}

type endpointState struct {
	leps         []*inaming.Endpoint // the list of currently active endpoints.
	tmplEndpoint *inaming.Endpoint   // endpoint used as a template for creating new endpoints from the network interfaces provided from roaming.
	roaming      bool
}

func NewWithBlessings(ctx *context.T, serverBlessings security.Blessings, rid naming.RoutingID, serverAuthorizedPeers []security.BlessingPattern, dhcpPublisher *pubsub.Publisher, channelTimeout time.Duration) flow.Manager {

	m := &manager{
		rid:                  rid,
		closed:               make(chan struct{}),
		cache:                NewConnCache(),
		ctx:                  ctx,
		acceptChannelTimeout: channelTimeout,
	}
	if rid != naming.NullRoutingID {
		m.serverBlessings = serverBlessings
		m.serverAuthorizedPeers = serverAuthorizedPeers
		m.serverNames = security.BlessingNames(v23.GetPrincipal(ctx), serverBlessings)
		m.ls = &listenState{
			q:              upcqueue.New(),
			listeners:      []flow.Listener{},
			stopProxy:      make(chan struct{}),
			notifyWatchers: make(chan struct{}),
			dhcpPublisher:  dhcpPublisher,
		}
	}
	go func() {
		ticker := time.NewTicker(reapCacheInterval)
		for {
			select {
			case <-ctx.Done():
				m.stopListening()
				m.cache.Close(ctx)
				ticker.Stop()
				close(m.closed)
				return
			case <-ticker.C:
				// Periodically kill closed connections.
				m.cache.KillConnections(ctx, 0)
			}
		}
	}()
	return m
}

func New(ctx *context.T, rid naming.RoutingID, dhcpPublisher *pubsub.Publisher, channelTimeout time.Duration) flow.Manager {
	var serverBlessings security.Blessings
	if rid != naming.NullRoutingID {
		serverBlessings = v23.GetPrincipal(ctx).BlessingStore().Default()
	}
	return NewWithBlessings(ctx, serverBlessings, rid, nil, dhcpPublisher, channelTimeout)
}

func (m *manager) stopListening() {
	if m.ls == nil {
		return
	}
	m.ls.mu.Lock()
	listeners := m.ls.listeners
	m.ls.listeners = nil
	m.ls.endpoints = nil
	if m.ls.stopProxy != nil {
		close(m.ls.stopProxy)
		m.ls.stopProxy = nil
	}
	if m.ls.notifyWatchers != nil {
		close(m.ls.notifyWatchers)
		m.ls.notifyWatchers = nil
	}
	roamingClosed := m.ls.roamingClosed
	m.ls.mu.Unlock()
	if roamingClosed != nil {
		<-roamingClosed
	}
	for _, ln := range listeners {
		ln.Close()
	}
	m.ls.listenLoops.Wait()
}

func (m *manager) StopListening(ctx *context.T) {
	if m.ls == nil {
		return
	}
	m.stopListening()
	// Now no more connections can start.  We should lame duck all the conns
	// and wait for all of them to ack.
	m.cache.EnterLameDuckMode(ctx)
	// Now nobody should send any more flows, so close the queue.
	m.ls.q.Close()
}

// Listen causes the Manager to accept flows from the provided protocol and address.
// Listen may be called muliple times.
func (m *manager) Listen(ctx *context.T, protocol, address string) error {
	if m.ls == nil {
		return NewErrListeningWithNullRid(ctx)
	}
	ln, err := listen(ctx, protocol, address)
	if err != nil {
		return iflow.MaybeWrapError(flow.ErrNetwork, ctx, err)
	}
	local := &inaming.Endpoint{
		Protocol:  protocol,
		Address:   ln.Addr().String(),
		RID:       m.rid,
		Blessings: m.serverNames,
	}
	defer m.ls.mu.Unlock()
	m.ls.mu.Lock()
	if m.ls.listeners == nil {
		m.ls.mu.Unlock()
		ln.Close()
		return flow.NewErrBadState(ctx, NewErrManagerClosed(ctx))
	}
	m.ls.listeners = append(m.ls.listeners, ln)
	leps, roam, err := m.createEndpoints(ctx, local)
	if err != nil {
		return iflow.MaybeWrapError(flow.ErrBadArg, ctx, err)
	}
	m.ls.endpoints = append(m.ls.endpoints, &endpointState{
		leps:         leps,
		tmplEndpoint: local,
		roaming:      roam,
	})
	if !m.ls.roaming && m.ls.dhcpPublisher != nil && roam {
		m.ls.roaming = true
		m.ls.roamingClosed = roaming.ReadRoamingStream(ctx, m.ls.dhcpPublisher, m.rmAddrs, m.addAddrs)
	}

	m.ls.listenLoops.Add(1)
	go m.lnAcceptLoop(ctx, ln, local)
	return nil
}

func (m *manager) createEndpoints(ctx *context.T, lep naming.Endpoint) ([]*inaming.Endpoint, bool, error) {
	iep := lep.(*inaming.Endpoint)
	if !strings.HasPrefix(iep.Protocol, "tcp") &&
		!strings.HasPrefix(iep.Protocol, "ws") {
		// If not tcp, ws, or wsh, just return the endpoint we were given.
		return []*inaming.Endpoint{iep}, false, nil
	}
	host, port, err := net.SplitHostPort(iep.Address)
	if err != nil {
		return nil, false, err
	}
	chooser := v23.GetListenSpec(ctx).AddressChooser
	addrs, unspecified, err := netstate.PossibleAddresses(iep.Protocol, host, chooser)
	if err != nil {
		return nil, false, err
	}
	ieps := make([]*inaming.Endpoint, 0, len(addrs))
	for _, addr := range addrs {
		n, err := inaming.NewEndpoint(lep.String())
		if err != nil {
			return nil, false, err
		}
		n.Address = net.JoinHostPort(addr.String(), port)
		ieps = append(ieps, n)
	}
	return ieps, unspecified, nil
}

func (m *manager) addAddrs(addrs []net.Addr) {
	defer m.ls.mu.Unlock()
	m.ls.mu.Lock()
	changed := false
	for _, addr := range netstate.ConvertToAddresses(addrs) {
		if !netstate.IsAccessibleIP(addr) {
			continue
		}
		host, _ := getHostPort(addr)
		for _, epState := range m.ls.endpoints {
			if !epState.roaming {
				continue
			}
			tmplEndpoint := epState.tmplEndpoint
			_, port := getHostPort(tmplEndpoint.Addr())
			if i := findEndpoint(epState, host); i < 0 {
				nep := *tmplEndpoint
				nep.Address = net.JoinHostPort(host, port)
				epState.leps = append(epState.leps, &nep)
				changed = true
			}
		}
	}
	if changed && m.ls.notifyWatchers != nil {
		close(m.ls.notifyWatchers)
		m.ls.notifyWatchers = make(chan struct{})
	}
}

func findEndpoint(epState *endpointState, host string) int {
	for i, ep := range epState.leps {
		epHost, _ := getHostPort(ep.Addr())
		if epHost == host {
			return i
		}
	}
	return -1
}

func (m *manager) rmAddrs(addrs []net.Addr) {
	defer m.ls.mu.Unlock()
	m.ls.mu.Lock()
	changed := false
	for _, addr := range netstate.ConvertToAddresses(addrs) {
		host, _ := getHostPort(addr)
		for _, epState := range m.ls.endpoints {
			if !epState.roaming {
				continue
			}
			if i := findEndpoint(epState, host); i >= 0 {
				n := len(epState.leps) - 1
				epState.leps[i], epState.leps[n] = epState.leps[n], nil
				epState.leps = epState.leps[:n]
				changed = true
			}
		}
	}
	if changed && m.ls.notifyWatchers != nil {
		close(m.ls.notifyWatchers)
		m.ls.notifyWatchers = make(chan struct{})
	}
}

func getHostPort(address net.Addr) (string, string) {
	host, port, err := net.SplitHostPort(address.String())
	if err == nil {
		return host, port
	}
	return address.String(), ""
}

// ProxyListen causes the Manager to accept flows from the specified endpoint.
// The endpoint must correspond to a vanadium proxy.
// The call will block until the connection to the proxy endpoint fails. The
// caller may then choose to retry the connection.
func (m *manager) ProxyListen(ctx *context.T, ep naming.Endpoint) error {
	if m.ls == nil {
		return NewErrListeningWithNullRid(ctx)
	}
	defer m.updateProxyEndpoints(nil)
	f, c, err := m.internalDial(ctx, ep, proxyAuthorizer{}, m.acceptChannelTimeout)
	if err != nil {
		return err
	}
	c.UpdateFlowHandler(ctx, &proxyFlowHandler{ctx: ctx, m: m})
	w, err := message.Append(ctx, &message.ProxyServerRequest{}, nil)
	if err != nil {
		return err
	}
	if _, err = f.WriteMsg(w); err != nil {
		return err
	}
	// We do exponential backoff unless the proxy closes the flow cleanly, in which
	// case we redial immediately.
	for {
		// we keep reading updates until we encounter an error, usually because the
		// flow has been closed.
		eps, err := m.readProxyResponse(ctx, f)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		for i := range eps {
			eps[i].(*inaming.Endpoint).Blessings = m.serverNames
		}
		m.updateProxyEndpoints(eps)
	}
	// We should not reach this line.
	return nil
}

func (m *manager) updateProxyEndpoints(eps []naming.Endpoint) {
	defer m.ls.mu.Unlock()
	m.ls.mu.Lock()
	if endpointsEqual(m.ls.proxyEndpoints, eps) {
		return
	}
	m.ls.proxyEndpoints = eps
	// The proxy endpoints have changed so we need to notify any watchers to
	// requery ListeningEndpoints.
	if m.ls.notifyWatchers != nil {
		close(m.ls.notifyWatchers)
		m.ls.notifyWatchers = make(chan struct{})
	}
}

func endpointsEqual(a, b []naming.Endpoint) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]struct{})
	for _, ep := range a {
		m[ep.String()] = struct{}{}
	}
	for _, ep := range b {
		key := ep.String()
		if _, ok := m[key]; !ok {
			return false
		}
		delete(m, key)
	}
	return len(m) == 0
}

func (m *manager) readProxyResponse(ctx *context.T, f flow.Flow) ([]naming.Endpoint, error) {
	b, err := f.ReadMsg()
	if err != nil {
		// Its important that we return the error as-is from f.ReadMsg since ProxyListen uses it.
		return nil, err
	}
	msg, err := message.Read(ctx, b)
	if err != nil {
		return nil, iflow.MaybeWrapError(flow.ErrBadArg, ctx, err)
	}
	res, ok := msg.(*message.ProxyResponse)
	if !ok {
		return nil, flow.NewErrBadArg(ctx, NewErrInvalidProxyResponse(ctx, fmt.Sprintf("%t", res)))
	}
	return res.Endpoints, nil
}

// TODO(suharshs): Figure out what blessings to present here. And present discharges.
type proxyAuthorizer struct{}

func (proxyAuthorizer) AuthorizePeer(
	ctx *context.T,
	localEndpoint, remoteEndpoint naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge,
) ([]string, []security.RejectedBlessing, error) {
	return nil, nil, nil
}

func (a proxyAuthorizer) BlessingsForPeer(ctx *context.T, _ []string) (
	security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}

func (m *manager) lnAcceptLoop(ctx *context.T, ln flow.Listener, local naming.Endpoint) {
	defer m.ls.listenLoops.Done()
	const killConnectionsRetryDelay = 5 * time.Millisecond
	for {
		flowConn, err := ln.Accept(ctx)
		for tokill := 1; isTemporaryError(err); tokill *= 2 {
			if isTooManyOpenFiles(err) {
				if err := m.cache.KillConnections(ctx, tokill); err != nil {
					ctx.VI(2).Infof("failed to kill connections: %v", err)
					continue
				}
			} else {
				tokill = 1
			}
			time.Sleep(killConnectionsRetryDelay)
			flowConn, err = ln.Accept(ctx)
		}
		if err != nil {
			m.ls.mu.Lock()
			closed := m.ls.listeners == nil
			m.ls.mu.Unlock()
			if !closed {
				ctx.Errorf("ln.Accept on localEP %v failed: %v", local, err)
			}
			return
		}

		m.ls.mu.Lock()
		if m.ls.listeners == nil {
			m.ls.mu.Unlock()
			return
		}
		m.ls.mu.Unlock()
		fh := &flowHandler{m, make(chan struct{})}
		go func() {
			c, err := conn.NewAccepted(
				m.ctx,
				m.serverBlessings,
				m.serverAuthorizedPeers,
				flowConn,
				local,
				version.Supported,
				handshakeTimeout,
				m.acceptChannelTimeout,
				fh)
			if err != nil {
				ctx.Errorf("failed to accept flow.Conn on localEP %v failed: %v", local, err)
				flowConn.Close()
			} else if err = m.cache.InsertWithRoutingID(c); err != nil {
				ctx.Errorf("failed to cache conn %v: %v", c, err)
			}
			close(fh.cached)
		}()
	}
}

type flowHandler struct {
	m      *manager
	cached chan struct{}
}

func (h *flowHandler) HandleFlow(f flow.Flow) error {
	if h.cached != nil {
		<-h.cached
	}
	return h.m.ls.q.Put(f)
}

type proxyFlowHandler struct {
	ctx *context.T
	m   *manager
}

func (h *proxyFlowHandler) HandleFlow(f flow.Flow) error {
	go func() {
		fh := &flowHandler{h.m, make(chan struct{})}
		h.m.ls.mu.Lock()
		if h.m.ls.listeners == nil {
			// If we've entered lame duck mode we want to reject new flows
			// from the proxy.  This should come out as a connection failure
			// for the client, which will result in a retry.
			h.m.ls.mu.Unlock()
			f.Close()
			return
		}
		h.m.ls.mu.Unlock()
		c, err := conn.NewAccepted(
			h.ctx,
			h.m.serverBlessings,
			h.m.serverAuthorizedPeers,
			f,
			f.LocalEndpoint(),
			version.Supported,
			handshakeTimeout,
			h.m.acceptChannelTimeout,
			fh)
		if err != nil {
			h.ctx.Errorf("failed to create accepted conn: %v", err)
		} else if err = h.m.cache.InsertWithRoutingID(c); err != nil {
			h.ctx.Errorf("failed to create accepted conn: %v", err)
		}
		close(fh.cached)
	}()
	return nil
}

// ListeningEndpoints returns the endpoints that the Manager has explicitly
// called Listen on. The Manager will accept new flows on these endpoints.
// Proxied endpoints are not returned.
// If the Manager is not listening on any endpoints, an endpoint with the
// Manager's RoutingID will be returned for use in bidirectional RPC.
// Returned endpoints all have the Manager's unique RoutingID.
func (m *manager) ListeningEndpoints() (out []naming.Endpoint, changed <-chan struct{}) {
	if m.ls == nil {
		return nil, nil
	}
	m.ls.mu.Lock()
	out = make([]naming.Endpoint, len(m.ls.proxyEndpoints))
	copy(out, m.ls.proxyEndpoints)
	for _, epState := range m.ls.endpoints {
		for _, ep := range epState.leps {
			out = append(out, ep)
		}
	}
	changed = m.ls.notifyWatchers
	m.ls.mu.Unlock()
	if len(out) == 0 {
		out = append(out, &inaming.Endpoint{Protocol: bidi.Name, RID: m.rid})
	}
	return out, changed
}

// Accept blocks until a new Flow has been initiated by a remote process.
// Flows are accepted from addresses that the Manager is listening on,
// including outgoing dialed connections.
//
// For example:
//   err := m.Listen(ctx, "tcp", ":0")
//   for {
//     flow, err := m.Accept(ctx)
//     // process flow
//   }
//
// can be used to accept Flows initiated by remote processes.
func (m *manager) Accept(ctx *context.T) (flow.Flow, error) {
	if m.ls == nil {
		return nil, NewErrListeningWithNullRid(ctx)
	}
	item, err := m.ls.q.Get(ctx.Done())
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, flow.NewErrNetwork(ctx, NewErrManagerClosed(ctx))
	case err != nil:
		return nil, flow.NewErrNetwork(ctx, NewErrAcceptFailed(ctx, err))
	default:
		return item.(flow.Flow), nil
	}
}

// Dial creates a Flow to the provided remote endpoint, using 'auth' to
// determine the blessings that will be sent to the remote end.
//
// To maximize re-use of connections, the Manager will also Listen on Dialed
// connections for the lifetime of the connection.
func (m *manager) Dial(ctx *context.T, remote naming.Endpoint, auth flow.PeerAuthorizer, channelTimeout time.Duration) (flow.Flow, error) {
	f, _, err := m.internalDial(ctx, remote, auth, channelTimeout)
	return f, err
}

func (m *manager) internalDial(ctx *context.T, remote naming.Endpoint, auth flow.PeerAuthorizer, channelTimeout time.Duration) (flow.Flow, *conn.Conn, error) {
	// Look up the connection based on RoutingID first.
	c, err := m.cache.FindWithRoutingID(remote.RoutingID())
	if err != nil {
		return nil, nil, iflow.MaybeWrapError(flow.ErrBadState, ctx, err)
	}
	var (
		protocol         flow.Protocol
		network, address string
	)
	auth = &peerAuthorizer{auth, m.serverAuthorizedPeers}
	if c == nil {
		addr := remote.Addr()
		protocol, _ = flow.RegisteredProtocol(addr.Network())
		// (network, address) in the endpoint might not always match up
		// with the key used for caching conns. For example:
		// - conn, err := net.Dial("tcp", "www.google.com:80")
		//   fmt.Println(conn.RemoteAddr()) // Might yield the corresponding IP address
		// - Similarly, an unspecified IP address (net.IP.IsUnspecified) like "[::]:80"
		//   might yield "[::1]:80" (loopback interface) in conn.RemoteAddr().
		// Thus we look for Conns with the resolved address.
		network, address, err = resolve(ctx, protocol, addr.Network(), addr.String())
		if err != nil {
			return nil, nil, iflow.MaybeWrapError(flow.ErrResolveFailed, ctx, err)
		}
		c, err = m.cache.ReservedFind(network, address, remote.BlessingNames())
		if err != nil {
			return nil, nil, iflow.MaybeWrapError(flow.ErrBadState, ctx, err)
		}
		defer m.cache.Unreserve(network, address, remote.BlessingNames())
	}
	if c == nil {
		flowConn, err := dial(ctx, protocol, network, address)
		if err != nil {
			switch err := err.(type) {
			case *net.OpError:
				if err, ok := err.Err.(net.Error); ok && err.Timeout() {
					return nil, nil, iflow.MaybeWrapError(verror.ErrTimeout, ctx, err)
				}
			}
			return nil, nil, iflow.MaybeWrapError(flow.ErrDialFailed, ctx, err)
		}
		var fh conn.FlowHandler
		if m.ls != nil {
			m.ls.mu.Lock()
			if stoppedListening := m.ls.listeners == nil; !stoppedListening {
				fh = &flowHandler{m: m}
			}
			m.ls.mu.Unlock()
		}
		c, err = conn.NewDialed(
			ctx,
			m.serverBlessings,
			flowConn,
			localEndpoint(flowConn, m.rid),
			remote,
			version.Supported,
			auth,
			handshakeTimeout,
			m.acceptChannelTimeout,
			fh,
		)
		if err != nil {
			flowConn.Close()
			return nil, nil, iflow.MaybeWrapError(flow.ErrDialFailed, ctx, err)
		}
		if err := m.cache.Insert(c, network, address); err != nil {
			return nil, nil, flow.NewErrBadState(ctx, err)
		}
		// Now that c is in the cache we can explicitly unreserve.
		m.cache.Unreserve(network, address, remote.BlessingNames())
	}
	f, err := c.Dial(ctx, auth, remote, channelTimeout)
	if err != nil {
		return nil, nil, iflow.MaybeWrapError(flow.ErrDialFailed, ctx, err)
	}

	// If we are dialing out to a Proxy, we need to dial a conn on this flow, and
	// return a flow on that corresponding conn.
	if proxyConn := c; remote.RoutingID() != naming.NullRoutingID && remote.RoutingID() != proxyConn.RemoteEndpoint().RoutingID() {
		var fh conn.FlowHandler
		if m.ls != nil {
			m.ls.mu.Lock()
			if stoppedListening := m.ls.listeners == nil; !stoppedListening {
				fh = &flowHandler{m: m}
			}
			m.ls.mu.Unlock()
		}
		c, err = conn.NewDialed(
			ctx,
			m.serverBlessings,
			f,
			proxyConn.LocalEndpoint(),
			remote,
			version.Supported,
			auth,
			handshakeTimeout,
			m.acceptChannelTimeout,
			fh)
		if err != nil {
			proxyConn.Close(ctx, err)
			return nil, nil, iflow.MaybeWrapError(flow.ErrDialFailed, ctx, err)
		}
		if err := m.cache.InsertWithRoutingID(c); err != nil {
			return nil, nil, iflow.MaybeWrapError(flow.ErrBadState, ctx, err)
		}
		f, err = c.Dial(ctx, auth, remote, channelTimeout)
		if err != nil {
			proxyConn.Close(ctx, err)
			return nil, nil, iflow.MaybeWrapError(flow.ErrDialFailed, ctx, err)
		}
	}
	return f, c, nil
}

// RoutingID returns the naming.Routing of the flow.Manager.
func (m *manager) RoutingID() naming.RoutingID {
	return m.rid
}

// Closed returns a channel that remains open for the lifetime of the Manager
// object. Once the channel is closed any operations on the Manager will
// necessarily fail.
func (m *manager) Closed() <-chan struct{} {
	return m.closed
}

func dial(ctx *context.T, p flow.Protocol, protocol, address string) (flow.Conn, error) {
	if p != nil {
		var timeout time.Duration
		if dl, ok := ctx.Deadline(); ok {
			timeout = dl.Sub(time.Now())
		}
		type connAndErr struct {
			c flow.Conn
			e error
		}
		ch := make(chan connAndErr)
		go func() {
			conn, err := p.Dial(ctx, protocol, address, timeout)
			ch <- connAndErr{conn, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case cae := <-ch:
			return cae.c, cae.e
		}
	}
	return nil, NewErrUnknownProtocol(ctx, protocol)
}

func resolve(ctx *context.T, p flow.Protocol, protocol, address string) (string, string, error) {
	if p != nil {
		net, addr, err := p.Resolve(ctx, protocol, address)
		if err != nil {
			return "", "", err
		}
		return net, addr, nil
	}
	return "", "", NewErrUnknownProtocol(ctx, protocol)
}

func listen(ctx *context.T, protocol, address string) (flow.Listener, error) {
	if p, _ := flow.RegisteredProtocol(protocol); p != nil {
		ln, err := p.Listen(ctx, protocol, address)
		if err != nil {
			return nil, err
		}
		return ln, nil
	}
	return nil, NewErrUnknownProtocol(ctx, protocol)
}

func localEndpoint(conn flow.Conn, rid naming.RoutingID) naming.Endpoint {
	localAddr := conn.LocalAddr()
	ep := &inaming.Endpoint{
		Protocol: localAddr.Network(),
		Address:  localAddr.String(),
		RID:      rid,
	}
	return ep
}

func isTemporaryError(err error) bool {
	oErr, ok := err.(*net.OpError)
	return ok && oErr.Temporary()
}

func isTooManyOpenFiles(err error) bool {
	oErr, ok := err.(*net.OpError)
	return ok && strings.Contains(oErr.Err.Error(), syscall.EMFILE.Error())
}

// peerAuthorizer implements flow.PeerAuthorizer. It is meant to be used
// when a server operating in private mode (i.e., with a non-empty set
// of authorized peers) acts as a client. It wraps around the PeerAuthorizer
// specified by the call opts and addiitonally ensures that any peers that
// the client communicates with belong to the set of authorized peers.
type peerAuthorizer struct {
	auth            flow.PeerAuthorizer
	authorizedPeers []security.BlessingPattern
}

func (x *peerAuthorizer) AuthorizePeer(
	ctx *context.T,
	localEP, remoteEP naming.Endpoint,
	remoteBlessings security.Blessings,
	remoteDischarges map[string]security.Discharge) ([]string, []security.RejectedBlessing, error) {
	if len(x.authorizedPeers) == 0 {
		return x.auth.AuthorizePeer(ctx, localEP, remoteEP, remoteBlessings, remoteDischarges)
	}
	localPrincipal := v23.GetPrincipal(ctx)
	// The "Method" and "Suffix" fields of the call are not populated
	// as they are considered irrelevant for authorizing server blessings.
	call := security.NewCall(&security.CallParams{
		Timestamp:        time.Now(),
		LocalPrincipal:   localPrincipal,
		LocalEndpoint:    localEP,
		RemoteBlessings:  remoteBlessings,
		RemoteDischarges: remoteDischarges,
		RemoteEndpoint:   remoteEP,
	})

	peerNames, rejectedPeerNames := security.RemoteBlessingNames(ctx, call)
	for _, p := range x.authorizedPeers {
		if p.MatchedBy(peerNames...) {
			return x.auth.AuthorizePeer(ctx, localEP, remoteEP, remoteBlessings, remoteDischarges)
		}
	}
	return nil, nil, fmt.Errorf("peer names: %v (rejected: %v) do not match one of the authorized patterns: %v", peerNames, rejectedPeerNames, x.authorizedPeers)
}

func (x *peerAuthorizer) BlessingsForPeer(ctx *context.T, peerNames []string) (
	security.Blessings, map[string]security.Discharge, error) {
	return x.auth.BlessingsForPeer(ctx, peerNames)
}
