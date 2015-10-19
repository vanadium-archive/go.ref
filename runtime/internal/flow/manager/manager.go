// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"fmt"
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

	iflow "v.io/x/ref/runtime/internal/flow"
	"v.io/x/ref/runtime/internal/flow/conn"
	"v.io/x/ref/runtime/internal/flow/protocols/bidi"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/version"
)

const (
	reconnectDelay    = 50 * time.Millisecond
	reapCacheInterval = 5 * time.Minute
	handshakeTimeout  = time.Minute
)

type manager struct {
	rid             naming.RoutingID
	closed          chan struct{}
	cache           *ConnCache
	ls              *listenState
	ctx             *context.T
	serverBlessings security.Blessings
	serverNames     []string
}

func NewWithBlessings(ctx *context.T, serverBlessings security.Blessings, rid naming.RoutingID) flow.Manager {
	m := &manager{
		rid:             rid,
		closed:          make(chan struct{}),
		cache:           NewConnCache(),
		ctx:             ctx,
		serverBlessings: serverBlessings,
		serverNames:     security.BlessingNames(v23.GetPrincipal(ctx), serverBlessings),
	}
	if rid != naming.NullRoutingID {
		m.ls = &listenState{
			q:         upcqueue.New(),
			listeners: []flow.Listener{},
			stopProxy: make(chan struct{}),
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

func New(ctx *context.T, rid naming.RoutingID) flow.Manager {
	var serverBlessings security.Blessings
	if rid != naming.NullRoutingID {
		serverBlessings = v23.GetPrincipal(ctx).BlessingStore().Default()
	}
	return NewWithBlessings(ctx, serverBlessings, rid)
}

type listenState struct {
	q           *upcqueue.T
	listenLoops sync.WaitGroup

	mu        sync.Mutex
	stopProxy chan struct{}
	listeners []flow.Listener
	endpoints []naming.Endpoint
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
	m.ls.mu.Unlock()
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
	m.ls.mu.Lock()
	if m.ls.listeners == nil {
		m.ls.mu.Unlock()
		ln.Close()
		return flow.NewErrBadState(ctx, NewErrManagerClosed(ctx))
	}
	m.ls.listeners = append(m.ls.listeners, ln)
	m.ls.endpoints = append(m.ls.endpoints, local)
	m.ls.mu.Unlock()

	m.ls.listenLoops.Add(1)
	go m.lnAcceptLoop(ctx, ln, local)
	return nil
}

// ProxyListen causes the Manager to accept flows from the specified endpoint.
// The endpoint must correspond to a vanadium proxy.
//
// update gets passed the complete set of endpoints for the proxy every time it
// is called.
func (m *manager) ProxyListen(ctx *context.T, ep naming.Endpoint, update func([]naming.Endpoint)) error {
	if m.ls == nil {
		return NewErrListeningWithNullRid(ctx)
	}
	m.ls.listenLoops.Add(1)
	go m.connectToProxy(ctx, ep, update)
	return nil
}

func (m *manager) connectToProxy(ctx *context.T, ep naming.Endpoint, update func([]naming.Endpoint)) {
	defer m.ls.listenLoops.Done()
	var eps []naming.Endpoint
	for delay := reconnectDelay; ; delay *= 2 {
		time.Sleep(delay - reconnectDelay)
		select {
		case <-ctx.Done():
			return
		case <-m.ls.stopProxy:
			return
		default:
		}
		f, c, err := m.internalDial(ctx, ep, proxyAuthorizer{})
		if err != nil {
			ctx.Error(err)
			continue
		}
		c.UpdateFlowHandler(ctx, &proxyFlowHandler{ctx: ctx, m: m})
		w, err := message.Append(ctx, &message.ProxyServerRequest{}, nil)
		if err != nil {
			ctx.Error(err)
			continue
		}
		if _, err = f.WriteMsg(w); err != nil {
			ctx.Error(err)
			continue
		}
		eps, err = m.readProxyResponse(ctx, f)
		if err != nil {
			ctx.Error(err)
			continue
		}
		for i := range eps {
			eps[i].(*inaming.Endpoint).Blessings = m.serverNames
		}
		update(eps)
		select {
		case <-ctx.Done():
			return
		case <-m.ls.stopProxy:
			return
		case <-f.Closed():
			update(nil)
			delay = reconnectDelay
		}
	}
}

func (m *manager) readProxyResponse(ctx *context.T, f flow.Flow) ([]naming.Endpoint, error) {
	b, err := f.ReadMsg()
	if err != nil {
		return nil, iflow.MaybeWrapError(flow.ErrNetwork, ctx, err)
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
				flowConn,
				local,
				version.Supported,
				handshakeTimeout,
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
			f,
			f.LocalEndpoint(),
			version.Supported,
			handshakeTimeout,
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
func (m *manager) ListeningEndpoints() (out []naming.Endpoint) {
	if m.ls == nil {
		return nil
	}
	m.ls.mu.Lock()
	out = make([]naming.Endpoint, len(m.ls.endpoints))
	copy(out, m.ls.endpoints)
	m.ls.mu.Unlock()
	if len(out) == 0 {
		out = append(out, &inaming.Endpoint{Protocol: bidi.Name, RID: m.rid})
	}
	return out
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
func (m *manager) Dial(ctx *context.T, remote naming.Endpoint, auth flow.PeerAuthorizer) (flow.Flow, error) {
	f, _, err := m.internalDial(ctx, remote, auth)
	return f, err
}

func (m *manager) internalDial(ctx *context.T, remote naming.Endpoint, auth flow.PeerAuthorizer) (flow.Flow, *conn.Conn, error) {
	// Look up the connection based on RoutingID first.
	c, err := m.cache.FindWithRoutingID(remote.RoutingID())
	if err != nil {
		return nil, nil, iflow.MaybeWrapError(flow.ErrBadState, ctx, err)
	}
	var (
		protocol         flow.Protocol
		network, address string
	)
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
	f, err := c.Dial(ctx, auth, remote)
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
			fh)
		if err != nil {
			proxyConn.Close(ctx, err)
			return nil, nil, iflow.MaybeWrapError(flow.ErrDialFailed, ctx, err)
		}
		if err := m.cache.InsertWithRoutingID(c); err != nil {
			return nil, nil, iflow.MaybeWrapError(flow.ErrBadState, ctx, err)
		}
		f, err = c.Dial(ctx, auth, remote)
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
		return p.Dial(ctx, protocol, address, timeout)
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
