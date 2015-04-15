// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package manager provides an implementation of the Manager interface defined in v.io/x/ref/profiles/internal/rpc/stream.
package manager

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/stats"
	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
	"v.io/x/ref/profiles/internal/rpc/stream/vif"
	"v.io/x/ref/profiles/internal/rpc/version"
)

const pkgPath = "v.io/x/ref/profiles/internal/rpc/stream/manager"

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
// placed inside v.io/x/ref/profiles/internal. Code outside the
// v.io/x/ref/profiles/internal/* packages should never call this method.
func InternalNew(rid naming.RoutingID) stream.Manager {
	m := &manager{
		rid:          rid,
		vifs:         vif.NewSet(),
		sessionCache: crypto.NewTLSClientSessionCache(),
		listeners:    make(map[listener]bool),
		statsName:    naming.Join("rpc", "stream", "routing-id", rid.String(), "debug"),
	}
	stats.NewStringFunc(m.statsName, m.DebugString)
	return m
}

type manager struct {
	rid          naming.RoutingID
	vifs         *vif.Set
	sessionCache crypto.TLSClientSessionCache

	muListeners sync.Mutex
	listeners   map[listener]bool // GUARDED_BY(muListeners)
	shutdown    bool              // GUARDED_BY(muListeners)

	statsName string
}

var _ stream.Manager = (*manager)(nil)

type DialTimeout time.Duration

func (DialTimeout) RPCStreamVCOpt() {}
func (DialTimeout) RPCClientOpt()   {}

func dial(network, address string, timeout time.Duration) (net.Conn, error) {
	if d, _, _ := rpc.RegisteredProtocol(network); d != nil {
		conn, err := d(network, address, timeout)
		if err != nil {
			return nil, verror.New(stream.ErrNetwork, nil, err)
		}
		return conn, nil
	}
	return nil, verror.New(stream.ErrBadArg, nil, verror.New(errUnknownNetwork, nil, network))
}

// FindOrDialVIF returns the network connection (VIF) to the provided address
// from the cache in the manager. If not already present in the cache, a new
// connection will be created using net.Dial.
func (m *manager) FindOrDialVIF(remote naming.Endpoint, principal security.Principal, opts ...stream.VCOpt) (*vif.VIF, error) {
	// Extract options.
	var timeout time.Duration
	for _, o := range opts {
		switch v := o.(type) {
		case DialTimeout:
			timeout = time.Duration(v)
		}
	}
	addr := remote.Addr()
	network, address := addr.Network(), addr.String()
	if vf := m.vifs.Find(network, address); vf != nil {
		return vf, nil
	}
	vlog.VI(1).Infof("(%q, %q) not in VIF cache. Dialing", network, address)
	conn, err := dial(network, address, timeout)
	if err != nil {
		return nil, err
	}
	// (network, address) in the endpoint might not always match up
	// with the key used in the vifs. For example:
	// - conn, err := net.Dial("tcp", "www.google.com:80")
	//   fmt.Println(conn.RemoteAddr()) // Might yield the corresponding IP address
	// - Similarly, an unspecified IP address (net.IP.IsUnspecified) like "[::]:80"
	//   might yield "[::1]:80" (loopback interface) in conn.RemoteAddr().
	// Thus, look for VIFs with the resolved address as well.
	if vf := m.vifs.Find(conn.RemoteAddr().Network(), conn.RemoteAddr().String()); vf != nil {
		vlog.VI(1).Infof("(%q, %q) resolved to (%q, %q) which exists in the VIF cache. Closing newly Dialed connection", network, address, conn.RemoteAddr().Network(), conn.RemoteAddr())
		conn.Close()
		return vf, nil
	}
	vRange := version.SupportedRange
	if ep, ok := remote.(*inaming.Endpoint); ok {
		epRange := &version.Range{Min: ep.MinRPCVersion, Max: ep.MaxRPCVersion}
		if r, err := vRange.Intersect(epRange); err == nil {
			vRange = r
		}
	}
	opts = append([]stream.VCOpt{vc.StartTimeout{defaultStartTimeout}}, opts...)
	vf, err := vif.InternalNewDialedVIF(conn, m.rid, principal, vRange, m.deleteVIF, opts...)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// TODO(ashankar): If two goroutines are simultaneously invoking
	// manager.Dial, it is possible that two VIFs are inserted into m.vifs
	// for the same remote network address. This is normally not a problem,
	// but can be troublesome if the remote endpoint corresponds to a
	// proxy, since the proxy requires a single network connection per
	// routing id. Figure out a way to handle this cleanly. One option is
	// to have only a single VIF per remote network address - have to think
	// that through.
	m.vifs.Insert(vf)
	return vf, nil
}

func (m *manager) Dial(remote naming.Endpoint, principal security.Principal, opts ...stream.VCOpt) (stream.VC, error) {
	// If vif.Dial fails because the cached network connection was broken, remove from
	// the cache and try once more.
	for retry := true; true; retry = false {
		vf, err := m.FindOrDialVIF(remote, principal, opts...)
		if err != nil {
			return nil, err
		}
		opts = append([]stream.VCOpt{m.sessionCache, vc.IdleTimeout{defaultIdleTimeout}}, opts...)
		vc, err := vf.Dial(remote, principal, opts...)
		if !retry || verror.ErrorID(err) != stream.ErrAborted.ID {
			return vc, err
		}
		vf.Close()
	}
	return nil, verror.NewErrInternal(nil) // Not reached
}

func listen(protocol, address string) (net.Listener, error) {
	if _, l, _ := rpc.RegisteredProtocol(protocol); l != nil {
		ln, err := l(protocol, address)
		if err != nil {
			return nil, verror.New(stream.ErrNetwork, nil, err)
		}
		return ln, nil
	}
	return nil, verror.New(stream.ErrBadArg, nil, verror.New(errUnknownNetwork, nil, protocol))
}

func (m *manager) Listen(protocol, address string, principal security.Principal, blessings security.Blessings, opts ...stream.ListenerOpt) (stream.Listener, naming.Endpoint, error) {
	bNames, err := extractBlessingNames(principal, blessings)
	if err != nil {
		return nil, nil, err
	}
	ln, ep, err := m.internalListen(protocol, address, principal, blessings, opts...)
	if err != nil {
		return nil, nil, err
	}
	ep.Blessings = bNames
	return ln, ep, nil
}

func (m *manager) internalListen(protocol, address string, principal security.Principal, blessings security.Blessings, opts ...stream.ListenerOpt) (stream.Listener, *inaming.Endpoint, error) {
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
		return m.remoteListen(ep, principal, opts)
	}
	netln, err := listen(protocol, address)
	if err != nil {
		return nil, nil, err
	}

	m.muListeners.Lock()
	if m.shutdown {
		m.muListeners.Unlock()
		closeNetListener(netln)
		return nil, nil, verror.New(stream.ErrBadState, nil, verror.New(errAlreadyShutdown, nil))
	}

	ln := newNetListener(m, netln, principal, blessings, opts)
	m.listeners[ln] = true
	m.muListeners.Unlock()
	return ln, version.Endpoint(protocol, netln.Addr().String(), m.rid), nil
}

func (m *manager) remoteListen(proxy naming.Endpoint, principal security.Principal, listenerOpts []stream.ListenerOpt) (stream.Listener, *inaming.Endpoint, error) {
	ln, ep, err := newProxyListener(m, proxy, principal, listenerOpts)
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

func (m *manager) deleteVIF(vf *vif.VIF) {
	vlog.VI(2).Infof("%p: VIF %v is closed, removing from cache", m, vf)
	m.vifs.Delete(vf)
}

func (m *manager) ShutdownEndpoint(remote naming.Endpoint) {
	vifs := m.vifs.List()
	total := 0
	for _, vf := range vifs {
		total += vf.ShutdownVCs(remote)
	}
	vlog.VI(1).Infof("ShutdownEndpoint(%q) closed %d VCs", remote, total)
}

func closeNetListener(ln net.Listener) {
	addr := ln.Addr()
	err := ln.Close()
	vlog.VI(1).Infof("Closed net.Listener on (%q, %q): %v", addr.Network(), addr, err)
}

func (m *manager) removeListener(ln listener) {
	m.muListeners.Lock()
	delete(m.listeners, ln)
	m.muListeners.Unlock()
}

func (m *manager) Shutdown() {
	stats.Delete(m.statsName)
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
	var ret []string
	for b, _ := range p.BlessingsInfo(b) {
		ret = append(ret, b)
	}
	if len(ret) == 0 {
		return nil, verror.New(stream.ErrBadArg, nil, verror.New(errNoBlessingNames, nil))
	}
	return ret, nil
}
