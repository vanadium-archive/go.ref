// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"v.io/x/ref/profiles/internal/lib/upcqueue"
	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream/proxy"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
	"v.io/x/ref/profiles/internal/rpc/stream/vif"

	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/profiles/internal/rpc/stream"
)

func reg(id, msg string) verror.IDAction {
	return verror.Register(verror.ID(pkgPath+id), verror.NoRetry, msg)
}

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errVomEncoder                 = reg(".errVomEncoder", "failed to create vom encoder{:3}")
	errVomDecoder                 = reg(".errVomDecoder", "failed to create vom decoder{:3}")
	errVomEncodeRequest           = reg(".errVomEncodeRequest", "failed to encode request to proxy{:3}")
	errVomDecodeResponse          = reg(".errVomDecodeRequest", "failed to decoded response from proxy{:3}")
	errProxyError                 = reg(".errProxyError", "proxy error {:3}")
	errProxyEndpointError         = reg(".errProxyEndpointError", "proxy returned an invalid endpoint {:3}{:4}")
	errAlreadyConnected           = reg(".errAlreadyConnected", "already connected to proxy and accepting connections? VIF: {3}, StartAccepting{:_}")
	errFailedToCreateLivenessFlow = reg(".errFailedToCreateLivenessFlow", "unable to create liveness check flow to proxy{:3}")
	errAcceptFailed               = reg(".errAcceptFailed", "accept failed{:3}")
	errFailedToEstablishVC        = reg(".errFailedToEstablishVC", "VC establishment with proxy failed{:_}")
	errListenerAlreadyClosed      = reg(".errListenerAlreadyClosed", "listener already closed")
)

// listener extends stream.Listener with a DebugString method.
type listener interface {
	stream.Listener
	DebugString() string
}

// netListener implements the listener interface by accepting flows (and VCs)
// over network connections accepted on an underlying net.Listener.
type netListener struct {
	q       *upcqueue.T
	netLn   net.Listener
	manager *manager
	vifs    *vif.Set

	connsMu sync.Mutex
	conns   map[net.Conn]bool

	netLoop  sync.WaitGroup
	vifLoops sync.WaitGroup
}

var _ stream.Listener = (*netListener)(nil)

// proxyListener implements the listener interface by connecting to a remote
// proxy (typically used to "listen" across network domains).
type proxyListener struct {
	q       *upcqueue.T
	proxyEP naming.Endpoint
	manager *manager
	vif     *vif.VIF

	vifLoop sync.WaitGroup
}

var _ stream.Listener = (*proxyListener)(nil)

func newNetListener(m *manager, netLn net.Listener, principal security.Principal, blessings security.Blessings, opts []stream.ListenerOpt) listener {
	ln := &netListener{
		q:       upcqueue.New(),
		manager: m,
		netLn:   netLn,
		vifs:    vif.NewSet(),
		conns:   make(map[net.Conn]bool),
	}

	// Set the default idle timeout for VC. But for "unixfd", we do not set
	// the idle timeout since we cannot reconnect it.
	if ln.netLn.Addr().Network() != "unixfd" {
		opts = append([]stream.ListenerOpt{vc.IdleTimeout{defaultIdleTimeout}}, opts...)
	}

	ln.netLoop.Add(1)
	go ln.netAcceptLoop(principal, blessings, opts)
	return ln
}

func isTemporaryError(err error) bool {
	if oErr, ok := err.(*net.OpError); ok && oErr.Temporary() {
		return true
	}
	return false
}

func isTooManyOpenFiles(err error) bool {
	if oErr, ok := err.(*net.OpError); ok && oErr.Err == syscall.EMFILE {
		return true
	}
	return false
}

func (ln *netListener) killConnections(n int) {
	ln.connsMu.Lock()
	if n > len(ln.conns) {
		n = len(ln.conns)
	}
	remaining := make([]net.Conn, 0, len(ln.conns))
	for c := range ln.conns {
		remaining = append(remaining, c)
	}
	removed := remaining[:n]
	ln.connsMu.Unlock()

	vlog.Infof("Killing %d Conns", n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		idx := rand.Intn(len(remaining))
		conn := remaining[idx]
		go func(conn net.Conn) {
			vlog.Infof("Killing connection (%s, %s)", conn.LocalAddr(), conn.RemoteAddr())
			conn.Close()
			ln.manager.killedConns.Incr(1)
			wg.Done()
		}(conn)
		remaining[idx], remaining[0] = remaining[0], remaining[idx]
		remaining = remaining[1:]
	}

	ln.connsMu.Lock()
	for _, conn := range removed {
		delete(ln.conns, conn)
	}
	ln.connsMu.Unlock()

	wg.Wait()
}

func (ln *netListener) netAcceptLoop(principal security.Principal, blessings security.Blessings, opts []stream.ListenerOpt) {
	defer ln.netLoop.Done()
	opts = append([]stream.ListenerOpt{vc.StartTimeout{defaultStartTimeout}}, opts...)
	for {
		conn, err := ln.netLn.Accept()
		if isTemporaryError(err) {
			// Use Info instead of Error to reduce the changes that
			// the log library will cause the process to abort on
			// failing to create a new file.
			vlog.Infof("net.Listener.Accept() failed on %v with %v", ln.netLn, err)
			for tokill := 1; isTemporaryError(err); tokill *= 2 {
				if isTooManyOpenFiles(err) {
					ln.killConnections(tokill)
				} else {
					tokill = 1
				}
				time.Sleep(10 * time.Millisecond)
				conn, err = ln.netLn.Accept()
			}
		}
		if err != nil {
			// TODO(cnicolaou): closeListener in manager.go writes to ln (by calling
			// ln.Close()) and we read it here in the Infof output, so there is
			// an unguarded read here that will fail under --race. This will only show
			// itself if the Infof below is changed to always be printed (which is
			// how I noticed). The right solution is to lock these datastructures, but
			// that can wait until a bigger overhaul occurs. For now, we leave this at
			// VI(1) knowing that it's basically harmless.
			vlog.VI(1).Infof("Exiting netAcceptLoop: net.Listener.Accept() failed on %v with %v", ln.netLn, err)
			return
		}
		ln.connsMu.Lock()
		ln.conns[conn] = true
		ln.connsMu.Unlock()

		vlog.VI(1).Infof("New net.Conn accepted from %s (local address: %s)", conn.RemoteAddr(), conn.LocalAddr())
		go func() {
			vf, err := vif.InternalNewAcceptedVIF(conn, ln.manager.rid, principal, blessings, nil, ln.deleteVIF, opts...)
			if err != nil {
				vlog.Infof("Shutting down conn from %s (local address: %s) as a VIF could not be created: %v", conn.RemoteAddr(), conn.LocalAddr(), err)
				conn.Close()
				return
			}
			ln.vifs.Insert(vf)
			ln.manager.vifs.Insert(vf)

			ln.vifLoops.Add(1)
			vifLoop(vf, ln.q, func() {
				ln.connsMu.Lock()
				delete(ln.conns, conn)
				ln.connsMu.Unlock()
				ln.vifLoops.Done()
			})
		}()
	}
}

func (ln *netListener) Accept() (stream.Flow, error) {
	item, err := ln.q.Get(nil)
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errListenerAlreadyClosed, nil))
	case err != nil:
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errAcceptFailed, nil, err))
	default:
		return item.(vif.ConnectorAndFlow).Flow, nil
	}
}

func (ln *netListener) Close() error {
	closeNetListener(ln.netLn)
	ln.netLoop.Wait()
	for _, vif := range ln.vifs.List() {
		// NOTE(caprita): We do not actually Close down the vifs, as
		// that would require knowing when all outstanding requests are
		// finished.  For now, do not worry about it, since we expect
		// shut down to immediately precede process exit.
		vif.StopAccepting()
	}
	ln.q.Shutdown()
	ln.manager.removeListener(ln)
	ln.vifLoops.Wait()
	vlog.VI(3).Infof("Closed stream.Listener %s", ln)
	return nil
}

func (ln *netListener) deleteVIF(vf *vif.VIF) {
	vlog.VI(2).Infof("VIF %v is closed, removing from cache", vf)
	ln.vifs.Delete(vf)
	ln.manager.vifs.Delete(vf)
}

func (ln *netListener) String() string {
	return fmt.Sprintf("%T: (%v, %v)", ln, ln.netLn.Addr().Network(), ln.netLn.Addr())
}

func (ln *netListener) DebugString() string {
	ret := []string{
		fmt.Sprintf("stream.Listener: net.Listener on (%q, %q)", ln.netLn.Addr().Network(), ln.netLn.Addr()),
	}
	if vifs := ln.vifs.List(); len(vifs) > 0 {
		ret = append(ret, fmt.Sprintf("===Accepted VIFs(%d)===", len(vifs)))
		for ix, vif := range vifs {
			ret = append(ret, fmt.Sprintf("%4d) %v", ix, vif))
		}
	}
	return strings.Join(ret, "\n")
}

func newProxyListener(m *manager, proxyEP naming.Endpoint, principal security.Principal, opts []stream.ListenerOpt) (listener, *inaming.Endpoint, error) {
	ln := &proxyListener{
		q:       upcqueue.New(),
		proxyEP: proxyEP,
		manager: m,
	}
	vf, ep, err := ln.connect(principal, opts)
	if err != nil {
		return nil, nil, err
	}
	ln.vif = vf
	ln.vifLoop.Add(1)
	go vifLoop(ln.vif, ln.q, func() {
		ln.vifLoop.Done()
	})
	return ln, ep, nil
}

func (ln *proxyListener) connect(principal security.Principal, opts []stream.ListenerOpt) (*vif.VIF, *inaming.Endpoint, error) {
	vlog.VI(1).Infof("Connecting to proxy at %v", ln.proxyEP)
	// Requires dialing a VC to the proxy, need to extract options from ln.opts to do so.
	var dialOpts []stream.VCOpt
	for _, opt := range opts {
		if dopt, ok := opt.(stream.VCOpt); ok {
			dialOpts = append(dialOpts, dopt)
		}
	}
	// TODO(cnicolaou, ashankar): probably want to set a timeout here. (is this covered by opts?)
	vf, err := ln.manager.FindOrDialVIF(ln.proxyEP, principal, dialOpts...)
	if err != nil {
		return nil, nil, err
	}
	// Prepend the default idle timeout for VC.
	opts = append([]stream.ListenerOpt{vc.IdleTimeout{defaultIdleTimeout}}, opts...)
	if err := vf.StartAccepting(opts...); err != nil {
		return nil, nil, verror.New(stream.ErrNetwork, nil, verror.New(errAlreadyConnected, nil, vf, err))
	}
	// Proxy protocol: See v.io/x/ref/profiles/internal/rpc/stream/proxy/protocol.vdl
	//
	// We don't need idle timeout for this VC, since one flow will be kept alive.
	vc, err := vf.Dial(ln.proxyEP, principal, dialOpts...)
	if err != nil {
		vf.StopAccepting()
		if verror.ErrorID(err) == verror.ErrAborted.ID {
			ln.manager.vifs.Delete(vf)
			return nil, nil, verror.New(stream.ErrAborted, nil, err)
		}
		return nil, nil, err
	}
	flow, err := vc.Connect()
	if err != nil {
		vf.StopAccepting()
		return nil, nil, verror.New(stream.ErrNetwork, nil, verror.New(errFailedToCreateLivenessFlow, nil, err))
	}
	var request proxy.Request
	var response proxy.Response
	enc, err := vom.NewEncoder(flow)
	if err != nil {
		flow.Close()
		vf.StopAccepting()
		return nil, nil, verror.New(stream.ErrNetwork, nil, verror.New(errVomDecoder, nil, err))
	}
	if err := enc.Encode(request); err != nil {
		flow.Close()
		vf.StopAccepting()
		return nil, nil, verror.New(stream.ErrNetwork, nil, verror.New(errVomEncodeRequest, nil, err))
	}
	dec, err := vom.NewDecoder(flow)
	if err != nil {
		flow.Close()
		vf.StopAccepting()
		return nil, nil, verror.New(stream.ErrNetwork, nil, verror.New(errVomDecoder, nil, err))
	}
	if err := dec.Decode(&response); err != nil {
		flow.Close()
		vf.StopAccepting()
		return nil, nil, verror.New(stream.ErrNetwork, nil, verror.New(errVomDecodeResponse, nil, err))
	}
	if response.Error != nil {
		flow.Close()
		vf.StopAccepting()
		return nil, nil, verror.New(stream.ErrProxy, nil, response.Error)
	}
	ep, err := inaming.NewEndpoint(response.Endpoint)
	if err != nil {
		flow.Close()
		vf.StopAccepting()
		return nil, nil, verror.New(stream.ErrProxy, nil, verror.New(errProxyEndpointError, nil, response.Endpoint, err))
	}
	go func(vf *vif.VIF, flow stream.Flow, q *upcqueue.T) {
		<-flow.Closed()
		vf.StopAccepting()
		q.Close()
	}(vf, flow, ln.q)
	return vf, ep, nil
}

func (ln *proxyListener) Accept() (stream.Flow, error) {
	item, err := ln.q.Get(nil)
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errListenerAlreadyClosed, nil))
	case err != nil:
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errAcceptFailed, nil, err))
	default:
		return item.(vif.ConnectorAndFlow).Flow, nil
	}
}

func (ln *proxyListener) Close() error {
	ln.vif.StopAccepting()
	ln.q.Shutdown()
	ln.manager.removeListener(ln)
	ln.vifLoop.Wait()
	vlog.VI(3).Infof("Closed stream.Listener %s", ln)
	return nil
}

func (ln *proxyListener) String() string {
	return ln.DebugString()
}

func (ln *proxyListener) DebugString() string {
	return fmt.Sprintf("stream.Listener: PROXY:%v RoutingID:%v", ln.proxyEP, ln.manager.rid)
}

func vifLoop(vf *vif.VIF, q *upcqueue.T, cleanup func()) {
	defer cleanup()
	for {
		cAndf, err := vf.Accept()
		switch {
		case err != nil:
			vlog.VI(2).Infof("Shutting down listener on VIF %v: %v", vf, err)
			return
		case cAndf.Flow == nil:
			vlog.VI(1).Infof("New VC %v on VIF %v", cAndf.Connector, vf)
		default:
			if err := q.Put(cAndf); err != nil {
				vlog.VI(1).Infof("Closing new flow on VC %v (VIF %v) as Put failed in vifLoop: %v", cAndf.Connector, vf, err)
				cAndf.Flow.Close()
			}
		}
	}
}
