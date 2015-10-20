// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

// Logging guidelines:
// .VI(1) for per-net.Conn information
// .VI(2) for per-VC information
// .VI(3) for per-Flow information

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vtrace"

	"v.io/x/ref/runtime/internal/lib/bqueue"
	"v.io/x/ref/runtime/internal/lib/bqueue/drrqueue"
	"v.io/x/ref/runtime/internal/lib/iobuf"
	"v.io/x/ref/runtime/internal/lib/pcqueue"
	vsync "v.io/x/ref/runtime/internal/lib/sync"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/crypto"
	"v.io/x/ref/runtime/internal/rpc/stream/id"
	"v.io/x/ref/runtime/internal/rpc/stream/message"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	iversion "v.io/x/ref/runtime/internal/rpc/version"
)

const pkgPath = "v.io/x/ref/runtime/internal/rpc/stream/vif"

func reg(id, msg string) verror.IDAction {
	return verror.Register(verror.ID(pkgPath+id), verror.NoRetry, msg)
}

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errShuttingDown             = reg(".errShuttingDown", "underlying network connection({3}) shutting down")
	errVCHandshakeFailed        = reg(".errVCHandshakeFailed", "VC handshake failed{:3}")
	errSendOnExpressQFailed     = reg(".errSendOnExpressQFailed", "vif.sendOnExpressQ(OpenVC) failed{:3}")
	errVIFIsBeingClosed         = reg(".errVIFIsBeingClosed", "VIF is being closed")
	errVIFAlreadyAcceptingFlows = reg(".errVIFAlreadyAcceptingFlows", "already accepting flows on VIF {3}")
	errVCsNotAcceptedOnVIF      = reg(".errVCsNotAcceptedOnVIF", "VCs not accepted on VIF {3}")
	errAcceptFailed             = reg(".errAcceptFailed", "Accept failed{:3}")
	errRemoteEndClosedVC        = reg(".errRemoteEndClosedVC", "remote end closed VC{:3}")
	errFlowsNoLongerAccepted    = reg(".errFlowsNowLongerAccepted", "Flows no longer being accepted")
	errVCAcceptFailed           = reg(".errVCAcceptFailed", "VC accept failed{:3}")
	errIdleTimeout              = reg(".errIdleTimeout", "idle timeout")
	errVIFAlreadySetup          = reg(".errVIFAlreadySetupt", "VIF is already setup")
	errBqueueWriterForXpress    = reg(".errBqueueWriterForXpress", "failed to create bqueue.Writer for express messages{:3}")
	errBqueueWriterForControl   = reg(".errBqueueWriterForControl", "failed to create bqueue.Writer for flow control counters{:3}")
	errBqueueWriterForStopping  = reg(".errBqueueWriterForStopping", "failed to create bqueue.Writer for stopping the write loop{:3}")
	errWriteFailed              = reg(".errWriteFailed", "write failed: got ({3}, {4}) for {5} byte message)")
)

// VIF implements a "virtual interface" over an underlying network connection
// (net.Conn). Just like multiple network connections can be established over a
// single physical interface, multiple Virtual Circuits (VCs) can be
// established over a single VIF.
type VIF struct {
	ctx *context.T

	// All reads must be performed through reader, and not directly through conn.
	conn    net.Conn
	pool    *iobuf.Pool
	reader  *iobuf.Reader
	localEP naming.Endpoint

	// ctrlCipher is normally guarded by writeMu, however see the exception in
	// readLoop.
	ctrlCipher crypto.ControlCipher
	writeMu    sync.Mutex

	muStartTimer sync.Mutex
	startTimer   timer

	vcMap              *vcMap
	idleTimerMap       *idleTimerMap
	wpending, rpending vsync.WaitGroup

	muListen     sync.Mutex
	acceptor     *upcqueue.T          // GUARDED_BY(muListen)
	listenerOpts []stream.ListenerOpt // GUARDED_BY(muListen)
	principal    security.Principal
	// TODO(jhahn): Merge this blessing with the one in authResult once
	// we fixed to pass blessings to StartAccepting().
	blessings  security.Blessings
	authResult *AuthenticationResult

	muNextVCI sync.Mutex
	nextVCI   id.VC

	outgoing bqueue.T
	expressQ bqueue.Writer

	flowQ        bqueue.Writer
	flowMu       sync.Mutex
	flowCounters message.Counters

	stopQ bqueue.Writer

	// The RPC version range supported by this VIF.  In practice this is
	// non-nil only in testing.  nil is equivalent to using the versions
	// actually supported by this RPC implementation (which is always
	// what you want outside of tests).
	versions *iversion.Range

	isClosedMu sync.Mutex
	isClosed   bool // GUARDED_BY(isClosedMu)
	onClose    func(*VIF)

	muStats sync.Mutex
	stats   Stats

	loopWG sync.WaitGroup
}

// ConnectorAndFlow represents a Flow and the Connector that can be used to
// create another Flow over the same underlying VC.
type ConnectorAndFlow struct {
	Connector stream.Connector
	Flow      stream.Flow
}

// Stats holds stats for a VIF.
type Stats struct {
	SendMsgCounter map[reflect.Type]uint64
	RecvMsgCounter map[reflect.Type]uint64

	NumDialedVCs        uint
	NumAcceptedVCs      uint
	NumPreAuthenticated uint
}

// Separate out constants that are not exported so that godoc looks nicer for
// the exported ones.
const (
	// Priorities of the buffered queues used for flow control of writes.
	expressPriority bqueue.Priority = iota
	controlPriority
	// The range of flow priorities is [flowPriority, flowPriority + NumFlowPriorities)
	flowPriority
	stopPriority = flowPriority + vc.NumFlowPriorities
)

const (
	// Convenience aliases so that the package name "vc" does not
	// conflict with the variables named "vc".
	defaultBytesBufferedPerFlow = vc.DefaultBytesBufferedPerFlow
	sharedFlowID                = vc.SharedFlowID
)

type vifSide bool

const (
	dialedVIF   vifSide = true
	acceptedVIF vifSide = false
)

// InternalNewDialedVIF creates a new virtual interface over the provided
// network connection, under the assumption that the conn object was created
// using net.Dial. If onClose is given, it is run in its own goroutine when
// the vif has been closed.
//
// As the name suggests, this method is intended for use only within packages
// placed inside v.io/x/ref/runtime/internal. Code outside the
// v.io/x/ref/runtime/internal/* packages should never call this method.
func InternalNewDialedVIF(ctx *context.T, conn net.Conn, rid naming.RoutingID, versions *iversion.Range, onClose func(*VIF), opts ...stream.VCOpt) (*VIF, error) {
	if ctx != nil {
		var span vtrace.Span
		ctx, span = vtrace.WithNewSpan(ctx, "InternalNewDialedVIF")
		span.Annotatef("(%v, %v)", conn.RemoteAddr().Network(), conn.RemoteAddr())
		defer span.Finish()
	}
	principal := stream.GetPrincipalVCOpts(ctx, opts...)
	pool := iobuf.NewPool(0)
	reader := iobuf.NewReader(pool, conn)
	localEP := localEndpoint(conn, rid, versions)

	// TODO(ataly, ashankar, suharshs): Figure out what authorization policy to use
	// for authenticating the server during VIF establishment. Note that we cannot
	// use the VC.ServerAuthorizer available in 'opts' as that applies to the end
	// server and not the remote endpoint of the VIF.
	c, authr, err := AuthenticateAsClient(conn, reader, localEP, versions, principal, nil)
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, ctx, err)
	}
	var blessings security.Blessings
	if principal != nil {
		blessings = principal.BlessingStore().Default()
	}
	var startTimeout time.Duration
	for _, o := range opts {
		switch v := o.(type) {
		case vc.StartTimeout:
			startTimeout = v.Duration
		}
	}
	return internalNew(ctx, conn, pool, reader, localEP, id.VC(vc.NumReservedVCs), versions, principal, blessings, startTimeout, onClose, nil, nil, c, authr)
}

// InternalNewAcceptedVIF creates a new virtual interface over the provided
// network connection, under the assumption that the conn object was created
// using an Accept call on a net.Listener object. If onClose is given, it is
// run in its own goroutine when the vif has been closed.
//
// The returned VIF is also setup for accepting new VCs and Flows with the provided
// ListenerOpts.
//
// As the name suggests, this method is intended for use only within packages
// placed inside v.io/x/ref/runtime/internal. Code outside the
// v.io/x/ref/runtime/internal/* packages should never call this method.
func InternalNewAcceptedVIF(ctx *context.T, conn net.Conn, rid naming.RoutingID, blessings security.Blessings, versions *iversion.Range, onClose func(*VIF), lopts ...stream.ListenerOpt) (*VIF, error) {
	pool := iobuf.NewPool(0)
	reader := iobuf.NewReader(pool, conn)
	localEP := localEndpoint(conn, rid, versions)
	dischargeClient := getDischargeClient(lopts)
	principal := stream.GetPrincipalListenerOpts(ctx, lopts...)
	c, authr, err := AuthenticateAsServer(conn, reader, localEP, versions, principal, blessings, dischargeClient)
	if err != nil {
		return nil, err
	}

	var startTimeout time.Duration
	for _, o := range lopts {
		switch v := o.(type) {
		case vc.StartTimeout:
			startTimeout = v.Duration
		}
	}
	return internalNew(ctx, conn, pool, reader, localEP, id.VC(vc.NumReservedVCs)+1, versions, principal, blessings, startTimeout, onClose, upcqueue.New(), lopts, c, authr)
}

func internalNew(ctx *context.T, conn net.Conn, pool *iobuf.Pool, reader *iobuf.Reader, localEP naming.Endpoint, initialVCI id.VC, versions *iversion.Range, principal security.Principal, blessings security.Blessings, startTimeout time.Duration, onClose func(*VIF), acceptor *upcqueue.T, listenerOpts []stream.ListenerOpt, c crypto.ControlCipher, authr *AuthenticationResult) (*VIF, error) {
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
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errBqueueWriterForXpress, nil, err))
	}
	expressQ.Release(-1) // Disable flow control

	flowQ, err := outgoing.NewWriter(flowID, controlPriority, flowToken.Size())
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errBqueueWriterForControl, nil, err))
	}
	flowQ.Release(-1) // Disable flow control

	stopQ, err := outgoing.NewWriter(stopID, stopPriority, 1)
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, verror.New(errBqueueWriterForStopping, nil, err))
	}
	stopQ.Release(-1) // Disable flow control

	if versions == nil {
		versions = iversion.SupportedRange
	}

	vif := &VIF{
		ctx:          ctx,
		conn:         conn,
		pool:         pool,
		reader:       reader,
		localEP:      localEP,
		ctrlCipher:   c,
		vcMap:        newVCMap(),
		acceptor:     acceptor,
		listenerOpts: listenerOpts,
		principal:    principal,
		blessings:    blessings,
		authResult:   authr,
		nextVCI:      initialVCI,
		outgoing:     outgoing,
		expressQ:     expressQ,
		flowQ:        flowQ,
		flowCounters: message.NewCounters(),
		stopQ:        stopQ,
		versions:     versions,
		onClose:      onClose,
		stats:        Stats{SendMsgCounter: make(map[reflect.Type]uint64), RecvMsgCounter: make(map[reflect.Type]uint64)},
	}
	if startTimeout > 0 {
		vif.startTimer = newTimer(startTimeout, vif.Close)
	}
	vif.idleTimerMap = newIdleTimerMap(func(vci id.VC) {
		vc, _, _ := vif.vcMap.Find(vci)
		if vc != nil {
			vif.closeVCAndSendMsg(vc, false, verror.New(errIdleTimeout, nil))
		}
	})
	vif.loopWG.Add(2)
	go vif.readLoop()
	go vif.writeLoop()
	return vif, nil
}

// Dial creates a new VC to the provided remote identity, authenticating the VC
// with the provided local identity.
func (vif *VIF) Dial(ctx *context.T, remoteEP naming.Endpoint, opts ...stream.VCOpt) (stream.VC, error) {
	var idleTimeout time.Duration
	for _, o := range opts {
		switch v := o.(type) {
		case vc.IdleTimeout:
			idleTimeout = v.Duration
		}
	}
	principal := stream.GetPrincipalVCOpts(ctx, opts...)
	vc, err := vif.newVC(ctx, vif.allocVCI(), vif.localEP, remoteEP, idleTimeout, 0, true)
	if err != nil {
		return nil, err
	}
	counters := message.NewCounters()
	counters.Add(vc.VCI(), sharedFlowID, defaultBytesBufferedPerFlow)

	usePreauth := vif.useVIFAuthForVC(vif.versions.Max, vif.localEP, remoteEP, dialedVIF) &&
		principal != nil &&
		reflect.DeepEqual(principal.PublicKey(), vif.principal.PublicKey())
	switch {
	case usePreauth:
		preauth := vif.authResult
		params := security.CallParams{
			LocalPrincipal:   principal,
			LocalEndpoint:    vif.localEP,
			RemoteEndpoint:   preauth.RemoteEndpoint,
			LocalBlessings:   preauth.LocalBlessings,
			RemoteBlessings:  preauth.RemoteBlessings,
			RemoteDischarges: preauth.RemoteDischarges,
		}
		sendSetupVC := func(pubKey *crypto.BoxKey, sigPreauth []byte) error {
			err := vif.sendOnExpressQ(&message.SetupVC{
				VCI:            vc.VCI(),
				RemoteEndpoint: remoteEP,
				LocalEndpoint:  vif.localEP,
				Counters:       counters,
				Setup: message.Setup{
					Versions: iversion.Range{Min: preauth.Version, Max: preauth.Version},
					Options: []message.SetupOption{
						&message.NaclBox{PublicKey: *pubKey},
						&message.UseVIFAuthentication{Signature: sigPreauth},
					},
				},
			})
			if err != nil {
				return verror.New(stream.ErrNetwork, nil, verror.New(errSendOnExpressQFailed, nil, err))
			}
			return nil
		}
		err = vc.HandshakeDialedVCPreAuthenticated(preauth.Version, params, &preauth.SessionKeys.RemotePublic, sendSetupVC, opts...)
	case principal == nil:
		sendSetupVC := func() error {
			err := vif.sendOnExpressQ(&message.SetupVC{
				VCI:            vc.VCI(),
				RemoteEndpoint: remoteEP,
				LocalEndpoint:  vif.localEP,
				Counters:       counters,
				Setup:          message.Setup{Versions: *vif.versions},
			})
			if err != nil {
				return verror.New(stream.ErrNetwork, nil, verror.New(errSendOnExpressQFailed, nil, err))
			}
			return nil
		}
		err = vc.HandshakeDialedVCNoAuthentication(sendSetupVC, opts...)
	default:
		sendSetupVC := func(pubKey *crypto.BoxKey) error {
			err := vif.sendOnExpressQ(&message.SetupVC{
				VCI:            vc.VCI(),
				RemoteEndpoint: remoteEP,
				LocalEndpoint:  vif.localEP,
				Counters:       counters,
				Setup: message.Setup{
					Versions: *vif.versions,
					Options:  []message.SetupOption{&message.NaclBox{PublicKey: *pubKey}},
				},
			})
			if err != nil {
				return verror.New(stream.ErrNetwork, nil, verror.New(errSendOnExpressQFailed, nil, err))
			}
			return nil
		}
		err = vc.HandshakeDialedVCWithAuthentication(principal, sendSetupVC, opts...)
	}
	if err != nil {
		vif.deleteVC(vc.VCI())
		vc.Close(err)
		return nil, err
	}

	vif.muStats.Lock()
	vif.stats.NumDialedVCs++
	if usePreauth {
		vif.stats.NumPreAuthenticated++
	}
	vif.muStats.Unlock()
	return vc, nil
}

func (vif *VIF) acceptVC(ctx *context.T, m *message.SetupVC) error {
	vrange, err := vif.versions.Intersect(&m.Setup.Versions)
	if err != nil {
		ctx.VI(2).Infof("SetupVC message %+v to VIF %s did not present compatible versions: %v", m, vif, err)
		return err
	}
	vif.muListen.Lock()
	closed := vif.acceptor == nil || vif.acceptor.IsClosed()
	lopts := vif.listenerOpts
	vif.muListen.Unlock()
	if closed {
		ctx.VI(2).Infof("Ignoring SetupVC message %+v as VIF %s does not accept VCs", m, vif)
		return errors.New("VCs not accepted")
	}
	var channelTimeout, idleTimeout time.Duration
	for _, o := range lopts {
		switch v := o.(type) {
		case vc.IdleTimeout:
			idleTimeout = v.Duration
		case vc.ChannelTimeout:
			channelTimeout = time.Duration(v)
		}
	}
	vcobj, err := vif.newVC(ctx, m.VCI, m.RemoteEndpoint, m.LocalEndpoint, idleTimeout, channelTimeout, false)
	if err != nil {
		return err
	}
	vif.distributeCounters(m.Counters)

	var remotePK *crypto.BoxKey
	if box := m.Setup.NaclBox(); box != nil {
		remotePK = &box.PublicKey
	}
	sigPreauth := m.Setup.UseVIFAuthentication()
	var hrCH <-chan vc.HandshakeResult
	switch {
	case len(sigPreauth) > 0:
		if !vif.useVIFAuthForVC(vrange.Max, m.RemoteEndpoint, m.LocalEndpoint, acceptedVIF) {
			ctx.VI(2).Infof("Ignoring SetupVC message %+v as VIF %s does not allow re-using VIF authentication for this VC", m, vif)
			return errors.New("VCs not accepted: cannot re-use VIF authentication for this VC")
		}
		preauth := vif.authResult
		params := security.CallParams{
			LocalPrincipal:  vif.principal,
			LocalBlessings:  vif.blessings,
			RemoteBlessings: preauth.RemoteBlessings,
			LocalDischarges: preauth.LocalDischarges,
		}
		hrCH = vcobj.HandshakeAcceptedVCPreAuthenticated(preauth.Version, params, sigPreauth, &preauth.SessionKeys.LocalPublic, &preauth.SessionKeys.LocalPrivate, remotePK, lopts...)
	case vif.principal == nil:
		sendSetupVC := func() error {
			err = vif.sendOnExpressQ(&message.SetupVC{
				VCI:            m.VCI,
				Setup:          message.Setup{Versions: *vrange},
				RemoteEndpoint: m.LocalEndpoint,
				LocalEndpoint:  vif.localEP,
			})
			return err
		}
		hrCH = vcobj.HandshakeAcceptedVCNoAuthentication(vrange.Max, sendSetupVC, lopts...)
	default:
		exchanger := func(pubKey *crypto.BoxKey) (*crypto.BoxKey, error) {
			err = vif.sendOnExpressQ(&message.SetupVC{
				VCI: m.VCI,
				Setup: message.Setup{
					// Note that servers send clients not their actual supported versions,
					// but the intersected range of the server and client ranges. This
					// is important because proxies may have adjusted the version ranges
					// along the way, and we should negotiate a version that is compatible
					// with all intermediate hops.
					Versions: *vrange,
					Options:  []message.SetupOption{&message.NaclBox{PublicKey: *pubKey}},
				},
				RemoteEndpoint: m.LocalEndpoint,
				LocalEndpoint:  vif.localEP,
				// TODO(mattr): Consider adding counters. See associated comment in
				// vc.initHandshakeAcceptedVC for more details. Note that we need to send
				// AddReceiveBuffers message when reusing VIF authentication since servers
				// doesn't send a reply for SetupVC.
			})
			return remotePK, err
		}
		hrCH = vcobj.HandshakeAcceptedVCWithAuthentication(vrange.Max, vif.principal, vif.blessings, exchanger, lopts...)
	}
	go vif.acceptFlowsLoop(vcobj, hrCH)

	vif.muStats.Lock()
	vif.stats.NumAcceptedVCs++
	if len(sigPreauth) > 0 {
		vif.stats.NumPreAuthenticated++
	}
	vif.muStats.Unlock()
	return nil
}

func (vif *VIF) useVIFAuthForVC(ver version.RPCVersion, localEP, remoteEP naming.Endpoint, side vifSide) bool {
	dialed := side == dialedVIF
	if vif.authResult == nil || vif.authResult.Dialed != dialed || vif.authResult.Version != ver {
		return false
	}
	// We allow to use the VIF authentication when the routing ID is null, since it
	// means that this VIF is connected to the peer directly with a hostname and port.
	if dialed {
		return naming.Compare(vif.authResult.RemoteEndpoint.RoutingID(), remoteEP.RoutingID()) ||
			naming.Compare(remoteEP.RoutingID(), naming.NullRoutingID)
	}
	return naming.Compare(vif.authResult.RemoteEndpoint.RoutingID(), remoteEP.RoutingID()) &&
		(naming.Compare(vif.localEP.RoutingID(), localEP.RoutingID()) || naming.Compare(localEP.RoutingID(), naming.NullRoutingID))
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

	vif.ctx.VI(1).Infof("Closing VIF %s", vif)
	// Stop accepting new VCs.
	vif.StopAccepting()
	// Close local datastructures for all existing VCs.
	vcs := vif.vcMap.Freeze()
	// Stop the idle timers.
	vif.idleTimerMap.Stop()
	for _, vc := range vcs {
		vc.VC.Close(verror.New(stream.ErrNetwork, nil, verror.New(errVIFIsBeingClosed, nil)))
	}
	// Wait for the vcWriteLoops to exit (after draining queued up messages).
	vif.stopQ.Close()
	vif.wpending.Wait()
	// Close the underlying network connection.
	// No need to send individual messages to close all pending VCs since
	// the remote end should know to close all VCs when the VIF's
	// connection breaks.
	if err := vif.conn.Close(); err != nil {
		vif.ctx.VI(1).Infof("net.Conn.Close failed on VIF %s: %v", vif, err)
	}

	// Notify that the VIF has been closed.
	if vif.onClose != nil {
		go vif.onClose(vif)
	}

	vif.loopWG.Wait()
}

// StartAccepting begins accepting Flows (and VCs) initiated by the remote end
// of a VIF. opts is used to setup the listener on newly established VCs.
func (vif *VIF) StartAccepting(opts ...stream.ListenerOpt) error {
	vif.muListen.Lock()
	defer vif.muListen.Unlock()
	if vif.acceptor != nil {
		return verror.New(stream.ErrNetwork, nil, verror.New(errVIFIsBeingClosed, nil, vif))
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
		return ConnectorAndFlow{}, verror.New(stream.ErrNetwork, nil, verror.New(errVCsNotAcceptedOnVIF, nil, vif))
	}
	item, err := acceptor.Get(nil)
	if err != nil {
		return ConnectorAndFlow{}, verror.New(stream.ErrNetwork, nil, verror.New(errAcceptFailed, nil, err))
	}
	return item.(ConnectorAndFlow), nil
}

func (vif *VIF) String() string {
	l := vif.conn.LocalAddr()
	r := vif.conn.RemoteAddr()
	return fmt.Sprintf("(%s, %s) <-> (%s, %s)", l.Network(), l, r.Network(), r)
}

func (vif *VIF) readLoop() {
	defer vif.Close()
	defer vif.loopWG.Done()
	defer vif.stopVCDispatchLoops()
	for {
		// vif.ctrlCipher is guarded by vif.writeMu.  However, the only mutation
		// to it is in handleMessage, which runs in the same goroutine, so a
		// lock is not required here.
		msg, err := message.ReadFrom(vif.reader, vif.ctrlCipher)
		if err != nil {
			vif.ctx.VI(1).Infof("Exiting readLoop of VIF %s because of read error: %v", vif, err)
			return
		}
		vif.ctx.VI(3).Infof("Received %T = [%v] on VIF %s", msg, msg, vif)
		if err := vif.handleMessage(msg); err != nil {
			vif.ctx.VI(1).Infof("Exiting readLoop of VIF %s because of message error: %v", vif, err)
			return
		}
	}
}

// handleMessage handles a single incoming message.  Any error returned is
// fatal, causing the VIF to close.
func (vif *VIF) handleMessage(msg message.T) error {
	mtype := reflect.TypeOf(msg)
	vif.muStats.Lock()
	vif.stats.RecvMsgCounter[mtype]++
	vif.muStats.Unlock()

	switch m := msg.(type) {

	case *message.Data:
		_, rq, _ := vif.vcMap.Find(m.VCI)
		if rq == nil {
			vif.ctx.VI(2).Infof("Ignoring message of %d bytes for unrecognized VCI %d on VIF %s", m.Payload.Size(), m.VCI, vif)
			m.Release()
			return nil
		}
		if err := rq.Put(m, nil); err != nil {
			vif.ctx.VI(2).Infof("Failed to put message(%v) on VC queue on VIF %v: %v", m, vif, err)
			m.Release()
		}

	case *message.AddReceiveBuffers:
		vif.distributeCounters(m.Counters)

	case *message.OpenFlow:
		if vc, _, _ := vif.vcMap.Find(m.VCI); vc != nil {
			if err := vc.AcceptFlow(m.Flow); err != nil {
				vif.ctx.VI(3).Infof("OpenFlow %+v on VIF %v failed:%v", m, vif, err)
				cm := &message.Data{VCI: m.VCI, Flow: m.Flow}
				cm.SetClose()
				vif.sendOnExpressQ(cm)
				return nil
			}
			vc.ReleaseCounters(m.Flow, m.InitialCounters)
			return nil
		}
		vif.ctx.VI(2).Infof("Ignoring OpenFlow(%+v) for unrecognized VCI on VIF %s", m, m, vif)

	case *message.SetupVC:
		// If we dialed this VC, then this is a response and we should finish
		// the vc handshake. Otherwise, this message is opening a new VC.
		if vif.dialedVCI(m.VCI) {
			vif.distributeCounters(m.Counters)
			vc, _, _ := vif.vcMap.Find(m.VCI)
			if vc == nil {
				vif.ctx.VI(2).Infof("Ignoring SetupVC message %+v for unknown dialed VC", m)
				return nil
			}
			vrange, err := vif.versions.Intersect(&m.Setup.Versions)
			if err != nil {
				vif.closeVCAndSendMsg(vc, false, err)
				return nil
			}
			var remotePK *crypto.BoxKey
			if box := m.Setup.NaclBox(); box != nil {
				remotePK = &box.PublicKey
			}
			if err := vc.FinishHandshakeDialedVC(vrange.Max, remotePK); err != nil {
				vif.closeVCAndSendMsg(vc, false, err)
			}
			return nil
		}
		// This is an accepted VC.
		if err := vif.acceptVC(vif.ctx, m); err != nil {
			vif.sendOnExpressQ(&message.CloseVC{VCI: m.VCI, Error: err.Error()})
		}
		return nil

	case *message.CloseVC:
		if vc, _, _ := vif.vcMap.Find(m.VCI); vc != nil {
			vif.deleteVC(vc.VCI())
			vif.ctx.VI(2).Infof("CloseVC(%+v) on VIF %s", m, vif)
			// TODO(cnicolaou): it would be nice to have a method on VC
			// to indicate a 'remote close' rather than a 'local one'. This helps
			// with error reporting since we expect reads/writes to occur
			// after a remote close, but not after a local close.
			vc.Close(verror.New(stream.ErrNetwork, nil, verror.New(errRemoteEndClosedVC, nil, m.Error)))
			return nil
		}
		vif.ctx.VI(2).Infof("Ignoring CloseVC(%+v) for unrecognized VCI on VIF %s", m, vif)

	case *message.HealthCheckRequest:
		vif.sendOnExpressQ(&message.HealthCheckResponse{VCI: m.VCI})

	case *message.HealthCheckResponse:
		if vc, _, _ := vif.vcMap.Find(m.VCI); vc != nil {
			vc.HandleHealthCheckResponse()
		}

	case *message.Setup:
		vif.ctx.Infof("Ignoring redundant Setup message %T on VIF %s", m, vif)

	default:
		vif.ctx.Infof("Ignoring unrecognized message %T on VIF %s", m, vif)
	}
	return nil
}

func (vif *VIF) vcDispatchLoop(ctx *context.T, vc *vc.VC, messages *pcqueue.T) {
	defer ctx.VI(2).Infof("Exiting vcDispatchLoop(%v) on VIF %v", vc, vif)
	defer vif.rpending.Done()
	for {
		qm, err := messages.Get(nil)
		if err != nil {
			return
		}
		m := qm.(*message.Data)
		if err := vc.DispatchPayload(m.Flow, m.Payload); err != nil {
			ctx.VI(2).Infof("Ignoring data message %v for on VIF %s: %v", m, vif, err)
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

func clientVCClosed(err error) bool {
	// If we've encountered a networking error, then all likelihood the
	// connection to the client is closed.
	return verror.ErrorID(err) == stream.ErrNetwork.ID
}

func (vif *VIF) acceptFlowsLoop(vc *vc.VC, c <-chan vc.HandshakeResult) {
	hr := <-c
	if hr.Error != nil {
		vif.closeVCAndSendMsg(vc, clientVCClosed(hr.Error), hr.Error)
		return
	}

	vif.muListen.Lock()
	acceptor := vif.acceptor
	vif.muListen.Unlock()
	if acceptor == nil {
		vif.closeVCAndSendMsg(vc, false, verror.New(errFlowsNoLongerAccepted, nil))
		return
	}

	// Notify any listeners that a new VC has been established
	if err := acceptor.Put(ConnectorAndFlow{vc, nil}); err != nil {
		vif.closeVCAndSendMsg(vc, clientVCClosed(err), verror.New(errVCAcceptFailed, nil, err))
		return
	}

	vif.ctx.VI(2).Infof("Running acceptFlowsLoop for VC %v on VIF %v", vc, vif)
	for {
		f, err := hr.Listener.Accept()
		if err != nil {
			vif.ctx.VI(2).Infof("Accept failed on VC %v on VIF %v: %v", vc, vif, err)
			return
		}
		if err := acceptor.Put(ConnectorAndFlow{vc, f}); err != nil {
			vif.ctx.VI(2).Infof("vif.acceptor.Put(%v, %T) on VIF %v failed: %v", vc, f, vif, err)
			f.Close()
			return
		}
	}
}

func (vif *VIF) distributeCounters(counters message.Counters) {
	for cid, bytes := range counters {
		vc, _, _ := vif.vcMap.Find(cid.VCI())
		if vc == nil {
			vif.ctx.VI(2).Infof("Ignoring counters for non-existent VCI %d on VIF %s", cid.VCI(), vif)
			continue
		}
		vc.ReleaseCounters(cid.Flow(), bytes)
	}
}

func (vif *VIF) writeLoop() {
	defer vif.loopWG.Done()
	defer vif.outgoing.Close()
	defer vif.stopVCWriteLoops()
	for {
		writer, bufs, err := vif.outgoing.Get(nil)
		if err != nil {
			vif.ctx.VI(1).Infof("Exiting writeLoop of VIF %s because of bqueue.Get error: %v", vif, err)
			return
		}
		wtype := reflect.TypeOf(writer)
		vif.muStats.Lock()
		vif.stats.SendMsgCounter[wtype]++
		vif.muStats.Unlock()
		switch writer {
		case vif.expressQ:
			for _, b := range bufs {
				if err := vif.writeSerializedMessage(b.Contents); err != nil {
					vif.ctx.VI(1).Infof("Exiting writeLoop of VIF %s because Control message write failed: %s", vif, err)
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
				vif.ctx.VI(3).Infof("Sending counters %v on VIF %s", msg.Counters, vif)
				if err := vif.writeMessage(msg); err != nil {
					vif.ctx.VI(1).Infof("Exiting writeLoop of VIF %s because AddReceiveBuffers message write failed: %v", vif, err)
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

func (vif *VIF) vcWriteLoop(ctx *context.T, vc *vc.VC, messages *pcqueue.T) {
	defer ctx.VI(2).Infof("Exiting vcWriteLoop(%v) on VIF %v", vc, vif)
	defer vif.wpending.Done()
	for {
		qm, err := messages.Get(nil)
		if err != nil {
			return
		}
		m := qm.(*message.Data)
		m.Payload, err = vc.Encrypt(m.Flow, m.Payload)
		if err != nil {
			ctx.Infof("Encryption failed. Flow:%v VC:%v Error:%v", m.Flow, vc, err)
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
	vif.idleTimerMap.Stop()
	for _, v := range vcs {
		v.WQ.Close()
	}
}

// sendOnExpressQ adds 'msg' to the expressQ (highest priority queue) of messages to write on the wire.
func (vif *VIF) sendOnExpressQ(msg message.T) error {
	vif.ctx.VI(2).Infof("sendOnExpressQ(%T = %+v) on VIF %s", msg, msg, vif)
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
		return verror.New(stream.ErrNetwork, nil, verror.New(errWriteFailed, nil, n, err, len(msg)))
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
		vif.ctx.VI(2).Infof("VCI %d on VIF %s was shutdown, dropping %d messages that were pending a write", vci, vif, len(bufs))
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

func (vif *VIF) dialedVCI(VCI id.VC) bool {
	vif.muNextVCI.Lock()
	dialed := vif.nextVCI%2 == VCI%2
	vif.muNextVCI.Unlock()
	return dialed
}

func (vif *VIF) allocVCI() id.VC {
	vif.muNextVCI.Lock()
	ret := vif.nextVCI
	vif.nextVCI += 2
	vif.muNextVCI.Unlock()
	return ret
}

func (vif *VIF) newVC(ctx *context.T, vci id.VC, localEP, remoteEP naming.Endpoint, idleTimeout, channelTimeout time.Duration, side vifSide) (*vc.VC, error) {
	vif.muStartTimer.Lock()
	if vif.startTimer != nil {
		vif.startTimer.Stop()
		vif.startTimer = nil
	}
	vif.muStartTimer.Unlock()
	vc := vc.InternalNew(ctx, vc.Params{
		VCI:            vci,
		Dialed:         side == dialedVIF,
		LocalEP:        localEP,
		RemoteEP:       remoteEP,
		Pool:           vif.pool,
		ReserveBytes:   uint(message.HeaderSizeBytes + vif.ctrlCipher.MACSize()),
		ChannelTimeout: channelTimeout,
		Helper:         vcHelper{vif},
	})
	added, rq, wq := vif.vcMap.Insert(vc)
	if added {
		vif.idleTimerMap.Insert(vc.VCI(), idleTimeout)
	}
	// Start vcWriteLoop
	if added = added && vif.wpending.TryAdd(); added {
		go vif.vcWriteLoop(ctx, vc, wq)
	}
	// Start vcDispatchLoop
	if added = added && vif.rpending.TryAdd(); added {
		go vif.vcDispatchLoop(ctx, vc, rq)
	}
	if !added {
		if rq != nil {
			rq.Close()
		}
		if wq != nil {
			wq.Close()
		}
		vc.Close(verror.New(stream.ErrAborted, nil, verror.New(errShuttingDown, nil, vif)))
		vif.deleteVC(vci)
		return nil, verror.New(stream.ErrAborted, nil, verror.New(errShuttingDown, nil, vif))
	}
	return vc, nil
}

func (vif *VIF) deleteVC(vci id.VC) {
	vif.idleTimerMap.Delete(vci)
	if vif.vcMap.Delete(vci) {
		vif.Close()
	}
}

func (vif *VIF) closeVCAndSendMsg(vc *vc.VC, clientVCClosed bool, errMsg error) {
	vif.ctx.VI(2).Infof("Shutting down VCI %d on VIF %v due to: %v", vc.VCI(), vif, errMsg)
	vif.deleteVC(vc.VCI())
	vc.Close(errMsg)
	if clientVCClosed {
		// No point in sending to the client if the VC is closed, or otherwise broken.
		return
	}
	msg := ""
	if errMsg != nil {
		msg = errMsg.Error()
	}
	if err := vif.sendOnExpressQ(&message.CloseVC{
		VCI:   vc.VCI(),
		Error: msg,
	}); err != nil {
		vif.ctx.VI(2).Infof("sendOnExpressQ(CloseVC{VCI:%d,...}) on VIF %v failed: %v", vc.VCI(), vif, err)
	}
}

// shutdownFlow clears out all the datastructures associated with fid.
func (vif *VIF) shutdownFlow(vc *vc.VC, fid id.Flow) {
	vc.ShutdownFlow(fid)
	vif.flowMu.Lock()
	delete(vif.flowCounters, message.MakeCounterID(vc.VCI(), fid))
	vif.flowMu.Unlock()
	vif.idleTimerMap.DeleteFlow(vc.VCI(), fid)
}

// ShutdownVCs closes all VCs established to the provided remote endpoint.
// Returns the number of VCs that were closed.
func (vif *VIF) ShutdownVCs(remote naming.Endpoint) int {
	vcs := vif.vcMap.List()
	n := 0
	for _, vc := range vcs {
		if naming.Compare(vc.RemoteEndpoint().RoutingID(), remote.RoutingID()) {
			vif.ctx.VI(1).Infof("VCI %d on VIF %s being closed because of ShutdownVCs call", vc.VCI(), vif)
			vif.closeVCAndSendMsg(vc, false, nil)
			n++
		}
	}
	return n
}

// NumVCs returns the number of VCs established over this VIF.
func (vif *VIF) NumVCs() int { return vif.vcMap.Size() }

// Stats returns the current stats of this VIF.
func (vif *VIF) Stats() Stats {
	stats := Stats{SendMsgCounter: make(map[reflect.Type]uint64), RecvMsgCounter: make(map[reflect.Type]uint64)}
	vif.muStats.Lock()
	for k, v := range vif.stats.SendMsgCounter {
		stats.SendMsgCounter[k] = v
	}
	for k, v := range vif.stats.RecvMsgCounter {
		stats.RecvMsgCounter[k] = v
	}
	stats.NumDialedVCs = vif.stats.NumDialedVCs
	stats.NumAcceptedVCs = vif.stats.NumAcceptedVCs
	stats.NumPreAuthenticated = vif.stats.NumPreAuthenticated
	vif.muStats.Unlock()
	return stats
}

// DebugString returns a descriptive state of the VIF.
//
// The returned string is meant for consumptions by humans. The specific format
// should not be relied upon by any automated processing.
func (vif *VIF) DebugString() string {
	vif.muNextVCI.Lock()
	nextVCI := vif.nextVCI
	vif.muNextVCI.Unlock()
	vif.isClosedMu.Lock()
	isClosed := vif.isClosed
	vif.isClosedMu.Unlock()
	vcs := vif.vcMap.List()
	stats := vif.Stats()

	l := make([]string, 0, 2+len(vcs)+len(stats.SendMsgCounter)+len(stats.RecvMsgCounter))
	l = append(l, fmt.Sprintf("VIF:[%s] -- #VCs:%d NextVCI:%d ControlChannelEncryption:%t IsClosed:%t #Dialed:%d #Accepted:%d #PreAuthenticated:%d",
		vif, len(vcs), nextVCI, vif.ctrlCipher != nullCipher, isClosed, stats.NumDialedVCs, stats.NumAcceptedVCs, stats.NumPreAuthenticated))

	l = append(l, "Message Counters:")
	msgStats := make([]string, 0, len(stats.SendMsgCounter)+len(stats.RecvMsgCounter))
	for k, v := range stats.SendMsgCounter {
		msgStats = append(msgStats, fmt.Sprintf(" %-32s %10d", "Send("+k.String()+")", v))
	}
	for k, v := range stats.RecvMsgCounter {
		msgStats = append(msgStats, fmt.Sprintf(" %-32s %10d", "Recv("+k.String()+")", v))
	}
	sort.Strings(msgStats)
	l = append(l, msgStats...)

	for _, vc := range vcs {
		l = append(l, vc.DebugString())
	}
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

func (h vcHelper) SendHealthCheck(vci id.VC) {
	h.vif.sendOnExpressQ(&message.HealthCheckRequest{VCI: vci})
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

func (h vcHelper) NewWriter(vci id.VC, fid id.Flow, priority bqueue.Priority) (bqueue.Writer, error) {
	h.vif.idleTimerMap.InsertFlow(vci, fid)
	return h.vif.outgoing.NewWriter(packIDs(vci, fid), flowPriority+priority, defaultBytesBufferedPerFlow)
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

// localEndpoint creates a naming.Endpoint from the provided parameters.
//
// It intentionally does not include any blessings (present in endpoints in the
// v5 format). At this point it is not clear whether the endpoint is being
// created for a "client" or a "server". If the endpoint is used for clients
// (i.e., for those sending an OpenVC message for example), then we do NOT want
// to include the blessings in the endpoint to ensure client privacy.
//
// Servers should be happy to let anyone with access to their endpoint string
// know their blessings, because they are willing to share those with anyone
// that connects to them.
//
// The addition of the endpoints is left as an excercise to higher layers of
// the stack, where the desire to share or hide blessings from the endpoint is
// clearer.
func localEndpoint(conn net.Conn, rid naming.RoutingID, versions *iversion.Range) naming.Endpoint {
	localAddr := conn.LocalAddr()
	ep := &inaming.Endpoint{
		Protocol: localAddr.Network(),
		Address:  localAddr.String(),
		RID:      rid,
	}
	return ep
}
