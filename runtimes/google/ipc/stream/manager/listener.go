package manager

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"veyron/runtimes/google/ipc/stream/vif"
	"veyron/runtimes/google/lib/upcqueue"

	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/verror"
	"veyron2/vlog"
)

var errListenerIsClosed = errors.New("Listener has been Closed")

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

	netLoop  sync.WaitGroup
	vifLoops sync.WaitGroup
}

// proxyListener implements the listener interface by connecting to a remote
// proxy (typically used to "listen" across network domains).
type proxyListener struct {
	q       *upcqueue.T
	proxyEP naming.Endpoint
	manager *manager

	reconnect chan bool     // true when the proxy connection dies, false when the listener is being closed.
	stopped   chan struct{} // closed when reconnectLoop exits
	opts      []stream.ListenerOpt
}

func newNetListener(m *manager, netLn net.Listener, opts []stream.ListenerOpt) listener {
	ln := &netListener{
		q:       upcqueue.New(),
		manager: m,
		netLn:   netLn,
		vifs:    vif.NewSet(),
	}
	ln.netLoop.Add(1)
	go ln.netAcceptLoop(opts)
	return ln
}

func (ln *netListener) netAcceptLoop(listenerOpts []stream.ListenerOpt) {
	defer ln.netLoop.Done()
	for {
		conn, err := ln.netLn.Accept()
		if err != nil {
			vlog.VI(1).Infof("Exiting netAcceptLoop: net.Listener.Accept() failed on %v with %v", ln.netLn, err)
			return
		}
		vlog.VI(1).Infof("New net.Conn accepted from %s (local address: %s)", conn.RemoteAddr(), conn.LocalAddr())
		vf, err := vif.InternalNewAcceptedVIF(conn, ln.manager.rid, nil)
		if err != nil {
			vlog.Infof("Shutting down conn from %s (local address: %s) as a VIF could not be created: %v", conn.RemoteAddr(), conn.LocalAddr(), err)
			conn.Close()
			continue
		}
		vf.StartAccepting(listenerOpts...)
		ln.vifs.Insert(vf)
		ln.manager.vifs.Insert(vf)
		ln.vifLoops.Add(1)
		go ln.vifLoop(vf)
	}
}

func (ln *netListener) vifLoop(vf *vif.VIF) {
	vifLoop(vf, ln.q)
	ln.vifLoops.Done()
}

func (ln *netListener) Accept() (stream.Flow, error) {
	item, err := ln.q.Get(nil)
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, errListenerIsClosed
	case err != nil:
		return nil, fmt.Errorf("Accept failed: %v", err)
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

func (ln *netListener) String() string {
	return fmt.Sprintf("%T: %v", ln, ln.netLn.Addr())
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

func newProxyListener(m *manager, ep naming.Endpoint, opts []stream.ListenerOpt) (listener, error) {
	ln := &proxyListener{
		q:         upcqueue.New(),
		proxyEP:   ep,
		manager:   m,
		reconnect: make(chan bool),
		stopped:   make(chan struct{}),
	}
	vf, err := ln.connect()
	if err != nil {
		return nil, err
	}
	go ln.reconnectLoop(vf)
	return ln, nil
}

func (ln *proxyListener) connect() (*vif.VIF, error) {
	vlog.VI(1).Infof("Connecting to proxy at %v", ln.proxyEP)
	vf, err := ln.manager.FindOrDialVIF(ln.proxyEP.Addr())
	if err != nil {
		return nil, err
	}
	if err := vf.StartAccepting(ln.opts...); err != nil {
		return nil, fmt.Errorf("already connected to proxy and accepting connections? VIF: %v, StartAccepting error: %v", vf, err)
	}
	// Proxy protocol:
	// (1) Dial a VC to it (to include this processes' routing id in the proxy's routing table)
	// (2) Open a Flow and wait for it to die (which should happen only when the proxy is down)
	// For (1), need stream.VCOpt (identity etc.)
	var dialOpts []stream.VCOpt
	for _, opt := range ln.opts {
		if dopt, ok := opt.(stream.VCOpt); ok {
			dialOpts = append(dialOpts, dopt)
		}
	}
	vc, err := vf.Dial(ln.proxyEP, dialOpts...)
	if err != nil {
		vf.StopAccepting()
		if verror.ErrorID(err) == verror.Aborted {
			ln.manager.vifs.Delete(vf)
		}
		return nil, fmt.Errorf("VC establishment with proxy failed: %v", err)
	}
	flow, err := vc.Connect()
	if err != nil {
		vf.StopAccepting()
		return nil, fmt.Errorf("unable to create liveness check flow to proxy: %v", err)
	}
	go func(vf *vif.VIF, flow stream.Flow) {
		<-flow.Closed()
		vf.StopAccepting()
	}(vf, flow)
	return vf, nil
}

func (ln *proxyListener) reconnectLoop(vf *vif.VIF) {
	const (
		min = 5 * time.Millisecond
		max = 5 * time.Minute
	)
	defer close(ln.stopped)
	go ln.vifLoop(vf)
	for {
		if retry := <-ln.reconnect; !retry {
			ln.waitForVIFLoop(vf)
			return
		}
		vlog.VI(3).Infof("Connection to proxy at %v broke. Re-establishing", ln.proxyEP)
		backoff := min
		for {
			var err error
			if vf, err = ln.connect(); err == nil {
				go ln.vifLoop(vf)
				vlog.VI(3).Infof("Proxy reconnect (%v) succeeded", ln.proxyEP)
				break
			}
			vlog.VI(3).Infof("Proxy reconnect (%v) FAILED (%v). Retrying in %v", ln.proxyEP, err, backoff)
			select {
			case retry := <-ln.reconnect:
				// Invariant: ln.vifLoop is not running. Thus,
				// the only writer to ln.reconnect is ln.Close,
				// which always writes false.
				if retry {
					vlog.Errorf("retry=true in %v: rogue vifLoop running?", ln)
				}
				ln.waitForVIFLoop(vf)
				return
			case <-time.After(backoff):
				if backoff = backoff * 2; backoff > max {
					backoff = max
				}
			}
		}
	}
}

func (ln *proxyListener) vifLoop(vf *vif.VIF) {
	vifLoop(vf, ln.q)
	ln.reconnect <- true
}

func (ln *proxyListener) waitForVIFLoop(vf *vif.VIF) {
	if vf == nil {
		return
	}
	// ln.vifLoop is running, wait for it to exit.  (when it exits, it will
	// send "true" on the reconnect channel)
	vf.StopAccepting()
	for retry := range ln.reconnect {
		if retry {
			return
		}
	}
}

func (ln *proxyListener) Accept() (stream.Flow, error) {
	item, err := ln.q.Get(nil)
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, errListenerIsClosed
	case err != nil:
		return nil, fmt.Errorf("Accept failed: %v", err)
	default:
		return item.(vif.ConnectorAndFlow).Flow, nil
	}
}

func (ln *proxyListener) Close() error {
	ln.reconnect <- false // tell reconnectLoop to stop
	<-ln.stopped          // wait for reconnectLoop to exit
	ln.q.Shutdown()
	ln.manager.removeListener(ln)
	return nil
}

func (ln *proxyListener) DebugString() string {
	return fmt.Sprintf("stream.Listener: PROXY:%v RoutingID:%v", ln.proxyEP, ln.manager.rid)
}

func vifLoop(vf *vif.VIF, q *upcqueue.T) {
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
