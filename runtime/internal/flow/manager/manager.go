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
	"v.io/v23/verror"

	"v.io/x/ref/runtime/internal/flow/conn"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/version"
)

const (
	reconnectDelay    = 50 * time.Millisecond
	reapCacheInterval = 5 * time.Minute
)

type manager struct {
	rid    naming.RoutingID
	closed chan struct{}
	q      *upcqueue.T
	cache  *ConnCache

	mu              *sync.Mutex
	listenEndpoints []naming.Endpoint
	proxyEndpoints  map[string][]naming.Endpoint // keyed by proxy address
	listeners       []flow.Listener
	wg              sync.WaitGroup
}

func New(ctx *context.T, rid naming.RoutingID) flow.Manager {
	m := &manager{
		rid:            rid,
		closed:         make(chan struct{}),
		q:              upcqueue.New(),
		cache:          NewConnCache(),
		mu:             &sync.Mutex{},
		proxyEndpoints: make(map[string][]naming.Endpoint),
		listeners:      []flow.Listener{},
	}
	go func() {
		ticker := time.NewTicker(reapCacheInterval)
		for {
			select {
			case <-ctx.Done():
				m.mu.Lock()
				listeners := m.listeners
				m.listeners = nil
				m.mu.Unlock()
				for _, ln := range listeners {
					ln.Close()
				}
				m.cache.Close(ctx)
				m.q.Close()
				m.wg.Wait()
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

// Listen causes the Manager to accept flows from the provided protocol and address.
// Listen may be called muliple times.
//
// The flow.Manager associated with ctx must be the receiver of the method,
// otherwise an error is returned.
func (m *manager) Listen(ctx *context.T, protocol, address string) error {
	if protocol == inaming.Network {
		return m.proxyListen(ctx, address)
	}
	return m.listen(ctx, protocol, address)
}

func (m *manager) listen(ctx *context.T, protocol, address string) error {
	ln, err := listen(ctx, protocol, address)
	if err != nil {
		return flow.NewErrNetwork(ctx, err)
	}
	local := &inaming.Endpoint{
		Protocol: protocol,
		Address:  ln.Addr().String(),
		RID:      m.rid,
	}
	m.mu.Lock()
	if m.listeners == nil {
		return flow.NewErrBadState(ctx, NewErrManagerClosed(ctx))
	}
	m.listeners = append(m.listeners, ln)
	m.mu.Unlock()
	m.wg.Add(1)
	go m.lnAcceptLoop(ctx, ln, local)
	m.mu.Lock()
	m.listenEndpoints = append(m.listenEndpoints, local)
	m.mu.Unlock()
	return nil
}

func (m *manager) proxyListen(ctx *context.T, address string) error {
	ep, err := inaming.NewEndpoint(address)
	if err != nil {
		return flow.NewErrBadArg(ctx, err)
	}
	m.wg.Add(1)
	go m.connectToProxy(ctx, address, ep)
	return nil
}

func (m *manager) connectToProxy(ctx *context.T, address string, ep naming.Endpoint) {
	defer m.wg.Done()
	for delay := reconnectDelay; ; delay *= 2 {
		time.Sleep(delay - reconnectDelay)
		select {
		case <-ctx.Done():
			return
		default:
		}
		f, err := m.internalDial(ctx, ep, proxyBlessingsForPeer{}.run, &proxyFlowHandler{ctx: ctx, m: m})
		if err != nil {
			ctx.Error(err)
			continue
		}
		w, err := message.Append(ctx, &message.ProxyServerRequest{}, nil)
		if err != nil {
			ctx.Error(err)
			continue
		}
		if _, err = f.WriteMsg(w); err != nil {
			ctx.Error(err)
			continue
		}
		eps, err := m.readProxyResponse(ctx, f)
		if err != nil {
			ctx.Error(err)
			continue
		}
		m.mu.Lock()
		m.proxyEndpoints[address] = eps
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-f.Closed():
			m.mu.Lock()
			delete(m.proxyEndpoints, address)
			m.mu.Unlock()
			delay = reconnectDelay
		}
	}
}

func (m *manager) readProxyResponse(ctx *context.T, f flow.Flow) ([]naming.Endpoint, error) {
	b, err := f.ReadMsg()
	if err != nil {
		return nil, flow.NewErrNetwork(ctx, err)
	}
	msg, err := message.Read(ctx, b)
	if err != nil {
		return nil, flow.NewErrBadArg(ctx, err)
	}
	res, ok := msg.(*message.ProxyResponse)
	if !ok {
		return nil, flow.NewErrBadArg(ctx, NewErrInvalidProxyResponse(ctx, fmt.Sprintf("%t", res)))
	}
	return res.Endpoints, nil
}

type proxyBlessingsForPeer struct{}

// TODO(suharshs): Figure out what blessings to present here. And present discharges.
func (proxyBlessingsForPeer) run(ctx *context.T, lep, rep naming.Endpoint, rb security.Blessings,
	rd map[string]security.Discharge) (security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}

func (m *manager) lnAcceptLoop(ctx *context.T, ln flow.Listener, local naming.Endpoint) {
	defer m.wg.Done()
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
			ctx.Errorf("ln.Accept on localEP %v failed: %v", local, err)
			return
		}
		cached := make(chan struct{})
		c, err := conn.NewAccepted(
			ctx,
			flowConn,
			local,
			version.Supported,
			&flowHandler{q: m.q, cached: cached},
		)
		if err != nil {
			close(cached)
			flowConn.Close()
			ctx.Errorf("failed to accept flow.Conn on localEP %v failed: %v", local, err)
			continue
		}
		if err := m.cache.InsertWithRoutingID(c); err != nil {
			close(cached)
			ctx.Errorf("failed to cache conn %v: %v", c, err)
		}
		close(cached)
	}
}

type flowHandler struct {
	q      *upcqueue.T
	cached chan struct{}
}

func (h *flowHandler) HandleFlow(f flow.Flow) error {
	if h.cached != nil {
		<-h.cached
	}
	return h.q.Put(f)
}

type proxyFlowHandler struct {
	ctx *context.T
	m   *manager
}

func (h *proxyFlowHandler) HandleFlow(f flow.Flow) error {
	go func() {
		c, err := conn.NewAccepted(
			h.ctx,
			f,
			f.Conn().LocalEndpoint(),
			version.Supported,
			&flowHandler{q: h.m.q})
		if err != nil {
			h.ctx.Errorf("failed to create accepted conn: %v", err)
			return
		}
		if err := h.m.cache.InsertWithRoutingID(c); err != nil {
			h.ctx.Errorf("failed to create accepted conn: %v", err)
			return
		}
	}()
	return nil
}

// ListeningEndpoints returns the endpoints that the Manager has explicitly
// listened on. The Manager will accept new flows on these endpoints.
// If the Manager is not listening on any endpoints, an endpoint with the
// Manager's RoutingID will be returned for use in bidirectional RPC.
// Returned endpoints all have the Manager's unique RoutingID.
func (m *manager) ListeningEndpoints() []naming.Endpoint {
	m.mu.Lock()
	ret := make([]naming.Endpoint, len(m.listenEndpoints))
	copy(ret, m.listenEndpoints)
	for _, peps := range m.proxyEndpoints {
		ret = append(ret, peps...)
	}
	m.mu.Unlock()
	if len(ret) == 0 {
		ret = append(ret, &inaming.Endpoint{RID: m.rid})
	}
	return ret
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
//
// The flow.Manager associated with ctx must be the receiver of the method,
// otherwise an error is returned.
func (m *manager) Accept(ctx *context.T) (flow.Flow, error) {
	// TODO(suharshs): Ensure that m is attached to ctx.
	item, err := m.q.Get(ctx.Done())
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, flow.NewErrNetwork(ctx, NewErrManagerClosed(ctx))
	case err != nil:
		return nil, flow.NewErrNetwork(ctx, NewErrAcceptFailed(ctx, err))
	default:
		return item.(flow.Flow), nil
	}
}

// Dial creates a Flow to the provided remote endpoint, using 'fn' to
// determine the blessings that will be sent to the remote end.
//
// To maximize re-use of connections, the Manager will also Listen on Dialed
// connections for the lifetime of the connection.
//
// The flow.Manager associated with ctx must be the receiver of the method,
// otherwise an error is returned.
func (m *manager) Dial(ctx *context.T, remote naming.Endpoint, fn flow.BlessingsForPeer) (flow.Flow, error) {
	var fh conn.FlowHandler
	if m.rid != naming.NullRoutingID {
		fh = &flowHandler{q: m.q}
	}
	return m.internalDial(ctx, remote, fn, fh)
}

func (m *manager) internalDial(ctx *context.T, remote naming.Endpoint, fn flow.BlessingsForPeer, fh conn.FlowHandler) (flow.Flow, error) {
	// Disallow making connections to ourselves.
	// TODO(suharshs): Figure out the right thing to do here. We could create a "localflow"
	// that bypasses auth and is added to the accept queue immediately.
	if remote.RoutingID() == m.rid {
		return nil, flow.NewErrBadArg(ctx, NewErrManagerDialingSelf(ctx))
	}
	// Look up the connection based on RoutingID first.
	c, err := m.cache.FindWithRoutingID(remote.RoutingID())
	if err != nil {
		return nil, flow.NewErrBadState(ctx, err)
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
			return nil, flow.NewErrResolveFailed(ctx, err)
		}
		c, err = m.cache.ReservedFind(network, address, remote.BlessingNames())
		if err != nil {
			return nil, flow.NewErrBadState(ctx, err)
		}
		defer m.cache.Unreserve(network, address, remote.BlessingNames())
	}
	if c == nil {
		flowConn, err := dial(ctx, protocol, network, address)
		if err != nil {
			return nil, flow.NewErrDialFailed(ctx, err)
		}
		// TODO(mattr): We should only pass a flowHandler to NewDialed if there
		// is a server attached to this flow manager.  Perhaps we can signal
		// "serving flow manager" by passing a 0 RID to non-serving flow managers?
		c, err = conn.NewDialed(
			ctx,
			flowConn,
			localEndpoint(flowConn, m.rid),
			remote,
			version.Supported,
			fh,
		)
		if err != nil {
			flowConn.Close()
			if verror.ErrorID(err) == message.ErrWrongProtocol.ID {
				return nil, err
			}
			return nil, flow.NewErrDialFailed(ctx, err)
		}
		if err := m.cache.Insert(c); err != nil {
			return nil, flow.NewErrBadState(ctx, err)
		}
	}
	f, err := c.Dial(ctx, fn)
	if err != nil {
		return nil, flow.NewErrDialFailed(ctx, err)
	}

	// If we are dialing out to a Proxy, we need to dial a conn on this flow, and
	// return a flow on that corresponding conn.
	if proxyConn := c; remote.RoutingID() != proxyConn.RemoteEndpoint().RoutingID() {
		c, err = conn.NewDialed(
			ctx,
			f,
			proxyConn.LocalEndpoint(),
			remote,
			version.Supported,
			fh,
		)
		if err != nil {
			proxyConn.Close(ctx, err)
			if verror.ErrorID(err) == message.ErrWrongProtocol.ID {
				return nil, err
			}
			return nil, flow.NewErrDialFailed(ctx, err)
		}
		if err := m.cache.InsertWithRoutingID(c); err != nil {
			return nil, flow.NewErrBadState(ctx, err)
		}
		f, err = c.Dial(ctx, fn)
		if err != nil {
			proxyConn.Close(ctx, err)
			return nil, flow.NewErrDialFailed(ctx, err)
		}
	}
	return f, nil
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
