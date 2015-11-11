// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package manager provides an implementation of the Manager interface defined in v.io/x/ref/runtime/internal/rpc/stream.
package manager

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/ref/lib/apilog"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/lib/stats/counter"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	"v.io/x/ref/runtime/internal/rpc/stream/vif"
)

const pkgPath = "v.io/x/ref/runtime/internal/rpc/stream/manager"

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errUnknownNetwork                          = reg(".errUnknownNetwork", "unknown network{:3}")
	errEndpointParseError                      = reg(".errEndpointParseError", "failed to parse endpoint {3}{:4}")
	errAlreadyShutdown                         = reg(".errAlreadyShutdown", "already shutdown")
	errProvidedServerBlessingsWithoutPrincipal = reg(".errServerBlessingsWithoutPrincipal", "blessings provided but with no principal")
	errNoBlessingNames                         = reg(".errNoBlessingNames", "no blessing names could be extracted for the provided principal")
)

const (
	// The default time after which an VIF is closed if no VC is opened.
	defaultStartTimeout = 3 * time.Second
	// The default time after which an idle VC is closed.
	defaultIdleTimeout = 30 * time.Second
)

// InternalNew creates a new stream.Manager for managing streams where the local
// process is identified by the provided RoutingID.
//
// As the name suggests, this method is intended for use only within packages
// placed inside v.io/x/ref/runtime/internal. Code outside the
// v.io/x/ref/runtime/internal/* packages should never call this method.
func InternalNew(ctx *context.T, rid naming.RoutingID) stream.Manager {
	statsPrefix := naming.Join("rpc", "stream", "routing-id", rid.String())
	m := &manager{
		ctx:         ctx,
		rid:         rid,
		vifs:        vif.NewSet(),
		listeners:   make(map[listener]bool),
		statsPrefix: statsPrefix,
		killedConns: stats.NewCounter(naming.Join(statsPrefix, "killed-connections")),
	}
	stats.NewStringFunc(naming.Join(m.statsPrefix, "debug"), m.DebugString)
	return m
}

type manager struct {
	ctx  *context.T
	rid  naming.RoutingID
	vifs *vif.Set

	muListeners sync.Mutex
	listeners   map[listener]bool // GUARDED_BY(muListeners)
	shutdown    bool              // GUARDED_BY(muListeners)

	statsPrefix string
	killedConns *counter.Counter
}

var _ stream.Manager = (*manager)(nil)

type DialTimeout time.Duration

func (DialTimeout) RPCStreamVCOpt() {}
func (DialTimeout) RPCClientOpt() {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
}

func dial(ctx *context.T, d rpc.DialerFunc, network, address string, timeout time.Duration) (net.Conn, error) {
	if d != nil {
		conn, err := d(ctx, network, address, timeout)
		if err != nil {
			return nil, verror.New(stream.ErrDialFailed, ctx, err)
		}
		return conn, nil
	}
	return nil, verror.New(stream.ErrDialFailed, ctx, verror.New(errUnknownNetwork, ctx, network))
}

func resolve(ctx *context.T, r rpc.ResolverFunc, network, address string) (string, string, error) {
	if r != nil {
		net, addr, err := r(ctx, network, address)
		if err != nil {
			return "", "", verror.New(stream.ErrResolveFailed, ctx, err)
		}
		return net, addr, nil
	}
	return "", "", verror.New(stream.ErrResolveFailed, ctx, verror.New(errUnknownNetwork, ctx, network))
}

type dialResult struct {
	conn net.Conn
	err  error
}

// FindOrDialVIF returns the network connection (VIF) to the provided address
// from the cache in the manager. If not already present in the cache, a new
// connection will be created using net.Dial.
func (m *manager) FindOrDialVIF(ctx *context.T, remote naming.Endpoint, opts ...stream.VCOpt) (*vif.VIF, error) {
	// Extract options.
	var timeout time.Duration
	for _, o := range opts {
		switch v := o.(type) {
		case DialTimeout:
			timeout = time.Duration(v)
		}
	}
	addr := remote.Addr()
	d, r, _, _ := rpc.RegisteredProtocol(addr.Network())
	// (network, address) in the endpoint might not always match up
	// with the key used in the vifs. For example:
	// - conn, err := net.Dial("tcp", "www.google.com:80")
	//   fmt.Println(conn.RemoteAddr()) // Might yield the corresponding IP address
	// - Similarly, an unspecified IP address (net.IP.IsUnspecified) like "[::]:80"
	//   might yield "[::1]:80" (loopback interface) in conn.RemoteAddr().
	// Thus, look for VIFs with the resolved address.
	network, address, err := resolve(ctx, r, addr.Network(), addr.String())
	if err != nil {
		return nil, err
	}
	vf, unblock := m.vifs.BlockingFind(network, address)
	if vf != nil {
		ctx.VI(1).Infof("(%q, %q) resolved to (%q, %q) which exists in the VIF cache.", addr.Network(), addr.String(), network, address)
		return vf, nil
	}
	defer unblock()

	ctx.VI(1).Infof("(%q, %q) not in VIF cache. Dialing", network, address)

	ch := make(chan *dialResult, 1)
	go func() {
		conn, err := dial(ctx, d, network, address, timeout)
		ch <- &dialResult{conn, err}
	}()

	var conn net.Conn
	select {
	case result := <-ch:
		conn, err = result.conn, result.err
	case <-ctx.Done():
		go func() {
			result := <-ch
			if result.conn != nil {
				result.conn.Close()
			}
		}()
		return nil, verror.New(verror.ErrTimeout, ctx)
	}
	if err != nil {
		return nil, err
	}

	opts = append([]stream.VCOpt{vc.StartTimeout{Duration: defaultStartTimeout}}, opts...)
	type vfAndErr struct {
		vf  *vif.VIF
		err error
	}
	vch := make(chan vfAndErr, 1)
	go func() {
		vf, err := vif.InternalNewDialedVIF(ctx, conn, m.rid, nil, m.deleteVIF, opts...)
		vch <- vfAndErr{vf, err}
	}()
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case vae := <-vch:
		vf, err = vae.vf, vae.err
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	m.vifs.Insert(vf, network, address)
	return vf, nil
}

func (m *manager) Dial(ctx *context.T, remote naming.Endpoint, opts ...stream.VCOpt) (stream.VC, error) {
	// If vif.Dial fails because the cached network connection was broken, remove from
	// the cache and try once more.
	for retry := true; true; retry = false {
		vf, err := m.FindOrDialVIF(ctx, remote, opts...)
		if err != nil {
			return nil, err
		}
		opts = append([]stream.VCOpt{vc.IdleTimeout{Duration: defaultIdleTimeout}}, opts...)
		vc, err := vf.Dial(ctx, remote, opts...)
		if !retry || verror.ErrorID(err) != stream.ErrAborted.ID {
			return vc, err
		}
		vf.Close()
	}
	return nil, verror.NewErrInternal(nil) // Not reached
}

func (m *manager) deleteVIF(vf *vif.VIF) {
	m.ctx.VI(2).Infof("%p: VIF %v is closed, removing from cache", m, vf)
	m.vifs.Delete(vf)
}

func listen(ctx *context.T, protocol, address string) (net.Listener, error) {
	if _, _, l, _ := rpc.RegisteredProtocol(protocol); l != nil {
		ln, err := l(ctx, protocol, address)
		if err != nil {
			return nil, verror.New(stream.ErrNetwork, ctx, err)
		}
		return ln, nil
	}
	return nil, verror.New(stream.ErrBadArg, ctx, verror.New(errUnknownNetwork, ctx, protocol))
}

func (m *manager) Listen(ctx *context.T, protocol, address string, blessings security.Blessings, opts ...stream.ListenerOpt) (stream.Listener, naming.Endpoint, error) {
	principal := stream.GetPrincipalListenerOpts(ctx, opts...)
	bNames, err := extractBlessingNames(principal, blessings)
	if err != nil {
		return nil, nil, err
	}
	ln, ep, err := m.internalListen(ctx, protocol, address, blessings, opts...)
	if err != nil {
		return nil, nil, err
	}
	ep.Blessings = bNames
	return ln, ep, nil
}

func (m *manager) internalListen(ctx *context.T, protocol, address string, blessings security.Blessings, opts ...stream.ListenerOpt) (stream.Listener, *inaming.Endpoint, error) {
	m.muListeners.Lock()
	if m.shutdown {
		m.muListeners.Unlock()
		return nil, nil, verror.New(stream.ErrBadState, nil, verror.New(errAlreadyShutdown, nil))
	}
	m.muListeners.Unlock()

	if protocol == inaming.Network {
		// Act as if listening on the address of a remote proxy.
		ep, err := inaming.NewEndpoint(address)
		if err != nil {
			return nil, nil, verror.New(stream.ErrBadArg, nil, verror.New(errEndpointParseError, nil, address, err))
		}
		return m.remoteListen(ctx, ep, opts)
	}
	netln, err := listen(ctx, protocol, address)
	if err != nil {
		return nil, nil, err
	}

	m.muListeners.Lock()
	if m.shutdown {
		m.muListeners.Unlock()
		closeNetListener(ctx, netln)
		return nil, nil, verror.New(stream.ErrBadState, nil, verror.New(errAlreadyShutdown, nil))
	}

	ln := newNetListener(ctx, m, netln, blessings, opts)
	m.listeners[ln] = true
	m.muListeners.Unlock()
	ep := &inaming.Endpoint{
		Protocol: protocol,
		Address:  netln.Addr().String(),
		RID:      m.rid,
	}
	return ln, ep, nil
}

func (m *manager) remoteListen(ctx *context.T, proxy naming.Endpoint, listenerOpts []stream.ListenerOpt) (stream.Listener, *inaming.Endpoint, error) {
	ln, ep, err := newProxyListener(ctx, m, proxy, listenerOpts)
	if err != nil {
		return nil, nil, err
	}
	m.muListeners.Lock()
	defer m.muListeners.Unlock()
	if m.shutdown {
		ln.Close()
		return nil, nil, verror.New(stream.ErrBadState, nil, verror.New(errAlreadyShutdown, nil))
	}
	m.listeners[ln] = true
	return ln, ep, nil
}

func (m *manager) ShutdownEndpoint(remote naming.Endpoint) {
	vifs := m.vifs.List()
	total := 0
	for _, vf := range vifs {
		total += vf.ShutdownVCs(remote)
	}
	m.ctx.VI(1).Infof("ShutdownEndpoint(%q) closed %d VCs", remote, total)
}

func closeNetListener(ctx *context.T, ln net.Listener) {
	addr := ln.Addr()
	err := ln.Close()
	ctx.VI(1).Infof("Closed net.Listener on (%q, %q): %v", addr.Network(), addr, err)
}

func (m *manager) removeListener(ln listener) {
	m.muListeners.Lock()
	delete(m.listeners, ln)
	m.muListeners.Unlock()
}

func (m *manager) Shutdown() {
	stats.Delete(m.statsPrefix)
	m.muListeners.Lock()
	if m.shutdown {
		m.muListeners.Unlock()
		return
	}
	m.shutdown = true
	var wg sync.WaitGroup
	wg.Add(len(m.listeners))
	for ln, _ := range m.listeners {
		go func(ln stream.Listener) {
			ln.Close()
			wg.Done()
		}(ln)
	}
	m.listeners = make(map[listener]bool)
	m.muListeners.Unlock()
	wg.Wait()

	vifs := m.vifs.List()
	for _, vf := range vifs {
		vf.Close()
	}
}

func (m *manager) RoutingID() naming.RoutingID {
	return m.rid
}

func (m *manager) DebugString() string {
	vifs := m.vifs.List()

	m.muListeners.Lock()
	defer m.muListeners.Unlock()

	l := make([]string, 0)
	l = append(l, fmt.Sprintf("Manager: RoutingID:%v #VIFs:%d #Listeners:%d Shutdown:%t", m.rid, len(vifs), len(m.listeners), m.shutdown))
	if len(vifs) > 0 {
		l = append(l, "============================VIFs================================================")
		for ix, vif := range vifs {
			l = append(l, fmt.Sprintf("%4d) %v", ix, vif.DebugString()))
			l = append(l, "--------------------------------------------------------------------------------")
		}
	}
	if len(m.listeners) > 0 {
		l = append(l, "=======================================Listeners==================================================")
		l = append(l, "  (stream listeners, their local network listeners (missing for proxied listeners), and VIFS")
		for ln, _ := range m.listeners {
			l = append(l, ln.DebugString())
		}
	}
	return strings.Join(l, "\n")
}

func extractBlessingNames(p security.Principal, b security.Blessings) ([]string, error) {
	if !b.IsZero() && p == nil {
		return nil, verror.New(stream.ErrBadArg, nil, verror.New(errProvidedServerBlessingsWithoutPrincipal, nil))
	}
	if p == nil {
		return nil, nil
	}
	ret := security.BlessingNames(p, b)
	if len(ret) == 0 {
		return nil, verror.New(stream.ErrBadArg, nil, verror.New(errNoBlessingNames, nil))
	}
	return ret, nil
}
