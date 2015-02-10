// Package manager provides an implementation of the Manager interface defined in veyron/runtimes/google/ipc/stream.
package manager

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/stats"
	"v.io/core/veyron/runtimes/google/ipc/stream"
	"v.io/core/veyron/runtimes/google/ipc/stream/crypto"
	"v.io/core/veyron/runtimes/google/ipc/stream/vif"
	"v.io/core/veyron/runtimes/google/ipc/version"
	inaming "v.io/core/veyron/runtimes/google/naming"
)

var errShutDown = errors.New("manager has been shut down")

// InternalNew creates a new stream.Manager for managing streams where the local
// process is identified by the provided RoutingID.
//
// As the name suggests, this method is intended for use only within packages
// placed inside veyron/runtimes/google. Code outside the
// veyron/runtimes/google/* packages should never call this method.
func InternalNew(rid naming.RoutingID) stream.Manager {
	m := &manager{
		rid:          rid,
		vifs:         vif.NewSet(),
		sessionCache: crypto.NewTLSClientSessionCache(),
		listeners:    make(map[listener]bool),
		statsName:    naming.Join("ipc", "stream", "routing-id", rid.String(), "debug"),
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

type DialTimeout struct{ time.Duration }

func (DialTimeout) IPCStreamVCOpt() {}
func (DialTimeout) IPCClientOpt()   {}

func dial(network, address string, timeout time.Duration) (net.Conn, error) {
	if d, _ := ipc.RegisteredProtocol(network); d != nil {
		return d(network, address, timeout)
	}
	return nil, fmt.Errorf("unknown network %s", network)
}

// FindOrDialVIF returns the network connection (VIF) to the provided address
// from the cache in the manager. If not already present in the cache, a new
// connection will be created using net.Dial.
func (m *manager) FindOrDialVIF(remote naming.Endpoint, opts ...stream.VCOpt) (*vif.VIF, error) {
	// Extract options.
	var timeout time.Duration
	for _, o := range opts {
		switch v := o.(type) {
		case *DialTimeout:
			timeout = v.Duration
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
		return nil, fmt.Errorf("net.Dial(%q, %q) failed: %v", network, address, err)
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
		epRange := &version.Range{Min: ep.MinIPCVersion, Max: ep.MaxIPCVersion}
		if r, err := vRange.Intersect(epRange); err == nil {
			vRange = r
		}
	}
	vf, err := vif.InternalNewDialedVIF(conn, m.rid, vRange, opts...)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create VIF: %v", err)
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

func (m *manager) Dial(remote naming.Endpoint, opts ...stream.VCOpt) (stream.VC, error) {
	// If vif.Dial fails because the cached network connection was broken, remove from
	// the cache and try once more.
	for retry := true; true; retry = false {
		vf, err := m.FindOrDialVIF(remote, opts...)
		if err != nil {
			return nil, err
		}
		vc, err := vf.Dial(remote, append(opts, m.sessionCache)...)
		if !retry || verror.ErrorID(err) != verror.Aborted.ID {
			return vc, err
		}
		vf.Close()
		m.vifs.Delete(vf)
		vlog.VI(2).Infof("VIF %v is closed, removing from cache", vf)
	}
	return nil, verror.MakeInternal(nil) // Not reached
}

func listen(protocol, address string) (net.Listener, error) {
	if _, l := ipc.RegisteredProtocol(protocol); l != nil {
		return l(protocol, address)
	}
	return nil, fmt.Errorf("unknown network %s", protocol)
}

func (m *manager) Listen(protocol, address string, opts ...stream.ListenerOpt) (stream.Listener, naming.Endpoint, error) {
	m.muListeners.Lock()
	if m.shutdown {
		m.muListeners.Unlock()
		return nil, nil, errShutDown
	}
	m.muListeners.Unlock()

	if protocol == inaming.Network {
		// Act as if listening on the address of a remote proxy.
		ep, err := inaming.NewEndpoint(address)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse endpoint %q: %v", address, err)
		}
		return m.remoteListen(ep, opts)
	}
	netln, err := listen(protocol, address)
	if err != nil {
		return nil, nil, fmt.Errorf("net.Listen(%q, %q) failed: %v", protocol, address, err)
	}

	m.muListeners.Lock()
	if m.shutdown {
		m.muListeners.Unlock()
		closeNetListener(netln)
		return nil, nil, errShutDown
	}

	ln := newNetListener(m, netln, opts)
	m.listeners[ln] = true
	m.muListeners.Unlock()
	ep := version.Endpoint(protocol, netln.Addr().String(), m.rid)
	return ln, ep, nil
}

func (m *manager) remoteListen(proxy naming.Endpoint, listenerOpts []stream.ListenerOpt) (stream.Listener, naming.Endpoint, error) {
	ln, ep, err := newProxyListener(m, proxy, listenerOpts)
	if err != nil {
		return nil, nil, err
	}
	m.muListeners.Lock()
	defer m.muListeners.Unlock()
	if m.shutdown {
		ln.Close()
		return nil, nil, errShutDown
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
