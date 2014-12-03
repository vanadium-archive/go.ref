package proxy

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"

	"veyron.io/veyron/veyron/lib/websocket"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/crypto"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/id"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/message"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vif"
	"veyron.io/veyron/veyron/runtimes/google/ipc/version"
	"veyron.io/veyron/veyron/runtimes/google/lib/bqueue"
	"veyron.io/veyron/veyron/runtimes/google/lib/bqueue/drrqueue"
	"veyron.io/veyron/veyron/runtimes/google/lib/iobuf"
	"veyron.io/veyron/veyron/runtimes/google/lib/upcqueue"
)

var (
	errNoRoutingTableEntry = errors.New("routing table has no entry for the VC")
	errProcessVanished     = errors.New("remote process vanished")
	errDuplicateOpenVC     = errors.New("duplicate OpenVC request")
)

// Proxy routes virtual circuit (VC) traffic between multiple underlying
// network connections.
type Proxy struct {
	ln         net.Listener
	rid        naming.RoutingID
	principal  security.Principal
	mu         sync.RWMutex
	servers    *servermap
	processes  map[*process]struct{}
	pubAddress string
}

// process encapsulates the physical network connection and the routing table
// associated with the process at the other end of the network connection.
type process struct {
	Conn         net.Conn
	isSetup      bool
	ctrlCipher   crypto.ControlCipher
	Queue        *upcqueue.T
	mu           sync.RWMutex
	routingTable map[id.VC]*destination
	nextVCI      id.VC
	servers      map[id.VC]*vc.VC // servers wishing to be proxied create a VC that terminates at the proxy
	BQ           bqueue.T         // Flow control for messages sent on behalf of servers.
}

// destination is an entry in the routingtable of a process.
type destination struct {
	VCI     id.VC
	Process *process
}

// server encapsulates information stored about a server exporting itself via the proxy.
type server struct {
	Process *process
	VC      *vc.VC
}

func (s *server) RoutingID() naming.RoutingID { return s.VC.RemoteAddr().RoutingID() }
func (s *server) Close(err error) {
	if vc := s.Process.RemoveServerVC(s.VC.VCI()); vc != nil {
		if err != nil {
			vc.Close(err.Error())
		} else {
			vc.Close("server closed by proxy")
		}
		s.Process.SendCloseVC(s.VC.VCI(), err)
	}
}
func (s *server) String() string {
	return fmt.Sprintf("RoutingID %v on process %v (VCI:%v Blessings:%v)", s.RoutingID(), s.Process, s.VC.VCI(), s.VC.RemoteBlessings())
}

// servermap is a concurrent-access safe map from the RoutingID of a server exporting itself
// through the proxy to the underlying network connection that the server is found on.
type servermap struct {
	mu sync.Mutex
	m  map[naming.RoutingID]*server
}

func (m *servermap) Add(server *server) error {
	key := server.RoutingID()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m[key] != nil {
		return fmt.Errorf("server with routing id %v is already being proxied", key)
	}
	m.m[key] = server
	proxyLog().Infof("Started proxying server: %v", server)
	return nil
}
func (m *servermap) Remove(server *server) {
	key := server.RoutingID()
	m.mu.Lock()
	if m.m[key] != nil {
		delete(m.m, key)
		proxyLog().Infof("Stopped proxying server: %v", server)
	}
	m.mu.Unlock()
}
func (m *servermap) Process(rid naming.RoutingID) *process {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.m[rid]; s != nil {
		return s.Process
	}
	return nil
}
func (m *servermap) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ret := make([]string, 0, len(m.m))
	for _, s := range m.m {
		ret = append(ret, s.String())
	}
	return ret
}

// New creates a new Proxy that listens for network connections on the provided
// (network, address) pair and routes VC traffic between accepted connections.
func New(rid naming.RoutingID, principal security.Principal, network, address, pubAddress string) (*Proxy, error) {
	ln, err := net.Listen(network, address)
	if err != nil {
		return nil, fmt.Errorf("net.Listen(%q, %q) failed: %v", network, address, err)
	}
	ln, err = websocket.NewListener(ln)
	if err != nil {
		return nil, err
	}
	if len(pubAddress) == 0 {
		pubAddress = ln.Addr().String()
	}
	proxy := &Proxy{
		ln:         ln,
		rid:        rid,
		servers:    &servermap{m: make(map[naming.RoutingID]*server)},
		processes:  make(map[*process]struct{}),
		pubAddress: pubAddress,
		principal:  principal,
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
		go p.acceptProcess(conn)
	}
}

func (p *Proxy) acceptProcess(conn net.Conn) {
	process := &process{
		Conn:         conn,
		ctrlCipher:   &crypto.NullControlCipher{},
		Queue:        upcqueue.New(),
		routingTable: make(map[id.VC]*destination),
		servers:      make(map[id.VC]*vc.VC),
		BQ:           drrqueue.New(vc.MaxPayloadSizeBytes),
	}
	go writeLoop(process)
	go serverVCsLoop(process)
	go p.readLoop(process)
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
		if err = message.WriteTo(process.Conn, item.(message.T), process.ctrlCipher); err != nil {
			processLog().Infof("message.WriteTo on %v failed: %v", process, err)
			return
		}
	}
}
func serverVCsLoop(process *process) {
	for {
		w, bufs, err := process.BQ.Get(nil)
		if err != nil {
			return
		}
		vci, fid := unpackIDs(w.ID())
		if vc := process.ServerVC(vci); vc != nil {
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
func (p *Proxy) startProcess(process *process) {
	p.mu.Lock()
	p.processes[process] = struct{}{}
	p.mu.Unlock()
	processLog().Infof("Started process %v", process)
}
func (p *Proxy) stopProcess(process *process) {
	process.Close()
	p.mu.Lock()
	delete(p.processes, process)
	p.mu.Unlock()
	processLog().Infof("Stopped process %v", process)
}
func (p *Proxy) readLoop(process *process) {
	p.startProcess(process)
	defer p.stopProcess(process)
	reader := iobuf.NewReader(iobuf.NewPool(0), process.Conn)
	defer reader.Close()
	for {
		msg, err := message.ReadFrom(reader, process.ctrlCipher)
		if err != nil {
			processLog().Infof("Read on %v failed: %v", process, err)
			return
		}
		msgLog().Infof("Received msg: %T = %v", msg, msg)
		switch m := msg.(type) {
		case *message.Data:
			if vc := process.ServerVC(m.VCI); vc != nil {
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
			if vc := process.ServerVC(m.VCI); vc != nil {
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
			if vc := process.RemoveServerVC(m.VCI); vc != nil {
				vc.Close(m.Error)
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
			dstrid := m.DstEndpoint.RoutingID()
			if naming.Compare(dstrid, p.rid) || naming.Compare(dstrid, naming.NullRoutingID) {
				// VC that terminates at the proxy.
				// See protocol.vdl for details on the protocol between the server and the proxy.
				vcObj := process.NewServerVC(m)
				// route counters after creating the VC so counters to vc are not lost.
				p.routeCounters(process, m.Counters)
				if vcObj != nil {
					server := &server{Process: process, VC: vcObj}
					go p.runServer(server, vcObj.HandshakeAcceptedVC(vc.LocalPrincipal{p.principal}))
				}
				break
			}
			dstprocess := p.servers.Process(dstrid)
			if dstprocess == nil {
				process.SendCloseVC(m.VCI, fmt.Errorf("no server with routing id %v is being proxied", dstrid))
				p.routeCounters(process, m.Counters)
				break
			}
			srcVCI := m.VCI
			dstVCI := dstprocess.AllocVCI()
			startRoutingVC(srcVCI, dstVCI, process, dstprocess)
			// Forward the OpenVC message.
			// Typically, an OpenVC message is accompanied with Counters for the new VC.
			// Keep that in the forwarded message and route the remaining counters separately.
			counters := m.Counters
			m.Counters = message.NewCounters()
			for cid, bytes := range counters {
				if cid.VCI() == srcVCI {
					m.Counters.Add(dstVCI, cid.Flow(), bytes)
					delete(counters, cid)
				}
			}
			m.VCI = dstVCI
			dstprocess.Queue.Put(m)
			p.routeCounters(process, counters)
		case *message.HopSetup:
			// Set up the hop.  This takes over the process during negotiation.
			if process.isSetup {
				// Already performed authentication.  We don't do it again.
				processLog().Infof("Process %v is already setup", process)
				return
			}
			var blessings security.Blessings
			if p.principal != nil {
				blessings = p.principal.BlessingStore().Default()
			}
			c, err := vif.AuthenticateAsServer(process.Conn, nil, p.principal, blessings, nil, m)
			if err != nil {
				processLog().Infof("Process %v failed to authenticate: %s", process, err)
				return
			}
			process.ctrlCipher = c
			process.isSetup = true
		default:
			processLog().Infof("Closing %v because of unrecognized message %T", process, m)
			return
		}
	}
}
func (p *Proxy) runServer(server *server, c <-chan vc.HandshakeResult) {
	hr := <-c
	if hr.Error != nil {
		server.Close(hr.Error)
		return
	}
	// See comments in protocol.vdl for the protocol between servers and the proxy.
	conn, err := hr.Listener.Accept()
	if err != nil {
		server.Close(errors.New("failed to accept health check flow"))
		return
	}
	defer server.Close(nil)
	server.Process.InitVCI(server.VC.VCI())
	var request Request
	var response Response
	if err := vom.NewDecoder(conn).Decode(&request); err != nil {
		response.Error = verror.BadProtocolf("proxy: unable to read Request: %v", err)
	} else if err := p.servers.Add(server); err != nil {
		response.Error = verror.Convert(err)
	} else {
		defer p.servers.Remove(server)
		ep, err := version.ProxiedEndpoint(server.VC.RemoteAddr().RoutingID(), p.Endpoint())
		if err != nil {
			response.Error = verror.ConvertWithDefault(verror.Internal, err)
		}
		if ep != nil {
			response.Endpoint = ep.String()
		}
	}
	if err := vom.NewEncoder(conn).Encode(response); err != nil {
		proxyLog().Infof("Failed to encode response %#v for server %v", response, server)
		server.Close(err)
		return
	}
	// Reject all other flows
	go func() {
		for {
			flow, err := hr.Listener.Accept()
			if err != nil {
				return
			}
			flow.Close()
		}
	}()
	// Wait for this flow to be closed.
	<-conn.Closed()
}
func (p *Proxy) routeCounters(process *process, counters message.Counters) {
	// Since each VC can be routed to a different process, split up the
	// Counters into one message per VC.
	// Ideally, would split into one message per process (rather than per
	// flow). This optimization is left an as excercise to the interested.
	for cid, bytes := range counters {
		srcVCI := cid.VCI()
		if vc := process.ServerVC(srcVCI); vc != nil {
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
	return version.Endpoint(p.ln.Addr().Network(), p.pubAddress, p.rid)
}

// Shutdown stops the proxy service, closing all network connections.
func (p *Proxy) Shutdown() {
	p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for process, _ := range p.processes {
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
func (p *process) InitVCI(vci id.VC) {
	p.mu.Lock()
	if p.nextVCI <= vci {
		p.nextVCI = vci + 1
	}
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
	for _, vc := range p.servers {
		vc.Close("net.Conn is closing")
	}
	p.mu.Unlock()
	for _, d := range rt {
		d.Process.SendCloseVC(d.VCI, errProcessVanished)
	}
	p.BQ.Close()
	p.Queue.Close()
	p.Conn.Close()
}
func (p *process) ServerVC(vci id.VC) *vc.VC {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.servers[vci]
}
func (p *process) NewServerVC(m *message.OpenVC) *vc.VC {
	p.mu.Lock()
	defer p.mu.Unlock()
	if vc := p.servers[m.VCI]; vc != nil {
		vc.Close("duplicate OpenVC request")
		return nil
	}
	version, err := version.CommonVersion(m.DstEndpoint, m.SrcEndpoint)
	if err != nil {
		p.SendCloseVC(m.VCI, fmt.Errorf("incompatible IPC protocol versions: %v", err))
		return nil
	}
	vc := vc.InternalNew(vc.Params{
		VCI:          m.VCI,
		LocalEP:      m.DstEndpoint,
		RemoteEP:     m.SrcEndpoint,
		Pool:         iobuf.NewPool(0),
		ReserveBytes: message.HeaderSizeBytes,
		Helper:       p,
		Version:      version,
	})
	p.servers[m.VCI] = vc
	proxyLog().Infof("Registered VC %v from server on process %v", vc, p)
	return vc
}
func (p *process) RemoveServerVC(vci id.VC) *vc.VC {
	p.mu.Lock()
	defer p.mu.Unlock()
	if vc := p.servers[vci]; vc != nil {
		delete(p.servers, vci)
		proxyLog().Infof("Unregistered server VC %v from process %v", vc, p)
		return vc
	}
	return nil
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
