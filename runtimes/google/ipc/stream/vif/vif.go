package vif

// Logging guidelines:
// vlog.VI(1) for per-net.Conn information
// vlog.VI(2) for per-VC information
// vlog.VI(3) for per-Flow information

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
	"v.io/x/ref/runtimes/google/ipc/stream"
	"v.io/x/ref/runtimes/google/ipc/stream/crypto"
	"v.io/x/ref/runtimes/google/ipc/stream/id"
	"v.io/x/ref/runtimes/google/ipc/stream/message"
	"v.io/x/ref/runtimes/google/ipc/stream/vc"
	"v.io/x/ref/runtimes/google/ipc/version"
	"v.io/x/ref/runtimes/google/lib/bqueue"
	"v.io/x/ref/runtimes/google/lib/bqueue/drrqueue"
	"v.io/x/ref/runtimes/google/lib/iobuf"
	"v.io/x/ref/runtimes/google/lib/pcqueue"
	vsync "v.io/x/ref/runtimes/google/lib/sync"
	"v.io/x/ref/runtimes/google/lib/upcqueue"
)

const pkgPath = "v.io/x/ref/runtimes/google/ipc/stream/vif"

var (
	errShuttingDown = verror.Register(pkgPath+".errShuttingDown", verror.NoRetry, "{1:}{2:} underlying network connection({3}) shutting down{:_}")
)

// VIF implements a "virtual interface" over an underlying network connection
// (net.Conn). Just like multiple network connections can be established over a
// single physical interface, multiple Virtual Circuits (VCs) can be
// established over a single VIF.
type VIF struct {
	// All reads must be performed through reader, and not directly through conn.
	conn    net.Conn
	pool    *iobuf.Pool
	reader  *iobuf.Reader
	localEP naming.Endpoint

	// control channel encryption.
	isSetup bool
	// ctrlCipher is normally guarded by writeMu, however see the exception in
	// readLoop.
	ctrlCipher crypto.ControlCipher
	writeMu    sync.Mutex

	vcMap              *vcMap
	wpending, rpending vsync.WaitGroup

	muListen     sync.Mutex
	acceptor     *upcqueue.T          // GUARDED_BY(muListen)
	listenerOpts []stream.ListenerOpt // GUARDED_BY(muListen)

	muNextVCI sync.Mutex
	nextVCI   id.VC

	outgoing bqueue.T
	expressQ bqueue.Writer

	flowQ        bqueue.Writer
	flowMu       sync.Mutex
	flowCounters message.Counters

	stopQ bqueue.Writer

	// The IPC version range supported by this VIF.  In practice this is
	// non-nil only in testing.  nil is equivalent to using the versions
	// actually supported by this IPC implementation (which is always
	// what you want outside of tests).
	versions *version.Range

	isClosedMu sync.Mutex
	isClosed   bool // GUARDED_BY(isClosedMu)

	// All sets that this VIF is in.
	muSets sync.Mutex
	sets   []*Set // GUARDED_BY(muSets)

	// These counters track the number of messages sent and received by
	// this VIF.
	muMsgCounters sync.Mutex
	msgCounters   map[string]int64
}

// ConnectorAndFlow represents a Flow and the Connector that can be used to
// create another Flow over the same underlying VC.
type ConnectorAndFlow struct {
	Connector stream.Connector
	Flow      stream.Flow
}

// Separate out constants that are not exported so that godoc looks nicer for
// the exported ones.
const (
	// Priorities of the buffered queues used for flow control of writes.
	expressPriority bqueue.Priority = iota
	flowPriority
	normalPriority
	stopPriority

	// Convenience aliases so that the package name "vc" does not
	// conflict with the variables named "vc".
	defaultBytesBufferedPerFlow = vc.DefaultBytesBufferedPerFlow
	sharedFlowID                = vc.SharedFlowID
)

var (
	errAlreadySetup = errors.New("VIF is already setup")
)

// InternalNewDialedVIF creates a new virtual interface over the provided
// network connection, under the assumption that the conn object was created
// using net.Dial.
//
// As the name suggests, this method is intended for use only within packages
// placed inside veyron/runtimes/google. Code outside the
// veyron2/runtimes/google/* packages should never call this method.
func InternalNewDialedVIF(conn net.Conn, rid naming.RoutingID, versions *version.Range, opts ...stream.VCOpt) (*VIF, error) {
	ctx, principal, err := clientAuthOptions(opts)
	if err != nil {
		return nil, err
	}
	if ctx != nil {
		var span vtrace.Span
		ctx, span = vtrace.SetNewSpan(ctx, "InternalNewDialedVIF")
		span.Annotatef("(%v, %v)", conn.RemoteAddr().Network(), conn.RemoteAddr())
		defer span.Finish()
	}
	pool := iobuf.NewPool(0)
	reader := iobuf.NewReader(pool, conn)
	params := security.CallParams{LocalPrincipal: principal, LocalEndpoint: localEP(conn, rid, versions)}

	// TODO(ataly, ashankar, suharshs): Figure out what authorization policy to use
	// for authenticating the server during VIF establishment. Note that we cannot
	// use the VC.ServerAuthorizer available in 'opts' as that applies to the end
	// server and not the remote endpoint of the VIF.
	c, err := AuthenticateAsClient(conn, reader, versions, params, nil)
	if err != nil {
		return nil, err
	}
	return internalNew(conn, pool, reader, rid, id.VC(vc.NumReservedVCs), versions, nil, nil, c)
}

// InternalNewAcceptedVIF creates a new virtual interface over the provided
// network connection, under the assumption that the conn object was created
// using an Accept call on a net.Listener object.
//
// The returned VIF is also setup for accepting new VCs and Flows with the provided
// ListenerOpts.
//
// As the name suggests, this method is intended for use only within packages
// placed inside veyron/runtimes/google. Code outside the
// veyron/runtimes/google/* packages should never call this method.
func InternalNewAcceptedVIF(conn net.Conn, rid naming.RoutingID, versions *version.Range, lopts ...stream.ListenerOpt) (*VIF, error) {
	pool := iobuf.NewPool(0)
	reader := iobuf.NewReader(pool, conn)
	return internalNew(conn, pool, reader, rid, id.VC(vc.NumReservedVCs)+1, versions, upcqueue.New(), lopts, &crypto.NullControlCipher{})
}

func internalNew(conn net.Conn, pool *iobuf.Pool, reader *iobuf.Reader, rid naming.RoutingID, initialVCI id.VC, versions *version.Range, acceptor *upcqueue.T, listenerOpts []stream.ListenerOpt, c crypto.ControlCipher) (*VIF, error) {
	var (
		// Choose IDs that will not conflict with any other (VC, Flow)
		// pairs.  VCI 0 is never used by the application (it is
		// reserved for control messages), so steal from the Flow space
		// there.
		expressID bqueue.ID = packIDs(0, 0)
		flowID    bqueue.ID = packIDs(0, 1)
		stopID    bqueue.ID = packIDs(0, 2)
	)
	outgoing := drrqueue.New(vc.MaxPayloadSizeBytes)

	expressQ, err := outgoing.NewWriter(expressID, expressPriority, defaultBytesBufferedPerFlow)
	if err != nil {
		return nil, fmt.Errorf("failed to create bqueue.Writer for express messages: %v", err)
	}
	expressQ.Release(-1) // Disable flow control

	flowQ, err := outgoing.NewWriter(flowID, flowPriority, flowToken.Size())
	if err != nil {
		return nil, fmt.Errorf("failed to create bqueue.Writer for flow control counters: %v", err)
	}
	flowQ.Release(-1) // Disable flow control

	stopQ, err := outgoing.NewWriter(stopID, stopPriority, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create bqueue.Writer for stopping the write loop: %v", err)
	}
	stopQ.Release(-1) // Disable flow control

	vif := &VIF{
		conn:         conn,
		pool:         pool,
		reader:       reader,
		ctrlCipher:   c,
		vcMap:        newVCMap(),
		acceptor:     acceptor,
		listenerOpts: listenerOpts,
		localEP:      localEP(conn, rid, versions),
		nextVCI:      initialVCI,
		outgoing:     outgoing,
		expressQ:     expressQ,
		flowQ:        flowQ,
		flowCounters: message.NewCounters(),
		stopQ:        stopQ,
		versions:     versions,
		msgCounters:  make(map[string]int64),
	}
	go vif.readLoop()
	go vif.writeLoop()
	return vif, nil
}

// Dial creates a new VC to the provided remote identity, authenticating the VC
// with the provided local identity.
func (vif *VIF) Dial(remoteEP naming.Endpoint, opts ...stream.VCOpt) (stream.VC, error) {
	vc, err := vif.newVC(vif.allocVCI(), vif.localEP, remoteEP, true)
	if err != nil {
		return nil, err
	}
	counters := message.NewCounters()
	counters.Add(vc.VCI(), sharedFlowID, defaultBytesBufferedPerFlow)
	// TODO(ashankar,mattr): If remoteEP/localEP version ranges allow, then
	// use message.SetupVC instead of message.OpenVC.
	// Rough outline:
	// (1) Switch to NaclBox for VC encryption (thus the VC handshake will
	// no longer require the TLS flow and roundtrips for that).
	// (2) Send an appropriate SetupVC message in response to a received
	// SetupVC message.
	// (3) Use the SetupVC received from the remote end to establish the
	// exact protocol version to use.
	err = vif.sendOnExpressQ(&message.OpenVC{
		VCI:         vc.VCI(),
		DstEndpoint: remoteEP,
		SrcEndpoint: vif.localEP,
		Counters:    counters})
	if err != nil {
		err = fmt.Errorf("vif.sendOnExpressQ(OpenVC) failed: %v", err)
		vc.Close(err.Error())
		return nil, err
	}
	if err := vc.HandshakeDialedVC(opts...); err != nil {
		vif.vcMap.Delete(vc.VCI())
		err = fmt.Errorf("VC handshake failed: %v", err)
		vc.Close(err.Error())
		return nil, err
	}
	return vc, nil
}

// addSet adds a set to the list of sets this VIF is in. This method is called
// by Set.Insert().
func (vif *VIF) addSet(s *Set) {
	vif.muSets.Lock()
	defer vif.muSets.Unlock()
	vif.sets = append(vif.sets, s)
}

// removeSet removes a set from the list of sets this VIF is in. This method is
// called by Set.Delete().
func (vif *VIF) removeSet(s *Set) {
	vif.muSets.Lock()
	defer vif.muSets.Unlock()
	for ix, vs := range vif.sets {
		if vs == s {
			vif.sets = append(vif.sets[:ix], vif.sets[ix+1:]...)
			return
		}
	}

}

// Close closes all VCs (and thereby Flows) over the VIF and then closes the
// underlying network connection after draining all pending writes on those
// VCs.
func (vif *VIF) Close() {
	vif.isClosedMu.Lock()
	if vif.isClosed {
		vif.isClosedMu.Unlock()
		return
	}
	vif.isClosed = true
	vif.isClosedMu.Unlock()

	vif.muSets.Lock()
	sets := vif.sets
	vif.sets = nil
	vif.muSets.Unlock()
	for _, s := range sets {
		s.Delete(vif)
	}

	vlog.VI(1).Infof("Closing VIF %s", vif)
	// Stop accepting new VCs.
	vif.StopAccepting()
	// Close local datastructures for all existing VCs.
	vcs := vif.vcMap.Freeze()
	for _, vc := range vcs {
		vc.VC.Close("VIF is being closed")
	}
	// Wait for the vcWriteLoops to exit (after draining queued up messages).
	vif.stopQ.Close()
	vif.wpending.Wait()
	// Close the underlying network connection.
	// No need to send individual messages to close all pending VCs since
	// the remote end should know to close all VCs when the VIF's
	// connection breaks.
	if err := vif.conn.Close(); err != nil {
		vlog.VI(1).Infof("net.Conn.Close failed on VIF %s: %v", vif, err)
	}
}

// StartAccepting begins accepting Flows (and VCs) initiated by the remote end
// of a VIF. opts is used to setup the listener on newly established VCs.
func (vif *VIF) StartAccepting(opts ...stream.ListenerOpt) error {
	vif.muListen.Lock()
	defer vif.muListen.Unlock()
	if vif.acceptor != nil {
		return fmt.Errorf("already accepting Flows on VIF %v", vif)
	}
	vif.acceptor = upcqueue.New()
	vif.listenerOpts = opts
	return nil
}

// StopAccepting prevents any Flows initiated by the remote end of a VIF from
// being accepted and causes any existing and future calls to Accept to fail
// immediately.
func (vif *VIF) StopAccepting() {
	vif.muListen.Lock()
	defer vif.muListen.Unlock()
	if vif.acceptor != nil {
		vif.acceptor.Shutdown()
		vif.acceptor = nil
		vif.listenerOpts = nil
	}
}

// Accept returns the (stream.Connector, stream.Flow) pair of a newly
// established VC and/or Flow.
//
// Sample usage:
//	for {
//		cAndf, err := vif.Accept()
//		switch {
//		case err != nil:
//			fmt.Println("Accept error:", err)
//			return
//		case cAndf.Flow == nil:
//			fmt.Println("New VC established:", cAndf.Connector)
//		default:
//			fmt.Println("New flow established")
//			go handleFlow(cAndf.Flow)
//		}
//	}
func (vif *VIF) Accept() (ConnectorAndFlow, error) {
	vif.muListen.Lock()
	acceptor := vif.acceptor
	vif.muListen.Unlock()
	if acceptor == nil {
		return ConnectorAndFlow{}, fmt.Errorf("VCs not accepted on VIF %v", vif)
	}
	item, err := acceptor.Get(nil)
	if err != nil {
		return ConnectorAndFlow{}, fmt.Errorf("Accept failed: %v", err)
	}
	return item.(ConnectorAndFlow), nil
}

func (vif *VIF) String() string {
	l := vif.conn.LocalAddr()
	r := vif.conn.RemoteAddr()
	return fmt.Sprintf("(%s, %s) <-> (%s, %s)", r.Network(), r, l.Network(), l)
}

func (vif *VIF) readLoop() {
	defer vif.Close()
	defer vif.stopVCDispatchLoops()
	for {
		// vif.ctrlCipher is guarded by vif.writeMu.  However, the only mutation
		// to it is in handleMessage, which runs in the same goroutine, so a
		// lock is not required here.
		msg, err := message.ReadFrom(vif.reader, vif.ctrlCipher)
		if err != nil {
			vlog.VI(1).Infof("Exiting readLoop of VIF %s because of read error: %v", vif, err)
			return
		}
		vlog.VI(3).Infof("Received %T = [%v] on VIF %s", msg, msg, vif)
		if err := vif.handleMessage(msg); err != nil {
			vlog.VI(1).Infof("Exiting readLoop of VIF %s because of message error: %v", vif, err)
			return
		}
	}
}

// handleMessage handles a single incoming message.  Any error returned is
// fatal, causing the VIF to close.
func (vif *VIF) handleMessage(msg message.T) error {
	vif.muMsgCounters.Lock()
	vif.msgCounters[fmt.Sprintf("Recv(%T)", msg)]++
	vif.muMsgCounters.Unlock()

	switch m := msg.(type) {
	case *message.Data:
		_, rq, _ := vif.vcMap.Find(m.VCI)
		if rq == nil {
			vlog.VI(2).Infof("Ignoring message of %d bytes for unrecognized VCI %d on VIF %s", m.Payload.Size(), m.VCI, vif)
			m.Release()
			return nil
		}
		if err := rq.Put(m, nil); err != nil {
			vlog.VI(2).Infof("Failed to put message(%v) on VC queue on VIF %v: %v", m, vif, err)
			m.Release()
		}
	case *message.OpenVC:
		vif.muListen.Lock()
		closed := vif.acceptor == nil || vif.acceptor.IsClosed()
		lopts := vif.listenerOpts
		vif.muListen.Unlock()
		if closed {
			vlog.VI(2).Infof("Ignoring OpenVC message %+v as VIF %s does not accept VCs", m, vif)
			vif.sendOnExpressQ(&message.CloseVC{
				VCI:   m.VCI,
				Error: "VCs not accepted",
			})
			return nil
		}
		vc, err := vif.newVC(m.VCI, m.DstEndpoint, m.SrcEndpoint, false)
		vif.distributeCounters(m.Counters)
		if err != nil {
			vif.sendOnExpressQ(&message.CloseVC{
				VCI:   m.VCI,
				Error: err.Error(),
			})
			return nil
		}
		go vif.acceptFlowsLoop(vc, vc.HandshakeAcceptedVC(lopts...))
	case *message.SetupVC:
		// TODO(ashankar,mattr): Handle this! See comment about SetupVC
		// in vif.Dial
		vif.distributeCounters(m.Counters)
		vif.sendOnExpressQ(&message.CloseVC{
			VCI:   m.VCI,
			Error: "SetupVC handling not implemented yet",
		})
		vlog.VI(2).Infof("Received SetupVC message, but handling not yet implemented")
	case *message.CloseVC:
		if vc, _, _ := vif.vcMap.Find(m.VCI); vc != nil {
			vif.vcMap.Delete(vc.VCI())
			vlog.VI(2).Infof("CloseVC(%+v) on VIF %s", m, vif)
			// TODO(cnicolaou): it would be nice to have a method on VC
			// to indicate a 'remote close' rather than a 'local one'. This helps
			// with error reporting since we expect reads/writes to occur
			// after a remote close, but not after a local close.
			vc.Close(fmt.Sprintf("remote end closed VC(%v)", m.Error))
			return nil
		}
		vlog.VI(2).Infof("Ignoring CloseVC(%+v) for unrecognized VCI on VIF %s", m, vif)
	case *message.AddReceiveBuffers:
		vif.distributeCounters(m.Counters)
	case *message.OpenFlow:
		if vc, _, _ := vif.vcMap.Find(m.VCI); vc != nil {
			if err := vc.AcceptFlow(m.Flow); err != nil {
				vlog.VI(3).Infof("OpenFlow %+v on VIF %v failed:%v", m, vif, err)
				cm := &message.Data{VCI: m.VCI, Flow: m.Flow}
				cm.SetClose()
				vif.sendOnExpressQ(cm)
				return nil
			}
			vc.ReleaseCounters(m.Flow, m.InitialCounters)
			return nil
		}
		vlog.VI(2).Infof("Ignoring OpenFlow(%+v) for unrecognized VCI on VIF %s", m, m, vif)
	case *message.HopSetup:
		// Configure the VIF.  This takes over the conn during negotiation.
		if vif.isSetup {
			return errAlreadySetup
		}
		vif.muListen.Lock()
		principal, lBlessings, dischargeClient, err := serverAuthOptions(vif.listenerOpts)
		vif.muListen.Unlock()
		if err != nil {
			return errVersionNegotiationFailed
		}
		vif.writeMu.Lock()
		c, err := AuthenticateAsServer(vif.conn, vif.reader, vif.versions, principal, lBlessings, dischargeClient, m)
		if err != nil {
			vif.writeMu.Unlock()
			return err
		}
		vif.ctrlCipher = c
		vif.writeMu.Unlock()
		vif.isSetup = true
	default:
		vlog.Infof("Ignoring unrecognized message %T on VIF %s", m, vif)
	}
	return nil
}

func (vif *VIF) vcDispatchLoop(vc *vc.VC, messages *pcqueue.T) {
	defer vlog.VI(2).Infof("Exiting vcDispatchLoop(%v) on VIF %v", vc, vif)
	defer vif.rpending.Done()
	for {
		qm, err := messages.Get(nil)
		if err != nil {
			return
		}
		m := qm.(*message.Data)
		if err := vc.DispatchPayload(m.Flow, m.Payload); err != nil {
			vlog.VI(2).Infof("Ignoring data message %v for on VIF %v: %v", m, vif, err)
		}
		if m.Close() {
			vif.shutdownFlow(vc, m.Flow)
		}
	}
}

func (vif *VIF) stopVCDispatchLoops() {
	vcs := vif.vcMap.Freeze()
	for _, v := range vcs {
		v.RQ.Close()
	}
	vif.rpending.Wait()
}

func (vif *VIF) acceptFlowsLoop(vc *vc.VC, c <-chan vc.HandshakeResult) {
	hr := <-c
	if hr.Error != nil {
		vif.closeVCAndSendMsg(vc, hr.Error.Error())
		return
	}

	vif.muListen.Lock()
	acceptor := vif.acceptor
	vif.muListen.Unlock()
	if acceptor == nil {
		vif.closeVCAndSendMsg(vc, "Flows no longer being accepted")
		return
	}

	// Notify any listeners that a new VC has been established
	if err := acceptor.Put(ConnectorAndFlow{vc, nil}); err != nil {
		vif.closeVCAndSendMsg(vc, fmt.Sprintf("VC accept failed: %v", err))
		return
	}

	vlog.VI(2).Infof("Running acceptFlowsLoop for VC %v on VIF %v", vc, vif)
	for {
		f, err := hr.Listener.Accept()
		if err != nil {
			vlog.VI(2).Infof("Accept failed on VC %v on VIF %v: %v", vc, vif, err)
			return
		}
		if err := acceptor.Put(ConnectorAndFlow{vc, f}); err != nil {
			vlog.VI(2).Infof("vif.acceptor.Put(%v, %T) on VIF %v failed: %v", vc, f, vif, err)
			f.Close()
			return
		}
	}
}

func (vif *VIF) distributeCounters(counters message.Counters) {
	for cid, bytes := range counters {
		vc, _, _ := vif.vcMap.Find(cid.VCI())
		if vc == nil {
			vlog.VI(2).Infof("Ignoring counters for non-existent VCI %d on VIF %s", cid.VCI(), vif)
			continue
		}
		vc.ReleaseCounters(cid.Flow(), bytes)
	}
}

func (vif *VIF) writeLoop() {
	defer vif.outgoing.Close()
	defer vif.stopVCWriteLoops()
	for {
		writer, bufs, err := vif.outgoing.Get(nil)
		if err != nil {
			vlog.VI(1).Infof("Exiting writeLoop of VIF %s because of bqueue.Get error: %v", vif, err)
			return
		}
		vif.muMsgCounters.Lock()
		vif.msgCounters[fmt.Sprintf("Send(%T)", writer)]++
		vif.muMsgCounters.Unlock()
		switch writer {
		case vif.expressQ:
			for _, b := range bufs {
				if err := vif.writeSerializedMessage(b.Contents); err != nil {
					vlog.VI(1).Infof("Exiting writeLoop of VIF %s because Control message write failed: %s", vif, err)
					releaseBufs(bufs)
					return
				}
				b.Release()
			}
		case vif.flowQ:
			msg := &message.AddReceiveBuffers{}
			// No need to call releaseBufs(bufs) as all bufs are
			// the exact same value: flowToken.
			vif.flowMu.Lock()
			if len(vif.flowCounters) > 0 {
				msg.Counters = vif.flowCounters
				vif.flowCounters = message.NewCounters()
			}
			vif.flowMu.Unlock()
			if len(msg.Counters) > 0 {
				vlog.VI(3).Infof("Sending counters %v on VIF %s", msg.Counters, vif)
				if err := vif.writeMessage(msg); err != nil {
					vlog.VI(1).Infof("Exiting writeLoop of VIF %s because AddReceiveBuffers message write failed: %v", vif, err)
					return
				}
			}
		case vif.stopQ:
			// Lowest-priority queue which will never have any
			// buffers, Close is the only method called on it.
			return
		default:
			vif.writeDataMessages(writer, bufs)
		}
	}
}

func (vif *VIF) vcWriteLoop(vc *vc.VC, messages *pcqueue.T) {
	defer vlog.VI(2).Infof("Exiting vcWriteLoop(%v) on VIF %v", vc, vif)
	defer vif.wpending.Done()
	for {
		qm, err := messages.Get(nil)
		if err != nil {
			return
		}
		m := qm.(*message.Data)
		m.Payload, err = vc.Encrypt(m.Flow, m.Payload)
		if err != nil {
			vlog.Infof("Encryption failed. Flow:%v VC:%v Error:%v", m.Flow, vc, err)
		}
		if m.Close() {
			// The last bytes written on the flow will be sent out
			// on vif.conn. Local datastructures for the flow can
			// be cleaned up now.
			vif.shutdownFlow(vc, m.Flow)
		}
		if err == nil {
			err = vif.writeMessage(m)
		}
		if err != nil {
			// TODO(caprita): Calling closeVCAndSendMsg below causes
			// a race as described in:
			// https://docs.google.com/a/google.com/document/d/1C0kxfYhuOcStdV7tnLZELZpUhfQCZj47B0JrzbE29h8/edit
			//
			// There should be a finer grained way to fix this, and
			// there are likely other instances where we should not
			// be closing the VC.
			//
			// For now, commenting out the line below removes the
			// flakiness from our existing unit tests, but this
			// needs to be revisited and fixed correctly.
			//
			//   vif.closeVCAndSendMsg(vc, fmt.Sprintf("write failure: %v", err))

			// Drain the queue and exit.
			for {
				qm, err := messages.Get(nil)
				if err != nil {
					return
				}
				qm.(*message.Data).Release()
			}
		}
	}
}

func (vif *VIF) stopVCWriteLoops() {
	vcs := vif.vcMap.Freeze()
	for _, v := range vcs {
		v.WQ.Close()
	}
}

// sendOnExpressQ adds 'msg' to the expressQ (highest priority queue) of messages to write on the wire.
func (vif *VIF) sendOnExpressQ(msg message.T) error {
	vlog.VI(2).Infof("sendOnExpressQ(%T = %+v) on VIF %s", msg, msg, vif)
	var buf bytes.Buffer
	// Don't encrypt yet, because the message ordering isn't yet determined.
	// Encryption is performed by vif.writeSerializedMessage() when the
	// message is actually written to vif.conn.
	vif.writeMu.Lock()
	c := vif.ctrlCipher
	vif.writeMu.Unlock()
	if err := message.WriteTo(&buf, msg, crypto.NewDisabledControlCipher(c)); err != nil {
		return err
	}
	return vif.expressQ.Put(iobuf.NewSlice(buf.Bytes()), nil)
}

// writeMessage writes the message to the channel.  Writes must be serialized so
// that the control channel can be encrypted, so we acquire the writeMu.
func (vif *VIF) writeMessage(msg message.T) error {
	vif.writeMu.Lock()
	defer vif.writeMu.Unlock()
	return message.WriteTo(vif.conn, msg, vif.ctrlCipher)
}

// Write writes the message to the channel, encrypting the control data.  Writes
// must be serialized so that the control channel can be encrypted, so we
// acquire the writeMu.
func (vif *VIF) writeSerializedMessage(msg []byte) error {
	vif.writeMu.Lock()
	defer vif.writeMu.Unlock()
	if err := message.EncryptMessage(msg, vif.ctrlCipher); err != nil {
		return err
	}
	if n, err := vif.conn.Write(msg); err != nil {
		return fmt.Errorf("write failed: got (%d, %v) for %d byte message", n, err, len(msg))
	}
	return nil
}

func (vif *VIF) writeDataMessages(writer bqueue.Writer, bufs []*iobuf.Slice) {
	vci, fid := unpackIDs(writer.ID())
	// iobuf.Coalesce will coalesce buffers only if they are adjacent to
	// each other.  In the worst case, each buf will be non-adjacent to the
	// others and the code below will end up with multiple small writes
	// instead of a single big one.
	// Might want to investigate this and see if this needs to be
	// revisited.
	bufs = iobuf.Coalesce(bufs, uint(vc.MaxPayloadSizeBytes))
	_, _, wq := vif.vcMap.Find(vci)
	if wq == nil {
		// VC has been removed, stop sending messages
		vlog.VI(2).Infof("VCI %d on VIF %s was shutdown, dropping %d messages that were pending a write", vci, vif, len(bufs))
		releaseBufs(bufs)
		return
	}
	last := len(bufs) - 1
	drained := writer.IsDrained()
	for i, b := range bufs {
		d := &message.Data{VCI: vci, Flow: fid, Payload: b}
		if drained && i == last {
			d.SetClose()
		}
		if err := wq.Put(d, nil); err != nil {
			releaseBufs(bufs[i:])
			return
		}
	}
	if len(bufs) == 0 && drained {
		d := &message.Data{VCI: vci, Flow: fid}
		d.SetClose()
		if err := wq.Put(d, nil); err != nil {
			d.Release()
		}
	}
}

func (vif *VIF) allocVCI() id.VC {
	vif.muNextVCI.Lock()
	ret := vif.nextVCI
	vif.nextVCI += 2
	vif.muNextVCI.Unlock()
	return ret
}

func (vif *VIF) newVC(vci id.VC, localEP, remoteEP naming.Endpoint, dialed bool) (*vc.VC, error) {
	version, err := version.CommonVersion(localEP, remoteEP)
	if vif.versions != nil {
		version, err = vif.versions.CommonVersion(localEP, remoteEP)
	}
	if err != nil {
		return nil, err
	}
	vc := vc.InternalNew(vc.Params{
		VCI:          vci,
		Dialed:       dialed,
		LocalEP:      localEP,
		RemoteEP:     remoteEP,
		Pool:         vif.pool,
		ReserveBytes: uint(message.HeaderSizeBytes + vif.ctrlCipher.MACSize()),
		Helper:       vcHelper{vif},
		Version:      version,
	})
	added, rq, wq := vif.vcMap.Insert(vc)
	// Start vcWriteLoop
	if added = added && vif.wpending.TryAdd(); added {
		go vif.vcWriteLoop(vc, wq)
	}
	// Start vcDispatchLoop
	if added = added && vif.rpending.TryAdd(); added {
		go vif.vcDispatchLoop(vc, rq)
	}
	if !added {
		if rq != nil {
			rq.Close()
		}
		if wq != nil {
			wq.Close()
		}
		vif.vcMap.Delete(vci)
		vc.Close("underlying network connection shutting down")
		// We embed an error inside verror.ErrAborted because other layers
		// check for the "Aborted" error as a special case.  Perhaps
		// eventually we'll get rid of the Aborted layer.
		return nil, verror.New(verror.ErrAborted, nil, verror.New(errShuttingDown, nil, vif))
	}
	return vc, nil
}

func (vif *VIF) closeVCAndSendMsg(vc *vc.VC, msg string) {
	vlog.VI(2).Infof("Shutting down VCI %d on VIF %v due to: %v", vc.VCI(), vif, msg)
	vif.vcMap.Delete(vc.VCI())
	vc.Close(msg)
	if err := vif.sendOnExpressQ(&message.CloseVC{
		VCI:   vc.VCI(),
		Error: msg,
	}); err != nil {
		vlog.VI(2).Infof("sendOnExpressQ(CloseVC{VCI:%d,...}) on VIF %v failed: %v", vc.VCI(), vif, err)
	}
}

// shutdownFlow clears out all the datastructures associated with fid.
func (vif *VIF) shutdownFlow(vc *vc.VC, fid id.Flow) {
	vc.ShutdownFlow(fid)
	vif.flowMu.Lock()
	delete(vif.flowCounters, message.MakeCounterID(vc.VCI(), fid))
	vif.flowMu.Unlock()
}

// ShutdownVCs closes all VCs established to the provided remote endpoint.
// Returns the number of VCs that were closed.
func (vif *VIF) ShutdownVCs(remote naming.Endpoint) int {
	vcs := vif.vcMap.List()
	n := 0
	for _, vc := range vcs {
		if naming.Compare(vc.RemoteAddr().RoutingID(), remote.RoutingID()) {
			vlog.VI(1).Infof("VCI %d on VIF %s being closed because of ShutdownVCs call", vc.VCI(), vif)
			vif.closeVCAndSendMsg(vc, "")
			n++
		}
	}
	return n
}

// NumVCs returns the number of VCs established over this VIF.
func (vif *VIF) NumVCs() int { return vif.vcMap.Size() }

// DebugString returns a descriptive state of the VIF.
//
// The returned string is meant for consumptions by humans. The specific format
// should not be relied upon by any automated processing.
func (vif *VIF) DebugString() string {
	vcs := vif.vcMap.List()
	l := make([]string, 0, len(vcs)+1)

	vif.muNextVCI.Lock() // Needed for vif.nextVCI
	l = append(l, fmt.Sprintf("VIF:[%s] -- #VCs:%d NextVCI:%d ControlChannelEncryption:%v IsClosed:%v", vif, len(vcs), vif.nextVCI, vif.isSetup, vif.isClosed))
	vif.muNextVCI.Unlock()

	for _, vc := range vcs {
		l = append(l, vc.DebugString())
	}

	l = append(l, "Message Counters:")
	ctrs := len(l)
	vif.muMsgCounters.Lock()
	for k, v := range vif.msgCounters {
		l = append(l, fmt.Sprintf(" %-32s %10d", k, v))
	}
	vif.muMsgCounters.Unlock()
	sort.Strings(l[ctrs:])
	return strings.Join(l, "\n")
}

// Methods and type that implement vc.Helper
//
// We create a separate type for vc.Helper to hide the vc.Helper methods
// from the exported method set of VIF.
type vcHelper struct{ vif *VIF }

func (h vcHelper) NotifyOfNewFlow(vci id.VC, fid id.Flow, bytes uint) {
	h.vif.sendOnExpressQ(&message.OpenFlow{VCI: vci, Flow: fid, InitialCounters: uint32(bytes)})
}

func (h vcHelper) AddReceiveBuffers(vci id.VC, fid id.Flow, bytes uint) {
	if bytes == 0 {
		return
	}
	h.vif.flowMu.Lock()
	h.vif.flowCounters.Add(vci, fid, uint32(bytes))
	h.vif.flowMu.Unlock()
	h.vif.flowQ.TryPut(flowToken)
}

func (h vcHelper) NewWriter(vci id.VC, fid id.Flow) (bqueue.Writer, error) {
	return h.vif.outgoing.NewWriter(packIDs(vci, fid), normalPriority, defaultBytesBufferedPerFlow)
}

// The token added to vif.flowQ.
var flowToken *iobuf.Slice

func init() {
	// flowToken must be non-empty otherwise bqueue.Writer.Put will ignore it.
	flowToken = iobuf.NewSlice(make([]byte, 1))
}

func packIDs(vci id.VC, fid id.Flow) bqueue.ID {
	return bqueue.ID(message.MakeCounterID(vci, fid))
}

func unpackIDs(b bqueue.ID) (id.VC, id.Flow) {
	cid := message.CounterID(b)
	return cid.VCI(), cid.Flow()
}

func releaseBufs(bufs []*iobuf.Slice) {
	for _, b := range bufs {
		b.Release()
	}
}

func localEP(conn net.Conn, rid naming.RoutingID, versions *version.Range) naming.Endpoint {
	localAddr := conn.LocalAddr()
	ep := version.Endpoint(localAddr.Network(), localAddr.String(), rid)
	if versions != nil {
		ep = versions.Endpoint(localAddr.Network(), localAddr.String(), rid)
	}
	return ep
}

// clientAuthOptions extracts the client authentication options from the options
// list.
func clientAuthOptions(lopts []stream.VCOpt) (ctx *context.T, principal security.Principal, err error) {
	var securityLevel options.VCSecurityLevel
	for _, o := range lopts {
		switch v := o.(type) {
		case vc.DialContext:
			ctx = v.T
		case vc.LocalPrincipal:
			principal = v.Principal
		case options.VCSecurityLevel:
			securityLevel = v
		}
	}
	switch securityLevel {
	case options.VCSecurityConfidential:
		if principal == nil {
			principal = vc.AnonymousPrincipal
		}
	case options.VCSecurityNone:
		principal = nil
	default:
		err = fmt.Errorf("unrecognized VC security level: %v", securityLevel)
	}
	return
}
