package proxy

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/publisher"
	"v.io/x/ref/lib/upcqueue"
	"v.io/x/ref/profiles/internal/ipc/stream/crypto"
	"v.io/x/ref/profiles/internal/ipc/stream/id"
	"v.io/x/ref/profiles/internal/ipc/stream/message"
	"v.io/x/ref/profiles/internal/ipc/stream/vc"
	"v.io/x/ref/profiles/internal/ipc/stream/vif"
	"v.io/x/ref/profiles/internal/ipc/version"
	"v.io/x/ref/profiles/internal/lib/bqueue"
	"v.io/x/ref/profiles/internal/lib/bqueue/drrqueue"
	"v.io/x/ref/profiles/internal/lib/iobuf"

	"v.io/x/ref/lib/stats"
)

const pkgPath = "v.io/x/ref/profiles/proxy"

var (
	errNoRoutingTableEntry = errors.New("routing table has no entry for the VC")
	errProcessVanished     = errors.New("remote process vanished")
	errDuplicateOpenVC     = errors.New("duplicate OpenVC request")

	errNoDecoder = verror.Register(pkgPath+".errNoDecoder", verror.NoRetry, "{1:}{2:} proxy: failed to create Decoder{:_}")
	errNoRequest = verror.Register(pkgPath+".errNoRequest", verror.NoRetry, "{1:}{2:} proxy: unable to read Request{:_}")
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
	statsName  string
}

// process encapsulates the physical network connection and the routing table
// associated with the process at the other end of the network connection.
type process struct {
	proxy        *Proxy
	conn         net.Conn
	pool         *iobuf.Pool
	reader       *iobuf.Reader
	isSetup      bool
	ctrlCipher   crypto.ControlCipher
	queue        *upcqueue.T
	mu           sync.RWMutex
	routingTable map[id.VC]*destination
	nextVCI      id.VC
	servers      map[id.VC]*vc.VC // servers wishing to be proxied create a VC that terminates at the proxy
	bq           bqueue.T         // Flow control for messages sent on behalf of servers.
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
// TODO(mattr): This should take a ListenSpec instead of network, address, and
// pubAddress.  However using a ListenSpec requires a great deal of supporting
// code that should be refactored out of v.io/x/ref/profiles/internal/ipc/server.go.
func New(ctx *context.T, network, address, pubAddress string, names ...string) (shutdown func(), endpoint naming.Endpoint, err error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, nil, err
	}

	proxyShutdown, endpoint, err := internalNew(
		rid, v23.GetPrincipal(ctx), network, address, pubAddress)
	if err != nil {
		return nil, nil, err
	}

	var pub publisher.Publisher
	if len(names) > 0 {
		pub = publisher.New(ctx, v23.GetNamespace(ctx), time.Minute)
		pub.AddServer(endpoint.String(), false)
		for _, name := range names {
			pub.AddName(name)
		}
	}

	shutdown = func() {
		if pub != nil {
			pub.Stop()
			pub.WaitForStop()
		}
		proxyShutdown()
	}
	return shutdown, endpoint, nil
}

func internalNew(rid naming.RoutingID, principal security.Principal, network, address, pubAddress string) (shutdown func(), endpoint naming.Endpoint, err error) {
	_, listenFn, _ := ipc.RegisteredProtocol(network)
	if listenFn == nil {
		return nil, nil, fmt.Errorf("unknown network %s", network)
	}
	ln, err := listenFn(network, address)
	if err != nil {
		return nil, nil, fmt.Errorf("net.Listen(%q, %q) failed: %v", network, address, err)
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
		statsName:  naming.Join("ipc", "proxy", "routing-id", rid.String(), "debug"),
	}
	stats.NewStringFunc(proxy.statsName, proxy.debugString)

	go proxy.listenLoop()
	return proxy.shutdown, proxy.endpoint(), nil
}

func (p *Proxy) listenLoop() {
	proxyLog().Infof("Proxy listening on (%q, %q): %v", p.ln.Addr().Network(), p.ln.Addr(), p.endpoint())
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			proxyLog().Infof("Exiting listenLoop of proxy %q: %v", p.endpoint(), err)
			return
		}
		go p.acceptProcess(conn)
	}
}

func (p *Proxy) acceptProcess(conn net.Conn) {
	pool := iobuf.NewPool(0)
	process := &process{
		proxy:        p,
		conn:         conn,
		pool:         pool,
		reader:       iobuf.NewReader(pool, conn),
		ctrlCipher:   &crypto.NullControlCipher{},
		queue:        upcqueue.New(),
		routingTable: make(map[id.VC]*destination),
		servers:      make(map[id.VC]*vc.VC),
		bq:           drrqueue.New(vc.MaxPayloadSizeBytes),
	}

	p.mu.Lock()
	p.processes[process] = struct{}{}
	p.mu.Unlock()

	go process.serverVCsLoop()
	go process.writeLoop()
	go process.readLoop()

	processLog().Infof("Started process %v", process)
}

func (p *Proxy) removeProcess(process *process) {
	p.mu.Lock()
	delete(p.processes, process)
	p.mu.Unlock()
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
	dec, err := vom.NewDecoder(conn)
	if err != nil {
		response.Error = verror.New(errNoDecoder, nil, err)
	} else if err := dec.Decode(&request); err != nil {
		response.Error = verror.New(errNoRequest, nil, err)
	} else if err := p.servers.Add(server); err != nil {
		response.Error = verror.Convert(verror.ErrUnknown, nil, err)
	} else {
		defer p.servers.Remove(server)
		ep, err := version.ProxiedEndpoint(server.VC.RemoteAddr().RoutingID(), p.endpoint())
		if err != nil {
			response.Error = verror.Convert(verror.ErrInternal, nil, err)
		}
		if ep != nil {
			response.Endpoint = ep.String()
		}
	}
	enc, err := vom.NewEncoder(conn)
	if err != nil {
		proxyLog().Infof("Failed to create Encoder for server %v: %v", server, err)
		server.Close(err)
		return
	}
	if err := enc.Encode(response); err != nil {
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
			if err := d.Process.queue.Put(&message.AddReceiveBuffers{Counters: c}); err != nil {
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
func (p *Proxy) endpoint() naming.Endpoint {
	ep := version.Endpoint(p.ln.Addr().Network(), p.pubAddress, p.rid)
	return ep
}

// Shutdown stops the proxy service, closing all network connections.
func (p *Proxy) shutdown() {
	stats.Delete(p.statsName)
	p.ln.Close()
	p.mu.Lock()
	processes := p.processes
	p.processes = nil
	p.mu.Unlock()
	for process, _ := range processes {
		process.Close()
	}
}

func (p *process) serverVCsLoop() {
	for {
		w, bufs, err := p.bq.Get(nil)
		if err != nil {
			return
		}
		vci, fid := unpackIDs(w.ID())
		if vc := p.ServerVC(vci); vc != nil {
			queueDataMessages(bufs, vc, fid, p.queue)
			if len(bufs) == 0 {
				m := &message.Data{VCI: vci, Flow: fid}
				m.SetClose()
				p.queue.Put(m)
				w.Shutdown(true)
			}
			continue
		}
		releaseBufs(0, bufs)
	}
}

func releaseBufs(start int, bufs []*iobuf.Slice) {
	for _, buf := range bufs[start:] {
		buf.Release()
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

func (p *process) writeLoop() {
	defer processLog().Infof("Exited writeLoop for %v", p)
	defer p.Close()

	for {
		item, err := p.queue.Get(nil)
		if err != nil {
			if err != upcqueue.ErrQueueIsClosed {
				processLog().Infof("upcqueue.Get failed on %v: %v", p, err)
			}
			return
		}
		if err = message.WriteTo(p.conn, item.(message.T), p.ctrlCipher); err != nil {
			processLog().Infof("message.WriteTo on %v failed: %v", p, err)
			return
		}
	}
}

func (p *process) readLoop() {
	defer processLog().Infof("Exited readLoop for %v", p)
	defer p.Close()

	for {
		msg, err := message.ReadFrom(p.reader, p.ctrlCipher)
		if err != nil {
			processLog().Infof("Read on %v failed: %v", p, err)
			return
		}
		msgLog().Infof("Received msg: %T = %v", msg, msg)
		switch m := msg.(type) {
		case *message.Data:
			if vc := p.ServerVC(m.VCI); vc != nil {
				if err := vc.DispatchPayload(m.Flow, m.Payload); err != nil {
					processLog().Infof("Ignoring data message %v from process %v: %v", m, p, err)
				}
				if m.Close() {
					vc.ShutdownFlow(m.Flow)
				}
				break
			}
			srcVCI := m.VCI
			if d := p.Route(srcVCI); d != nil {
				m.VCI = d.VCI
				if err := d.Process.queue.Put(m); err != nil {
					m.Release()
					p.RemoveRoute(srcVCI)
					p.SendCloseVC(srcVCI, fmt.Errorf("proxy failed to forward data message: %v", err))
				}
				break
			}
			p.SendCloseVC(srcVCI, errNoRoutingTableEntry)
		case *message.OpenFlow:
			if vc := p.ServerVC(m.VCI); vc != nil {
				if err := vc.AcceptFlow(m.Flow); err != nil {
					processLog().Infof("OpenFlow %+v on process %v failed: %v", m, p, err)
					cm := &message.Data{VCI: m.VCI, Flow: m.Flow}
					cm.SetClose()
					p.queue.Put(cm)
				}
				vc.ReleaseCounters(m.Flow, m.InitialCounters)
				break
			}
			srcVCI := m.VCI
			if d := p.Route(srcVCI); d != nil {
				m.VCI = d.VCI
				if err := d.Process.queue.Put(m); err != nil {
					p.RemoveRoute(srcVCI)
					p.SendCloseVC(srcVCI, fmt.Errorf("proxy failed to forward open flow message: %v", err))
				}
				break
			}
			p.SendCloseVC(srcVCI, errNoRoutingTableEntry)
		case *message.CloseVC:
			if vc := p.RemoveServerVC(m.VCI); vc != nil {
				vc.Close(m.Error)
				break
			}
			srcVCI := m.VCI
			if d := p.Route(srcVCI); d != nil {
				m.VCI = d.VCI
				d.Process.queue.Put(m)
				d.Process.RemoveRoute(d.VCI)
			}
			p.RemoveRoute(srcVCI)
		case *message.AddReceiveBuffers:
			p.proxy.routeCounters(p, m.Counters)
		case *message.SetupVC:
			dstrid := m.RemoteEndpoint.RoutingID()
			if naming.Compare(dstrid, p.proxy.rid) || naming.Compare(dstrid, naming.NullRoutingID) {
				// VC that terminates at the proxy.
				// TODO(ashankar,mattr): Implement this!
				p.SendCloseVC(m.VCI, fmt.Errorf("proxy support for SetupVC not implemented yet"))
				p.proxy.routeCounters(p, m.Counters)
				break
			}
			dstprocess := p.proxy.servers.Process(dstrid)
			if dstprocess == nil {
				p.SendCloseVC(m.VCI, fmt.Errorf("no server with routing id %v is being proxied", dstrid))
				p.proxy.routeCounters(p, m.Counters)
				break
			}
			srcVCI := m.VCI
			d := p.Route(srcVCI)
			if d == nil {
				// SetupVC involves two messages: One sent by
				// the initiator and one by the acceptor. The
				// routing table gets setup on the first
				// message, so if there is no destination
				// process - setup a routing table entry.
				// If d != nil, then this SetupVC message is
				// likely the one sent by the acceptor.
				dstVCI := dstprocess.AllocVCI()
				startRoutingVC(srcVCI, dstVCI, p, dstprocess)
				if d = p.Route(srcVCI); d == nil {
					p.SendCloseVC(srcVCI, fmt.Errorf("server with routing id %v vanished", dstrid))
					p.proxy.routeCounters(p, m.Counters)
					break
				}
			}
			// Forward the SetupVC message.
			// Typically, a SetupVC message is accompanied with
			// Counters for the new VC.  Keep that in the forwarded
			// message and route the remaining counters separately.
			counters := m.Counters
			m.Counters = message.NewCounters()
			dstVCI := d.VCI
			for cid, bytes := range counters {
				if cid.VCI() == srcVCI {
					m.Counters.Add(dstVCI, cid.Flow(), bytes)
					delete(counters, cid)
				}
			}
			m.VCI = dstVCI
			dstprocess.queue.Put(m)
			p.proxy.routeCounters(p, counters)

		case *message.OpenVC:
			dstrid := m.DstEndpoint.RoutingID()
			if naming.Compare(dstrid, p.proxy.rid) || naming.Compare(dstrid, naming.NullRoutingID) {
				// VC that terminates at the proxy.
				// See protocol.vdl for details on the protocol between the server and the proxy.
				vcObj := p.NewServerVC(m)
				// route counters after creating the VC so counters to vc are not lost.
				p.proxy.routeCounters(p, m.Counters)
				if vcObj != nil {
					server := &server{Process: p, VC: vcObj}
					go p.proxy.runServer(server, vcObj.HandshakeAcceptedVC(vc.LocalPrincipal{p.proxy.principal}))
				}
				break
			}
			dstprocess := p.proxy.servers.Process(dstrid)
			if dstprocess == nil {
				p.SendCloseVC(m.VCI, fmt.Errorf("no server with routing id %v is being proxied", dstrid))
				p.proxy.routeCounters(p, m.Counters)
				break
			}
			srcVCI := m.VCI
			dstVCI := dstprocess.AllocVCI()
			startRoutingVC(srcVCI, dstVCI, p, dstprocess)
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
			dstprocess.queue.Put(m)
			p.proxy.routeCounters(p, counters)
		case *message.HopSetup:
			// Set up the hop.  This takes over the process during negotiation.
			if p.isSetup {
				// Already performed authentication.  We don't do it again.
				processLog().Infof("Process %v is already setup", p)
				return
			}
			var blessings security.Blessings
			if p.proxy.principal != nil {
				blessings = p.proxy.principal.BlessingStore().Default()
			}
			c, err := vif.AuthenticateAsServer(p.conn, p.reader, nil, p.proxy.principal, blessings, nil, m)
			if err != nil {
				processLog().Infof("Process %v failed to authenticate: %s", p, err)
				return
			}
			p.ctrlCipher = c
			p.isSetup = true
		default:
			processLog().Infof("Closing %v because of unrecognized message %T", p, m)
			return
		}
	}
}

func (p *process) String() string {
	r := p.conn.RemoteAddr()
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
	p.queue.Put(&message.CloseVC{VCI: vci, Error: estr})
}

func (p *process) Close() {
	p.mu.Lock()
	if p.routingTable == nil {
		p.mu.Unlock()
		return
	}
	rt := p.routingTable
	p.routingTable = nil
	for _, vc := range p.servers {
		vc.Close("net.Conn is closing")
	}
	p.mu.Unlock()
	for _, d := range rt {
		d.Process.SendCloseVC(d.VCI, errProcessVanished)
	}
	p.bq.Close()
	p.queue.Close()
	p.conn.Close()

	p.proxy.removeProcess(p)
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
		Pool:         p.pool,
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
	if err := p.queue.Put(msg); err != nil {
		processLog().Infof("Failed to send OpenFlow(%+v) on process %v: %v", msg, p, err)
	}
}

func (p *process) AddReceiveBuffers(vci id.VC, fid id.Flow, bytes uint) {
	if bytes == 0 {
		return
	}
	msg := &message.AddReceiveBuffers{Counters: message.NewCounters()}
	msg.Counters.Add(vci, fid, uint32(bytes))
	if err := p.queue.Put(msg); err != nil {
		processLog().Infof("Failed to send AddReceiveBuffers(%+v) on process %v: %v", msg, p, err)
	}
}

func (p *process) NewWriter(vci id.VC, fid id.Flow) (bqueue.Writer, error) {
	return p.bq.NewWriter(packIDs(vci, fid), 0, vc.DefaultBytesBufferedPerFlow)
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
