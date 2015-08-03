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

	"v.io/x/ref/runtime/internal/flow/conn"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/version"
)

type manager struct {
	rid    naming.RoutingID
	closed <-chan struct{}
	q      *upcqueue.T

	mu              *sync.Mutex
	listenEndpoints []naming.Endpoint
}

func New(ctx *context.T, rid naming.RoutingID) flow.Manager {
	m := &manager{
		rid:    rid,
		closed: ctx.Done(),
		mu:     &sync.Mutex{},
		q:      upcqueue.New(),
	}
	return m
}

// Listen causes the Manager to accept flows from the provided protocol and address.
// Listen may be called muliple times.
//
// The flow.Manager associated with ctx must be the receiver of the method,
// otherwise an error is returned.
func (m *manager) Listen(ctx *context.T, protocol, address string) error {
	netLn, err := listen(ctx, protocol, address)
	if err != nil {
		return flow.NewErrNetwork(ctx, err)
	}
	local := &inaming.Endpoint{
		Protocol: protocol,
		Address:  netLn.Addr().String(),
		RID:      m.rid,
	}
	m.mu.Lock()
	m.listenEndpoints = append(m.listenEndpoints, local)
	m.mu.Unlock()
	go m.netLnAcceptLoop(ctx, netLn, local)
	return nil
}

func (m *manager) netLnAcceptLoop(ctx *context.T, netLn net.Listener, local naming.Endpoint) {
	const killConnectionsRetryDelay = 5 * time.Millisecond
	for {
		netConn, err := netLn.Accept()
		for tokill := 1; isTemporaryError(err); tokill *= 2 {
			if isTooManyOpenFiles(err) {
				// TODO(suharshs): Find a way to kill connections here. We will need
				// caching to be able to delete the connections.
			} else {
				tokill = 1
			}
			time.Sleep(killConnectionsRetryDelay)
			netConn, err = netLn.Accept()
		}
		if err != nil {
			ctx.VI(2).Infof("net.Listener.Accept on localEP %v failed: %v", local, err)
		}
		// TODO(suharshs): This conn needs to be cached instead of ignored.
		_, err = conn.NewAccepted(
			ctx,
			&framer{ReadWriter: netConn},
			local,
			v23.GetPrincipal(ctx).BlessingStore().Default(),
			version.Supported,
			&flowHandler{q: m.q, closed: m.closed},
		)
		if err != nil {
			netConn.Close()
			ctx.VI(2).Infof("failed to accept flow.Conn on localEP %v failed: %v", local, err)
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
	// TODO(suharshs): Add caching of connections.
	addr := remote.Addr()
	d, _, _, _ := rpc.RegisteredProtocol(addr.Network())
	netConn, err := dial(ctx, d, addr.Network(), addr.String())
	if err != nil {
		return nil, flow.NewErrDialFailed(ctx, err)
	}

	c, err := conn.NewDialed(
		ctx,
		&framer{ReadWriter: netConn}, // TODO(suharshs): Don't frame if the net.Conn already has framing in its protocol.
		localEndpoint(netConn, m.rid),
		remote,
		version.Supported,
		&flowHandler{q: m.q, closed: m.closed},
		fn,
	)
	if err != nil {
		return nil, flow.NewErrDialFailed(ctx, err)
	}
	return c.Dial(ctx)
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
		return d(protocol, address, timeout)
	}
	return nil, NewErrUnknownProtocol(ctx, protocol)
}

func resolve(ctx *context.T, r rpc.ResolverFunc, protocol, address string) (string, string, error) {
	if r != nil {
		net, addr, err := r(protocol, address)
		if err != nil {
			return "", "", err
		}
		return net, addr, nil
	}
	return "", "", NewErrUnknownProtocol(ctx, protocol)
}

func listen(ctx *context.T, protocol, address string) (net.Listener, error) {
	if _, _, l, _ := rpc.RegisteredProtocol(protocol); l != nil {
		ln, err := l(protocol, address)
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
