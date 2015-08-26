// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vom"

	"v.io/x/ref/runtime/internal/flow/conn"
	"v.io/x/ref/runtime/internal/lib/framer"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/version"
)

type manager struct {
	rid    naming.RoutingID
	closed <-chan struct{}
	q      *upcqueue.T
	cache  *ConnCache

	mu              *sync.Mutex
	listenEndpoints []naming.Endpoint
}

func New(ctx *context.T, rid naming.RoutingID) flow.Manager {
	m := &manager{
		rid:    rid,
		closed: ctx.Done(),
		q:      upcqueue.New(),
		cache:  NewConnCache(),
		mu:     &sync.Mutex{},
	}
	return m
}

// Listen causes the Manager to accept flows from the provided protocol and address.
// Listen may be called muliple times.
//
// The flow.Manager associated with ctx must be the receiver of the method,
// otherwise an error is returned.
func (m *manager) Listen(ctx *context.T, protocol, address string) error {
	var (
		ep  naming.Endpoint
		err error
	)
	if protocol == inaming.Network {
		ep, err = m.proxyListen(ctx, address)
	} else {
		ep, err = m.listen(ctx, protocol, address)
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.listenEndpoints = append(m.listenEndpoints, ep)
	m.mu.Unlock()
	return nil
}

func (m *manager) listen(ctx *context.T, protocol, address string) (naming.Endpoint, error) {
	netLn, err := listen(ctx, protocol, address)
	if err != nil {
		return nil, flow.NewErrNetwork(ctx, err)
	}
	local := &inaming.Endpoint{
		Protocol: protocol,
		Address:  netLn.Addr().String(),
		RID:      m.rid,
	}
	go m.netLnAcceptLoop(ctx, netLn, local)
	return local, nil
}

func (m *manager) proxyListen(ctx *context.T, address string) (naming.Endpoint, error) {
	ep, err := inaming.NewEndpoint(address)
	if err != nil {
		return nil, flow.NewErrBadArg(ctx, err)
	}
	f, err := m.internalDial(ctx, ep, proxyBlessingsForPeer{}.run, &proxyFlowHandler{ctx: ctx, m: m})
	if err != nil {
		return nil, flow.NewErrNetwork(ctx, err)
	}
	// Write to ensure we send an openFlow message.
	if _, err := f.Write([]byte{0}); err != nil {
		return nil, flow.NewErrNetwork(ctx, err)
	}
	var lep string
	if err := vom.NewDecoder(f).Decode(&lep); err != nil {
		return nil, flow.NewErrNetwork(ctx, err)
	}
	return inaming.NewEndpoint(lep)
}

type proxyBlessingsForPeer struct{}

// TODO(suharshs): Figure out what blessings to present here. And present discharges.
func (proxyBlessingsForPeer) run(ctx *context.T, lep, rep naming.Endpoint, rb security.Blessings,
	rd map[string]security.Discharge) (security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}

func (m *manager) netLnAcceptLoop(ctx *context.T, netLn net.Listener, local naming.Endpoint) {
	const killConnectionsRetryDelay = 5 * time.Millisecond
	for {
		netConn, err := netLn.Accept()
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
			netConn, err = netLn.Accept()
		}
		if err != nil {
			ctx.Errorf("net.Listener.Accept on localEP %v failed: %v", local, err)
			continue
		}
		c, err := conn.NewAccepted(
			ctx,
			framer.New(netConn),
			local,
			version.Supported,
			&flowHandler{q: m.q, closed: m.closed},
		)
		if err != nil {
			netConn.Close()
			ctx.Errorf("failed to accept flow.Conn on localEP %v failed: %v", local, err)
			continue
		}
		if err := m.cache.Insert(c); err != nil {
			ctx.VI(2).Infof("failed to cache conn %v: %v", c, err)
		}
	}
}

type flowHandler struct {
	q      *upcqueue.T
	closed <-chan struct{}
}

func (h *flowHandler) HandleFlow(f flow.Flow) error {
	select {
	case <-h.closed:
		// This will make the Put call below return a upcqueue.ErrQueueIsClosed.
		h.q.Close()
	default:
	}
	return h.q.Put(f)
}

type proxyFlowHandler struct {
	ctx *context.T
	m   *manager
}

func (h *proxyFlowHandler) HandleFlow(f flow.Flow) error {
	select {
	case <-h.m.closed:
		h.m.q.Close()
		return upcqueue.ErrQueueIsClosed
	default:
	}
	go func() {
		c, err := conn.NewAccepted(
			h.ctx,
			closer{f},
			f.Conn().LocalEndpoint(),
			version.Supported,
			&flowHandler{q: h.m.q, closed: h.m.closed})
		if err != nil {
			h.ctx.Errorf("failed to create accepted conn: %v", err)
			return
		}
		if err := h.m.cache.Insert(c); err != nil {
			h.ctx.Errorf("failed to create accepted conn: %v", err)
			return
		}
	}()
	return nil
}

// ListeningEndpoints returns the endpoints that the Manager has explicitly
// listened on. The Manager will accept new flows on these endpoints.
// Returned endpoints all have a RoutingID unique to the Acceptor.
func (m *manager) ListeningEndpoints() []naming.Endpoint {
	m.mu.Lock()
	ret := make([]naming.Endpoint, len(m.listenEndpoints))
	copy(ret, m.listenEndpoints)
	m.mu.Unlock()
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
	item, err := m.q.Get(m.closed)
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
	return m.internalDial(ctx, remote, fn, &flowHandler{q: m.q, closed: m.closed})
}

func (m *manager) internalDial(ctx *context.T, remote naming.Endpoint, fn flow.BlessingsForPeer, fh conn.FlowHandler) (flow.Flow, error) {
	// Look up the connection based on RoutingID first.
	c, err := m.cache.FindWithRoutingID(remote.RoutingID())
	if err != nil {
		return nil, flow.NewErrBadState(ctx, err)
	}
	var (
		d                rpc.DialerFunc
		network, address string
	)
	if c == nil {
		addr := remote.Addr()
		var r rpc.ResolverFunc
		d, r, _, _ = rpc.RegisteredProtocol(addr.Network())
		// (network, address) in the endpoint might not always match up
		// with the key used for caching conns. For example:
		// - conn, err := net.Dial("tcp", "www.google.com:80")
		//   fmt.Println(conn.RemoteAddr()) // Might yield the corresponding IP address
		// - Similarly, an unspecified IP address (net.IP.IsUnspecified) like "[::]:80"
		//   might yield "[::1]:80" (loopback interface) in conn.RemoteAddr().
		// Thus we look for Conns with the resolved address.
		network, address, err = resolve(ctx, r, addr.Network(), addr.String())
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
		netConn, err := dial(ctx, d, network, address)
		if err != nil {
			return nil, flow.NewErrDialFailed(ctx, err)
		}
		// TODO(mattr): We should only pass a flowHandler to NewDialed if there
		// is a server attached to this flow manager.  Perhaps we can signal
		// "serving flow manager" by passing a 0 RID to non-serving flow managers?
		c, err = conn.NewDialed(
			ctx,
			framer.New(netConn), // TODO(suharshs): Don't frame if the net.Conn already has framing in its protocol.
			localEndpoint(netConn, m.rid),
			remote,
			version.Supported,
			fh,
		)
		if err != nil {
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
	if remote.RoutingID() != c.RemoteEndpoint().RoutingID() {
		c, err = conn.NewDialed(
			ctx,
			closer{f},
			c.LocalEndpoint(),
			remote,
			version.Supported,
			fh,
		)
		if err != nil {
			return nil, flow.NewErrDialFailed(ctx, err)
		}
		f, err = c.Dial(ctx, fn)
		if err != nil {
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

func dial(ctx *context.T, d rpc.DialerFunc, protocol, address string) (net.Conn, error) {
	if d != nil {
		var timeout time.Duration
		if dl, ok := ctx.Deadline(); ok {
			timeout = dl.Sub(time.Now())
		}
		return d(ctx, protocol, address, timeout)
	}
	return nil, NewErrUnknownProtocol(ctx, protocol)
}

func resolve(ctx *context.T, r rpc.ResolverFunc, protocol, address string) (string, string, error) {
	if r != nil {
		net, addr, err := r(ctx, protocol, address)
		if err != nil {
			return "", "", err
		}
		return net, addr, nil
	}
	return "", "", NewErrUnknownProtocol(ctx, protocol)
}

func listen(ctx *context.T, protocol, address string) (net.Listener, error) {
	if _, _, l, _ := rpc.RegisteredProtocol(protocol); l != nil {
		ln, err := l(ctx, protocol, address)
		if err != nil {
			return nil, err
		}
		return ln, nil
	}
	return nil, NewErrUnknownProtocol(ctx, protocol)
}

func localEndpoint(conn net.Conn, rid naming.RoutingID) naming.Endpoint {
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

// TODO(suharshs): should we add Close method to the Flow API?
type closer struct {
	flow.Flow
}

func (c closer) Close() error {
	return nil
}
