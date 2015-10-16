// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"v.io/x/lib/netstate"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vom"

	"v.io/x/ref/runtime/internal/lib/bqueue"
	"v.io/x/ref/runtime/internal/lib/bqueue/drrqueue"
	"v.io/x/ref/runtime/internal/lib/iobuf"
	"v.io/x/ref/runtime/internal/lib/publisher"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/crypto"
	"v.io/x/ref/runtime/internal/rpc/stream/id"
	"v.io/x/ref/runtime/internal/rpc/stream/message"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	"v.io/x/ref/runtime/internal/rpc/stream/vif"
	iversion "v.io/x/ref/runtime/internal/rpc/version"

	"v.io/x/ref/lib/stats"
)

const pkgPath = "v.io/x/ref/runtime/proxy"

func reg(id, msg string) verror.IDAction {
	return verror.Register(verror.ID(pkgPath+id), verror.NoRetry, msg)
}

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errNoRoutingTableEntry       = reg(".errNoRoutingTableEntry", "routing table has no entry for the VC")
	errProcessVanished           = reg(".errProcessVanished", "remote process vanished")
	errDuplicateSetupVC          = reg(".errDuplicateSetupVC", "duplicate SetupVC request")
	errVomEncodeResponse         = reg(".errVomEncodeResponse", "failed to encode response from proxy{:3}")
	errNoRequest                 = reg(".errNoRequest", "unable to read Request{:3}")
	errServerClosedByProxy       = reg(".errServerClosedByProxy", "server closed by proxy")
	errRemoveServerVC            = reg(".errRemoveServerVC", "failed to remove server VC {3}{:4}")
	errNetConnClosing            = reg(".errNetConnClosing", "net.Conn is closing")
	errFailedToAcceptHealthCheck = reg(".errFailedToAcceptHealthCheck", "failed to accept health check flow")
	errIncompatibleVersions      = reg(".errIncompatibleVersions", "{:3}")
	errAlreadyProxied            = reg(".errAlreadyProxied", "server with routing id {3} is already being proxied")
	errUnknownNetwork            = reg(".errUnknownNetwork", "unknown network {3}")
	errListenFailed              = reg(".errListenFailed", "net.Listen({3}, {4}) failed{:5}")
	errFailedToForwardRxBufs     = reg(".errFailedToForwardRxBufs", "failed to forward receive buffers{:3}")
	errFailedToFowardDataMsg     = reg(".errFailedToFowardDataMsg", "failed to forward data message{:3}")
	errFailedToFowardOpenFlow    = reg(".errFailedToFowardOpenFlow", "failed to forward open flow{:3}")
	errServerNotBeingProxied     = reg(".errServerNotBeingProxied", "no server with routing id {3} is being proxied")
	errServerVanished            = reg(".errServerVanished", "server with routing id {3} vanished")
	errAccessibleAddresses       = reg(".errAccessibleAddresses", "failed to obtain a set of accessible addresses{:3}")
	errNoAccessibleAddresses     = reg(".errNoAccessibleAddresses", "no accessible addresses were available for {3}")
	errEmptyListenSpec           = reg(".errEmptyListenSpec", "no addresses supplied in the listen spec")
)

// Proxy routes virtual circuit (VC) traffic between multiple underlying
// network connections.
type Proxy struct {
	ctx        *context.T
	ln         net.Listener
	rid        naming.RoutingID
	principal  security.Principal
	blessings  security.Blessings
	authorizer security.Authorizer
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
	ctx          *context.T
	conn         net.Conn
	pool         *iobuf.Pool
	reader       *iobuf.Reader
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

func (s *server) RoutingID() naming.RoutingID { return s.VC.RemoteEndpoint().RoutingID() }

func (s *server) Close(err error) {
	if vc := s.Process.RemoveServerVC(s.VC.VCI()); vc != nil {
		if err != nil {
			vc.Close(verror.New(stream.ErrProxy, nil, verror.New(errRemoveServerVC, nil, s.VC.VCI(), err)))
		} else {
			vc.Close(verror.New(stream.ErrProxy, nil, verror.New(errServerClosedByProxy, nil)))
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
	ctx *context.T
	mu  sync.Mutex
	m   map[naming.RoutingID]*server
}

func (m *servermap) Add(server *server) error {
	key := server.RoutingID()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m[key] != nil {
		return verror.New(stream.ErrProxy, nil, verror.New(errAlreadyProxied, nil, key))
	}
	m.m[key] = server
	proxyLog(m.ctx, "Started proxying server: %v", server)
	return nil
}

func (m *servermap) Remove(server *server) {
	key := server.RoutingID()
	m.mu.Lock()
	if m.m[key] != nil {
		delete(m.m, key)
		proxyLog(m.ctx, "Stopped proxying server: %v", server)
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
// ListenSpec and routes VC traffic between accepted connections.
//
// Servers wanting to "listen through the proxy" will only be allowed to do so
// if the blessings they present are accepted to the provided authorization
// policy (authorizer).
func New(ctx *context.T, spec rpc.ListenSpec, authorizer security.Authorizer, names ...string) (shutdown func(), endpoint naming.Endpoint, err error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, nil, err
	}
	proxy, err := internalNew(rid, ctx, spec, authorizer)
	if err != nil {
		return nil, nil, err
	}

	var pub publisher.Publisher
	for _, name := range names {
		if name == "" {
			// Consistent with v23.rpc.Server.Serve(...)
			// an empty name implies, "do not publish"
			continue
		}
		if pub == nil {
			pub = publisher.New(ctx, v23.GetNamespace(ctx), time.Minute)
			pub.AddServer(proxy.endpoint().String())
		}
		pub.AddName(name, false, true)
	}

	shutdown = func() {
		if pub != nil {
			pub.Stop()
			pub.WaitForStop()
		}
		proxy.shutdown()
	}
	return shutdown, proxy.endpoint(), nil
}

func internalNew(rid naming.RoutingID, ctx *context.T, spec rpc.ListenSpec, authorizer security.Authorizer) (*Proxy, error) {
	if len(spec.Addrs) == 0 {
		return nil, verror.New(stream.ErrProxy, nil, verror.New(errEmptyListenSpec, nil))
	}
	laddr := spec.Addrs[0]
	network := laddr.Protocol
	address := laddr.Address
	_, _, listenFn, _ := rpc.RegisteredProtocol(network)
	if listenFn == nil {
		return nil, verror.New(stream.ErrProxy, nil, verror.New(errUnknownNetwork, nil, network))
	}
	ln, err := listenFn(ctx, network, address)
	if err != nil {
		return nil, verror.New(stream.ErrProxy, nil, verror.New(errListenFailed, nil, network, address, err))
	}
	pub, _, err := netstate.PossibleAddresses(ln.Addr().Network(), ln.Addr().String(), spec.AddressChooser)
	if err != nil {
		ln.Close()
		return nil, verror.New(stream.ErrProxy, nil, verror.New(errAccessibleAddresses, nil, err))
	}
	if len(pub) == 0 {
		ln.Close()
		return nil, verror.New(stream.ErrProxy, nil, verror.New(errNoAccessibleAddresses, nil, ln.Addr().String()))
	}
	if authorizer == nil {
		authorizer = security.DefaultAuthorizer()
	}
	proxy := &Proxy{
		ctx:        ctx,
		ln:         ln,
		rid:        rid,
		authorizer: authorizer,
		servers:    &servermap{ctx: ctx, m: make(map[naming.RoutingID]*server)},
		processes:  make(map[*process]struct{}),
		// TODO(cnicolaou): should use all of the available addresses
		pubAddress: pub[0].String(),
		principal:  v23.GetPrincipal(ctx),
		statsName:  naming.Join("rpc", "proxy", "routing-id", rid.String(), "debug"),
	}
	if proxy.principal != nil {
		proxy.blessings = proxy.principal.BlessingStore().Default()
	}
	stats.NewStringFunc(proxy.statsName, proxy.debugString)

	go proxy.listenLoop()
	return proxy, nil
}

func (p *Proxy) listenLoop() {
	proxyLog(p.ctx, "Proxy listening on (%q, %q): %v", p.ln.Addr().Network(), p.ln.Addr(), p.endpoint())
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			proxyLog(p.ctx, "Exiting listenLoop of proxy %q: %v", p.endpoint(), err)
			return
		}
		go p.acceptProcess(conn)
	}
}

func (p *Proxy) acceptProcess(conn net.Conn) {
	pool := iobuf.NewPool(0)
	reader := iobuf.NewReader(pool, conn)

	var blessings security.Blessings
	if p.principal != nil {
		blessings = p.principal.BlessingStore().Default()
	}

	cipher, _, err := vif.AuthenticateAsServer(conn, reader, nil, nil, p.principal, blessings, nil)
	if err != nil {
		processLog(p.ctx, "Process %v failed to authenticate: %s", p, err)
		return
	}

	process := &process{
		ctx:          p.ctx,
		proxy:        p,
		conn:         conn,
		pool:         pool,
		reader:       reader,
		ctrlCipher:   cipher,
		queue:        upcqueue.New(),
		routingTable: make(map[id.VC]*destination),
		servers:      make(map[id.VC]*vc.VC),
		bq:           drrqueue.New(vc.MaxPayloadSizeBytes),
	}

	p.mu.Lock()
	if p.processes == nil {
		// The proxy has been shutdowned.
		p.mu.Unlock()
		return
	}
	p.processes[process] = struct{}{}
	p.mu.Unlock()

	go process.serverVCsLoop()
	go process.writeLoop()
	go process.readLoop()

	processLog(p.ctx, "Started process %v", process)
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
		server.Close(verror.New(stream.ErrProxy, nil, verror.New(errFailedToAcceptHealthCheck, nil)))
		return
	}
	server.Process.InitVCI(server.VC.VCI())
	var request Request
	var response Response
	dec := vom.NewDecoder(conn)
	if err := dec.Decode(&request); err != nil {
		response.Error = verror.New(stream.ErrProxy, nil, verror.New(errNoRequest, nil, err))
	} else if err := p.authorize(server.VC, request); err != nil {
		response.Error = err
	} else if err := p.servers.Add(server); err != nil {
		response.Error = verror.Convert(verror.ErrUnknown, nil, err)
	} else {
		defer p.servers.Remove(server)
		proxyEP := p.endpoint()
		ep := &inaming.Endpoint{
			Protocol: proxyEP.Protocol,
			Address:  proxyEP.Address,
			RID:      server.VC.RemoteEndpoint().RoutingID(),
		}
		response.Endpoint = ep.String()
	}
	enc := vom.NewEncoder(conn)
	if err := enc.Encode(response); err != nil {
		proxyLog(p.ctx, "Failed to encode response %#v for server %v", response, server)
		server.Close(verror.New(stream.ErrProxy, nil, verror.New(errVomEncodeResponse, nil, err)))
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
	server.Close(nil)
}

func (p *Proxy) authorize(vc *vc.VC, request Request) error {
	var dmap map[string]security.Discharge
	if len(request.Discharges) > 0 {
		dmap = make(map[string]security.Discharge)
		for _, d := range request.Discharges {
			dmap[d.ID()] = d
		}
	}
	// Blessings must be bound to the same public key as the VC.
	// (Repeating logic in the RPC server authorization code).
	if got, want := request.Blessings.PublicKey(), vc.RemoteBlessings().PublicKey(); !request.Blessings.IsZero() && !reflect.DeepEqual(got, want) {
		return verror.New(verror.ErrNoAccess, nil, fmt.Errorf("malformed request: Blessings sent in proxy.Request are bound to public key %v and not %v", got, want))
	}
	return p.authorizer.Authorize(p.ctx, security.NewCall(&security.CallParams{
		LocalPrincipal:   vc.LocalPrincipal(),
		LocalBlessings:   vc.LocalBlessings(),
		RemoteBlessings:  request.Blessings,
		LocalEndpoint:    vc.LocalEndpoint(),
		RemoteEndpoint:   vc.RemoteEndpoint(),
		LocalDischarges:  vc.LocalDischarges(),
		RemoteDischarges: dmap,
	}))
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
				process.SendCloseVC(srcVCI, verror.New(stream.ErrProxy, nil, verror.New(errFailedToForwardRxBufs, nil, err)))
			}
		}
	}
}

func startRoutingVC(ctx *context.T, srcVCI, dstVCI id.VC, srcProcess, dstProcess *process) {
	dstProcess.AddRoute(dstVCI, &destination{VCI: srcVCI, Process: srcProcess})
	srcProcess.AddRoute(srcVCI, &destination{VCI: dstVCI, Process: dstProcess})
	vcLog(ctx, "Routing (VCI %d @ [%s]) <-> (VCI %d @ [%s])", srcVCI, srcProcess, dstVCI, dstProcess)
}

// Endpoint returns the endpoint of the proxy service.  By Dialing a VC to this
// endpoint, processes can have their services exported through the proxy.
func (p *Proxy) endpoint() *inaming.Endpoint {

	ep := &inaming.Endpoint{
		Protocol: p.ln.Addr().Network(),
		Address:  p.pubAddress,
		RID:      p.rid,
	}
	if prncpl := p.principal; prncpl != nil {
		ep.Blessings = security.BlessingNames(prncpl, prncpl.BlessingStore().Default())
	}
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
			queueDataMessages(p.ctx, bufs, vc, fid, p.queue)
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

func queueDataMessages(ctx *context.T, bufs []*iobuf.Slice, vc *vc.VC, fid id.Flow, q *upcqueue.T) {
	for ix, b := range bufs {
		m := &message.Data{VCI: vc.VCI(), Flow: fid}
		var err error
		if m.Payload, err = vc.Encrypt(fid, b); err != nil {
			msgLog(ctx, "vc.Encrypt failed. VC:%v Flow:%v Error:%v", vc, fid, err)
			releaseBufs(ix+1, bufs)
			return
		}
		if err = q.Put(m); err != nil {
			msgLog(ctx, "Failed to enqueue data message %v: %v", m, err)
			m.Release()
			releaseBufs(ix+1, bufs)
			return
		}
	}
}

func (p *process) writeLoop() {
	defer processLog(p.ctx, "Exited writeLoop for %v", p)
	defer p.Close()

	for {
		item, err := p.queue.Get(nil)
		if err != nil {
			if err != upcqueue.ErrQueueIsClosed {
				processLog(p.ctx, "upcqueue.Get failed on %v: %v", p, err)
			}
			return
		}
		if err = message.WriteTo(p.conn, item.(message.T), p.ctrlCipher); err != nil {
			processLog(p.ctx, "message.WriteTo on %v failed: %v", p, err)
			return
		}
	}
}

func (p *process) readLoop() {
	defer processLog(p.ctx, "Exited readLoop for %v", p)
	defer p.Close()

	for {
		msg, err := message.ReadFrom(p.reader, p.ctrlCipher)
		if err != nil {
			processLog(p.ctx, "Read on %v failed: %v", p, err)
			return
		}
		msgLog(p.ctx, "Received msg: %T = %v", msg, msg)
		switch m := msg.(type) {
		case *message.Data:
			if vc := p.ServerVC(m.VCI); vc != nil {
				if err := vc.DispatchPayload(m.Flow, m.Payload); err != nil {
					processLog(p.ctx, "Ignoring data message %v from process %v: %v", m, p, err)
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
					p.SendCloseVC(srcVCI, verror.New(stream.ErrProxy, nil, verror.New(errFailedToFowardDataMsg, nil, err)))
				}
				break
			}
			p.SendCloseVC(srcVCI, verror.New(stream.ErrProxy, nil, verror.New(errNoRoutingTableEntry, nil)))
		case *message.OpenFlow:
			if vc := p.ServerVC(m.VCI); vc != nil {
				if err := vc.AcceptFlow(m.Flow); err != nil {
					processLog(p.ctx, "OpenFlow %+v on process %v failed: %v", m, p, err)
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
					p.SendCloseVC(srcVCI, verror.New(stream.ErrProxy, nil, verror.New(errFailedToFowardOpenFlow, nil, err)))
				}
				break
			}
			p.SendCloseVC(srcVCI, verror.New(stream.ErrProxy, nil, verror.New(errNoRoutingTableEntry, nil)))
		case *message.CloseVC:
			if vc := p.RemoveServerVC(m.VCI); vc != nil {
				vc.Close(verror.New(stream.ErrProxy, nil, verror.New(errRemoveServerVC, nil, m.VCI, m.Error)))
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
			// First let's ensure that we can speak a common protocol verison.
			intersection, err := iversion.SupportedRange.Intersect(&m.Setup.Versions)
			if err != nil {
				p.SendCloseVC(m.VCI, verror.New(stream.ErrProxy, nil,
					verror.New(errIncompatibleVersions, nil, err)))
				break
			}

			dstrid := m.RemoteEndpoint.RoutingID()
			if naming.Compare(dstrid, p.proxy.rid) || naming.Compare(dstrid, naming.NullRoutingID) {
				// VC that terminates at the proxy.
				// See protocol.vdl for details on the protocol between the server and the proxy.
				vcObj := p.NewServerVC(p.ctx, m)
				// route counters after creating the VC so counters to vc are not lost.
				p.proxy.routeCounters(p, m.Counters)
				if vcObj != nil {
					server := &server{Process: p, VC: vcObj}
					keyExchanger := func(pubKey *crypto.BoxKey) (*crypto.BoxKey, error) {
						p.queue.Put(&message.SetupVC{
							VCI: m.VCI,
							Setup: message.Setup{
								// Note that servers send clients not their actual supported versions,
								// but the intersected range of the server and client ranges.  This
								// is important because proxies may have adjusted the version ranges
								// along the way, and we should negotiate a version that is compatible
								// with all intermediate hops.
								Versions: *intersection,
								Options:  []message.SetupOption{&message.NaclBox{PublicKey: *pubKey}},
							},
							RemoteEndpoint: m.LocalEndpoint,
							LocalEndpoint:  p.proxy.endpoint(),
							// TODO(mattr): Consider adding counters.  See associated comment
							// in vc.go:VC.HandshakeAcceptedVC for more details.
						})
						var theirPK *crypto.BoxKey
						box := m.Setup.NaclBox()
						if box != nil {
							theirPK = &box.PublicKey
						}
						return theirPK, nil
					}
					go p.proxy.runServer(server, vcObj.HandshakeAcceptedVCWithAuthentication(intersection.Max, p.proxy.principal, p.proxy.blessings, keyExchanger))
				}
				break
			}

			srcVCI := m.VCI

			d := p.Route(srcVCI)
			if d == nil {
				// SetupVC involves two messages: One sent by the initiator
				// and one by the acceptor. The routing table gets setup on
				// the first message, so if there is no route -
				// setup a routing table entry.
				dstprocess := p.proxy.servers.Process(dstrid)
				if dstprocess == nil {
					p.SendCloseVC(m.VCI, verror.New(stream.ErrProxy, nil, verror.New(errServerNotBeingProxied, nil, dstrid)))
					p.proxy.routeCounters(p, m.Counters)
					break
				}
				dstVCI := dstprocess.AllocVCI()
				startRoutingVC(p.ctx, srcVCI, dstVCI, p, dstprocess)
				if d = p.Route(srcVCI); d == nil {
					p.SendCloseVC(srcVCI, verror.New(stream.ErrProxy, nil, verror.New(errServerVanished, nil, dstrid)))
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
			// Note that proxies rewrite the version range so that the final negotiated
			// version will be compatible with all intermediate hops.
			m.Setup.Versions = *intersection
			d.Process.queue.Put(m)
			p.proxy.routeCounters(p, counters)

		default:
			processLog(p.ctx, "Closing %v because of invalid message %T", p, m)
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
		vc.Close(verror.New(stream.ErrProxy, nil, verror.New(errNetConnClosing, nil)))
	}
	p.mu.Unlock()
	for _, d := range rt {
		d.Process.SendCloseVC(d.VCI, verror.New(stream.ErrProxy, nil, verror.New(errProcessVanished, nil)))
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

func (p *process) NewServerVC(ctx *context.T, m *message.SetupVC) *vc.VC {
	p.mu.Lock()
	defer p.mu.Unlock()
	if vc := p.servers[m.VCI]; vc != nil {
		vc.Close(verror.New(stream.ErrProxy, nil, verror.New(errDuplicateSetupVC, nil)))
		return nil
	}
	vc := vc.InternalNew(ctx, vc.Params{
		VCI:          m.VCI,
		LocalEP:      m.RemoteEndpoint,
		RemoteEP:     m.LocalEndpoint,
		Pool:         p.pool,
		ReserveBytes: message.HeaderSizeBytes,
		Helper:       p,
	})
	p.servers[m.VCI] = vc
	proxyLog(p.ctx, "Registered VC %v from server on process %v", vc, p)
	return vc
}

func (p *process) RemoveServerVC(vci id.VC) *vc.VC {
	p.mu.Lock()
	defer p.mu.Unlock()
	if vc := p.servers[vci]; vc != nil {
		delete(p.servers, vci)
		proxyLog(p.ctx, "Unregistered server VC %v from process %v", vc, p)
		return vc
	}
	return nil
}

// Make process implement vc.Helper
func (p *process) NotifyOfNewFlow(vci id.VC, fid id.Flow, bytes uint) {
	msg := &message.OpenFlow{VCI: vci, Flow: fid, InitialCounters: uint32(bytes)}
	if err := p.queue.Put(msg); err != nil {
		processLog(p.ctx, "Failed to send OpenFlow(%+v) on process %v: %v", msg, p, err)
	}
}

func (p *process) AddReceiveBuffers(vci id.VC, fid id.Flow, bytes uint) {
	if bytes == 0 {
		return
	}
	msg := &message.AddReceiveBuffers{Counters: message.NewCounters()}
	msg.Counters.Add(vci, fid, uint32(bytes))
	if err := p.queue.Put(msg); err != nil {
		processLog(p.ctx, "Failed to send AddReceiveBuffers(%+v) on process %v: %v", msg, p, err)
	}
}

func (p *process) NewWriter(vci id.VC, fid id.Flow, priority bqueue.Priority) (bqueue.Writer, error) {
	return p.bq.NewWriter(packIDs(vci, fid), priority, vc.DefaultBytesBufferedPerFlow)
}

// Convenience functions to assist with the logging convention.
func proxyLog(ctx *context.T, format string, args ...interface{}) {
	ctx.VI(1).Infof(format, args...)
}
func processLog(ctx *context.T, format string, args ...interface{}) {
	ctx.VI(2).Infof(format, args...)
}
func vcLog(ctx *context.T, format string, args ...interface{}) {
	ctx.VI(3).Infof(format, args...)
}
func msgLog(ctx *context.T, format string, args ...interface{}) {
	ctx.VI(4).Infof(format, args...)
}

func packIDs(vci id.VC, fid id.Flow) bqueue.ID {
	return bqueue.ID(message.MakeCounterID(vci, fid))
}
func unpackIDs(b bqueue.ID) (id.VC, id.Flow) {
	cid := message.CounterID(b)
	return cid.VCI(), cid.Flow()
}
