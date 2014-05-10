package proxy

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"veyron/runtimes/google/ipc/stream/id"
	"veyron/runtimes/google/ipc/stream/message"
	"veyron/runtimes/google/ipc/stream/vc"
	"veyron/runtimes/google/ipc/version"
	"veyron/runtimes/google/lib/bqueue"
	"veyron/runtimes/google/lib/bqueue/drrqueue"
	"veyron/runtimes/google/lib/iobuf"
	"veyron/runtimes/google/lib/upcqueue"

	"veyron2"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/vlog"
)

var (
	errNoRoutingTableEntry = errors.New("routing table has no entry for the VC")
	errProcessVanished     = errors.New("remote process vanished")
	errDuplicateOpenVC     = errors.New("duplicate OpenVC request")
)

// Proxy routes virtual circuit (VC) traffic between multiple underlying
// network connections.
type Proxy struct {
	ln  net.Listener
	rid naming.RoutingID
	id  security.PrivateID

	mu        sync.RWMutex
	processes map[naming.RoutingID]*process
}

// process encapsulates the physical network connection and the routing table
// associated with a single peer process (identified by a naming.RoutingID).
type process struct {
	Conn  net.Conn
	Queue *upcqueue.T

	mu           sync.RWMutex
	routingTable map[id.VC]*destination
	nextVCI      id.VC

	// Some VCs terminate at the proxy process (for example, VCs initiated
	// by servers wishing to be proxied).
	proxyVCs map[id.VC]*vc.VC // VCs that terminate at this proxy
	BQ       bqueue.T         // Flow control for messages sent on behalf of proxyVCs.
}

type destination struct {
	VCI     id.VC
	Process *process
}

// New creates a new Proxy that listens for network connections on the provided
// (network, address) pair and routes VC traffic between accepted connections.
func New(rid naming.RoutingID, identity security.PrivateID, network, address string) (*Proxy, error) {
	ln, err := net.Listen(network, address)
	if err != nil {
		return nil, fmt.Errorf("net.Listen(%q, %q) failed: %v", network, address, err)
	}
	proxy := &Proxy{
		ln:        ln,
		rid:       rid,
		id:        identity,
		processes: make(map[naming.RoutingID]*process),
	}
	go proxy.listenLoop()
	return proxy, nil
}

func (p *Proxy) listenLoop() {
	proxyLog().Infof("Proxy listening on (%q, %q): %v", p.ln.Addr().Network(), p.ln.Addr(), p.Endpoint())
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			proxyLog().Infof("Exiting listenLoop of proxy %q: %v", p.Endpoint(), err)
			return
		}
		process := &process{
			Conn:         conn,
			Queue:        upcqueue.New(),
			routingTable: make(map[id.VC]*destination),
			proxyVCs:     make(map[id.VC]*vc.VC),
			BQ:           drrqueue.New(vc.MaxPayloadSizeBytes),
		}
		go writeLoop(process)
		go p.readLoop(process)
		go proxyVCLoop(process)
		processLog().Infof("New network connection: %v", process)
	}
}

func writeLoop(process *process) {
	defer processLog().Infof("Exited writeLoop for %v", process)
	defer process.Close()
	for {
		item, err := process.Queue.Get(nil)
		if err != nil {
			if err != upcqueue.ErrQueueIsClosed {
				processLog().Infof("upcqueue.Get failed on %v: %v", process, err)
			}
			return
		}
		if err = message.WriteTo(process.Conn, item.(message.T)); err != nil {
			processLog().Infof("message.WriteTo on %v failed: %v", process, err)
			return
		}
	}
}

func proxyVCLoop(process *process) {
	for {
		w, bufs, err := process.BQ.Get(nil)
		if err != nil {
			return
		}
		vci, fid := unpackIDs(w.ID())
		if vc := process.ProxyVC(vci); vc != nil {
			queueDataMessages(bufs, vc, fid, process.Queue)
			if len(bufs) == 0 {
				m := &message.Data{VCI: vci, Flow: fid}
				m.SetClose()
				process.Queue.Put(m)
				w.Shutdown(true)
			}
			continue
		}
		releaseBufs(0, bufs)
	}
}

func releaseBufs(start int, bufs []*iobuf.Slice) {
	for i := start; i < len(bufs); i++ {
		bufs[i].Release()
	}
}

func queueDataMessages(bufs []*iobuf.Slice, vc *vc.VC, fid id.Flow, q *upcqueue.T) {
	for ix, b := range bufs {
		m := &message.Data{VCI: vc.VCI(), Flow: fid}
		var err error
		if m.Payload, err = vc.Encrypt(fid, b); err != nil {
			msgLog().Infof("vc.Encrypt failed. VC:%v Flow:%v Error:%v", vc, fid, err)
			releaseBufs(ix+1, bufs)
			return
		}
		if err = q.Put(m); err != nil {
			msgLog().Infof("Failed to enqueue data message %v: %v", m, err)
			m.Release()
			releaseBufs(ix+1, bufs)
			return
		}
	}
}

func (p *Proxy) readLoop(process *process) {
	defer processLog().Infof("Exited readLoop for %v", process)
	defer process.Close()

	reader := iobuf.NewReader(iobuf.NewPool(0), process.Conn)
	defer reader.Close()

	for {
		msg, err := message.ReadFrom(reader)
		if err != nil {
			processLog().Infof("Read on %v failed: %v", process, err)
			return
		}
		msgLog().Infof("Received msg: %T = %v", msg, msg)
		switch m := msg.(type) {
		case *message.Data:
			if vc := process.ProxyVC(m.VCI); vc != nil {
				if err := vc.DispatchPayload(m.Flow, m.Payload); err != nil {
					processLog().Infof("Ignoring data message %v from process %v: %v", m, process, err)
				}
				if m.Close() {
					vc.ShutdownFlow(m.Flow)
				}
				break
			}
			srcVCI := m.VCI
			if d := process.Route(srcVCI); d != nil {
				m.VCI = d.VCI
				if err := d.Process.Queue.Put(m); err != nil {
					process.RemoveRoute(srcVCI)
					process.SendCloseVC(srcVCI, fmt.Errorf("proxy failed to forward data message: %v", err))
				}
				break
			}
			process.SendCloseVC(srcVCI, errNoRoutingTableEntry)
		case *message.OpenFlow:
			if vc := process.ProxyVC(m.VCI); vc != nil {
				if err := vc.AcceptFlow(m.Flow); err != nil {
					processLog().Infof("OpenFlow %+v on process %v failed: %v", m, process, err)
					cm := &message.Data{VCI: m.VCI, Flow: m.Flow}
					cm.SetClose()
					process.Queue.Put(cm)
				}
				vc.ReleaseCounters(m.Flow, m.InitialCounters)
				break
			}
			srcVCI := m.VCI
			if d := process.Route(srcVCI); d != nil {
				m.VCI = d.VCI
				if err := d.Process.Queue.Put(m); err != nil {
					process.RemoveRoute(srcVCI)
					process.SendCloseVC(srcVCI, fmt.Errorf("proxy failed to forward open flow message: %v", err))
				}
				break
			}
			process.SendCloseVC(srcVCI, errNoRoutingTableEntry)
		case *message.CloseVC:
			if process.RemoveProxyVC(m.VCI, m.Error) {
				break
			}
			srcVCI := m.VCI
			if d := process.Route(srcVCI); d != nil {
				m.VCI = d.VCI
				d.Process.Queue.Put(m)
				d.Process.RemoveRoute(d.VCI)
			}
			process.RemoveRoute(srcVCI)
		case *message.AddReceiveBuffers:
			p.routeCounters(process, m.Counters)
		case *message.OpenVC:
			// Update processes map and routing tables if necessary.
			srcrid := m.SrcEndpoint.RoutingID()
			dstrid := m.DstEndpoint.RoutingID()
			seenBefore, err := p.addProcess(srcrid, process)
			if !seenBefore {
				processLog().Infof("RoutingID %v associated with process %v", srcrid, process)
				process.mu.Lock()
				process.nextVCI = m.VCI + 1
				process.mu.Unlock()
				defer p.removeProcess(srcrid)
			}
			switch {
			case err != nil:
				process.SendCloseVC(m.VCI, err)
			case naming.Compare(dstrid, p.rid) || naming.Compare(dstrid, naming.NullRoutingID):
				if !process.AddProxyVC(m, p.id) {
					process.SendCloseVC(m.VCI, errDuplicateOpenVC)
				}
				p.routeCounters(process, m.Counters)
			default:
				// Find the process corresponding to dstrid
				p.mu.RLock()
				dstProcess := p.processes[dstrid]
				p.mu.RUnlock()
				if dstProcess == nil {
					process.SendCloseVC(m.VCI, fmt.Errorf("routing id %v is not connected to the proxy", dstrid))
					break
				}
				// Update the routing tables
				srcVCI := m.VCI
				dstVCI := dstProcess.AllocVCI()
				startRoutingVC(srcVCI, dstVCI, process, dstProcess)
				// Forward the OpenVC message.
				// Typically, an OpenVC message is accompanied with Counters for
				// the new VC. Keep that in the forwarded message and route the
				// remaining counters separately.
				counters := m.Counters

				m.Counters = message.NewCounters()
				for cid, bytes := range counters {
					if cid.VCI() == m.VCI {
						m.Counters.Add(dstVCI, cid.Flow(), bytes)
						delete(counters, cid)
					}
				}
				m.VCI = dstVCI
				dstProcess.Queue.Put(m)
				p.routeCounters(process, counters)
			}
		default:
			processLog().Infof("Closing %v because of unrecognized message %T", process, m)
			return
		}
	}
}

func (p *Proxy) addProcess(rid naming.RoutingID, process *process) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch p.processes[rid] {
	case nil:
		// First time this rid was seen.
		p.processes[rid] = process
		return false, nil
	case process:
		// Same (rid, process) pair as before. Nothing to do.
		return true, nil
	default:
		// Conflicting (rid, process) pairs.
		// TODO(ashankar): This means that the first process to
		// advertise a particular srcrid gets into the routing
		// table. What's our security strategy vis-a-vis faking
		// routing ids?
		return true, fmt.Errorf("routing id %v is registered by another process at the proxy", rid)
	}
}

func (p *Proxy) removeProcess(rid naming.RoutingID) {
	p.mu.Lock()
	delete(p.processes, rid)
	p.mu.Unlock()
	processLog().Infof("Removed routing for %v", rid)
}

func (p *Proxy) routeCounters(process *process, counters message.Counters) {
	// Since each VC can be routed to a different process, split up the
	// Counters into one message per VC.
	// Ideally, would split into one message per process (rather than per
	// VC). This optimization is left an as excercise to the interested.
	for cid, bytes := range counters {
		srcVCI := cid.VCI()
		if vc := process.proxyVCs[srcVCI]; vc != nil {
			vc.ReleaseCounters(cid.Flow(), bytes)
			continue
		}
		if d := process.Route(srcVCI); d != nil {
			c := message.NewCounters()
			c.Add(d.VCI, cid.Flow(), bytes)
			if err := d.Process.Queue.Put(&message.AddReceiveBuffers{Counters: c}); err != nil {
				process.RemoveRoute(srcVCI)
				process.SendCloseVC(srcVCI, fmt.Errorf("proxy failed to forward receive buffers: %v", err))
			}
		}
	}
}

func startRoutingVC(srcVCI, dstVCI id.VC, srcProcess, dstProcess *process) {
	dstProcess.AddRoute(dstVCI, &destination{VCI: srcVCI, Process: srcProcess})
	srcProcess.AddRoute(srcVCI, &destination{VCI: dstVCI, Process: dstProcess})
	vcLog().Infof("Routing (VCI %d @ [%s]) <-> (VCI %d @ [%s])", srcVCI, srcProcess, dstVCI, dstProcess)
}

// Endpoint returns the endpoint of the proxy service.  By Dialing a VC to this
// endpoint, processes can have their services exported through the proxy.
func (p *Proxy) Endpoint() naming.Endpoint {
	return version.Endpoint(p.ln.Addr().Network(), p.ln.Addr().String(), p.rid)
}

// Shutdown stops the proxy service, closing all network connections.
func (p *Proxy) Shutdown() {
	p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, process := range p.processes {
		process.Close()
	}
}

func (p *process) String() string {
	r := p.Conn.RemoteAddr()
	return fmt.Sprintf("(%s, %s)", r.Network(), r)
}

func (p *process) Route(vci id.VC) *destination {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.routingTable[vci]
}

func (p *process) AddRoute(vci id.VC, d *destination) {
	p.mu.Lock()
	p.routingTable[vci] = d
	p.mu.Unlock()
}

func (p *process) AllocVCI() id.VC {
	p.mu.Lock()
	ret := p.nextVCI
	p.nextVCI += 2
	p.mu.Unlock()
	return ret
}

func (p *process) RemoveRoute(vci id.VC) {
	p.mu.Lock()
	delete(p.routingTable, vci)
	p.mu.Unlock()
}

func (p *process) SendCloseVC(vci id.VC, err error) {
	var estr string
	if err != nil {
		estr = err.Error()
	}
	p.Queue.Put(&message.CloseVC{VCI: vci, Error: estr})
}

func (p *process) Close() {
	p.mu.Lock()
	rt := p.routingTable
	p.routingTable = nil
	p.mu.Unlock()

	for _, d := range rt {
		d.Process.SendCloseVC(d.VCI, errProcessVanished)
	}
	p.BQ.Close()
	p.Queue.Close()
	p.Conn.Close()
}

func (p *process) ProxyVC(vci id.VC) *vc.VC {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.proxyVCs[vci]
}

func (p *process) AddProxyVC(m *message.OpenVC, id security.PrivateID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if vc := p.proxyVCs[m.VCI]; vc != nil {
		vc.Close("duplicate OpenVC request")
		return false
	}
	vc := vc.InternalNew(vc.Params{
		VCI:          m.VCI,
		LocalEP:      m.DstEndpoint,
		RemoteEP:     m.SrcEndpoint,
		Pool:         iobuf.NewPool(0),
		ReserveBytes: message.HeaderSizeBytes,
		Helper:       p,
	})
	p.proxyVCs[m.VCI] = vc
	proxyLog().Infof("Opening VC %v from process %v", vc, p)
	go p.handleProxyVC(vc, vc.HandshakeAcceptedVC(veyron2.LocalID(id)))
	return true
}

func (p *process) handleProxyVC(vc *vc.VC, c <-chan vc.HandshakeResult) {
	hr := <-c
	if hr.Error != nil {
		p.RemoveProxyVC(vc.VCI(), fmt.Sprintf("handshake failed: %v", hr.Error))
		p.SendCloseVC(vc.VCI(), hr.Error)
		return
	}
	// The proxy protocol is:
	// (1) Server establishes a VC to the proxy to setup routing table and
	// authenticate.
	// (2) The server opens a flow and waits for it to be closed.  No data
	// is every read/written on the flow, the flow closure is an indication
	// of the connection to the proxy dying.
	//
	// The proxy expects this "health" flow to be the first one and rejects
	// any other flows on the VC.
	//
	// TODO(ashankar): Document this "protocol" once it is stable.
	_, err := hr.Listener.Accept()
	if err != nil {
		vc.Close("failed to accept health check flow")
		return
	}
	// Reject all other flows
	for {
		flow, err := hr.Listener.Accept()
		if err != nil {
			return
		}
		flow.Close()
	}
}

func (p *process) RemoveProxyVC(vci id.VC, reason string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if vc := p.proxyVCs[vci]; vc != nil {
		proxyLog().Infof("Closing VC %v from process %v", vc, p)
		vc.Close(reason)
		delete(p.proxyVCs, vci)
		return true
	}
	return false
}

// Make process implement vc.Helper
func (p *process) NotifyOfNewFlow(vci id.VC, fid id.Flow, bytes uint) {
	msg := &message.OpenFlow{VCI: vci, Flow: fid, InitialCounters: uint32(bytes)}
	if err := p.Queue.Put(msg); err != nil {
		processLog().Infof("Failed to send OpenFlow(%+v) on process %v: %v", msg, p, err)
	}
}

func (p *process) AddReceiveBuffers(vci id.VC, fid id.Flow, bytes uint) {
	if bytes == 0 {
		return
	}
	msg := &message.AddReceiveBuffers{Counters: message.NewCounters()}
	msg.Counters.Add(vci, fid, uint32(bytes))
	if err := p.Queue.Put(msg); err != nil {
		processLog().Infof("Failed to send AddReceiveBuffers(%+v) on process %v: %v", msg, p, err)
	}
}

func (p *process) NewWriter(vci id.VC, fid id.Flow) (bqueue.Writer, error) {
	return p.BQ.NewWriter(packIDs(vci, fid), 0, vc.DefaultBytesBufferedPerFlow)
}

// Convenience functions to assist with the logging convention.
func proxyLog() vlog.InfoLog   { return vlog.VI(1) }
func processLog() vlog.InfoLog { return vlog.VI(2) }
func vcLog() vlog.InfoLog      { return vlog.VI(3) }
func msgLog() vlog.InfoLog     { return vlog.VI(4) }

func packIDs(vci id.VC, fid id.Flow) bqueue.ID {
	return bqueue.ID(message.MakeCounterID(vci, fid))
}

func unpackIDs(b bqueue.ID) (id.VC, id.Flow) {
	cid := message.CounterID(b)
	return cid.VCI(), cid.Flow()
}
