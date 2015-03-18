package vc

// Logging guidelines:
// Verbosity level 1 is for per-VC messages.
// Verbosity level 2 is for per-Flow messages.

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"v.io/x/ref/profiles/internal/lib/bqueue"
	"v.io/x/ref/profiles/internal/lib/iobuf"
	vsync "v.io/x/ref/profiles/internal/lib/sync"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
	"v.io/x/ref/profiles/internal/rpc/stream/id"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/profiles/internal/rpc/stream"
)

var (
	errAlreadyListening = errors.New("Listen has already been called")
	errDuplicateFlow    = errors.New("duplicate OpenFlow message")
	errUnrecognizedFlow = errors.New("unrecognized flow")
)

// DischargeExpiryBuffer specifies how much before discharge expiration we should
// refresh discharges.
// Discharges will be refreshed DischargeExpiryBuffer before they expire.
type DischargeExpiryBuffer time.Duration

func (DischargeExpiryBuffer) RPCStreamListenerOpt() {}
func (DischargeExpiryBuffer) RPCServerOpt()         {}

const DefaultServerDischargeExpiryBuffer = 20 * time.Second

// VC implements the stream.VC interface and exports additional methods to
// manage Flows.
//
// stream.Flow objects created by this stream.VC implementation use a buffer
// queue (v.io/x/ref/profiles/internal/lib/bqueue) to provide flow control on Write
// operations.
type VC struct {
	vci                             id.VC
	localEP, remoteEP               naming.Endpoint
	localPrincipal                  security.Principal
	localBlessings, remoteBlessings security.Blessings
	localDischarges                 map[string]security.Discharge // Discharges shared by the local end of the VC.
	remoteDischarges                map[string]security.Discharge // Discharges shared by the remote end of the VC.

	pool           *iobuf.Pool
	reserveBytes   uint
	sharedCounters *vsync.Semaphore

	mu                  sync.Mutex
	flowMap             map[id.Flow]*flow // nil iff the VC is closed.
	acceptHandshakeDone chan struct{}     // non-nil when HandshakeAcceptedVC begins the handshake, closed when handshake completes.
	handshakeFID        id.Flow           // flow used for a TLS handshake to setup encryption.
	authFID             id.Flow           // flow used by the authentication protocol.
	nextConnectFID      id.Flow
	listener            *listener // non-nil iff Listen has been called and the VC has not been closed.
	crypter             crypto.Crypter
	closeReason         string // reason why the VC was closed
	closeCh             chan struct{}
	closed              bool

	helper    Helper
	version   version.RPCVersion
	dataCache *dataCache // dataCache contains information that can shared between Flows from this VC.
}

// ServerAuthorizer encapsulates the policy used to authorize servers during VC
// establishment.
//
// A client will first authorize a server before revealing any of its credentials
// (public key, blessings etc.) to the server. Thus, if the authorization policy
// calls for the server to be rejected, then the client will not have revealed
// any of its credentials to the server.
//
// ServerAuthorizer in turn uses an authorization policy (security.Authorizer),
// with the context matching the context of the RPC that caused the initiation
// of the VC.
type ServerAuthorizer struct {
	Suffix, Method string
	Policy         security.Authorizer
}

func (a *ServerAuthorizer) RPCStreamVCOpt() {}
func (a *ServerAuthorizer) Authorize(params security.CallParams) error {
	params.Suffix = a.Suffix
	params.Method = a.Method
	ctx, cancel := context.RootContext()
	defer cancel()
	return a.Policy.Authorize(security.SetCall(ctx, security.NewCall(&params)))
}

var _ stream.VC = (*VC)(nil)

// Helper is the interface for functionality required by the stream.VC
// implementation in this package.
type Helper interface {
	// NotifyOfNewFlow notifies the remote end of a VC that the caller intends to
	// establish a new flow to it and that the caller is ready to receive bytes
	// data from the remote end.
	NotifyOfNewFlow(vci id.VC, fid id.Flow, bytes uint)

	// AddReceiveBuffers notifies the remote end of a VC that it is read to receive
	// bytes more data on the flow identified by fid over the VC identified by vci.
	//
	// Unlike NotifyOfNewFlow, this call does not let the remote end know of the
	// intent to establish a new flow.
	AddReceiveBuffers(vci id.VC, fid id.Flow, bytes uint)

	// NewWriter creates a buffer queue for Write operations on the
	// stream.Flow implementation.
	NewWriter(vci id.VC, fid id.Flow) (bqueue.Writer, error)
}

// Params encapsulates the set of parameters needed to create a new VC.
type Params struct {
	VCI          id.VC           // Identifier of the VC
	Dialed       bool            // True if the VC was initiated by the local process.
	LocalEP      naming.Endpoint // Endpoint of the local end of the VC.
	RemoteEP     naming.Endpoint // Endpoint of the remote end of the VC.
	Pool         *iobuf.Pool     // Byte pool used for read and write buffer allocations.
	ReserveBytes uint            // Number of padding bytes to reserve for headers.
	Helper       Helper
	Version      version.RPCVersion
}

// DischargeClient is an interface for obtaining discharges for a set of third-party
// caveats.
//
// TODO(ataly, ashankar): What should be the impetus for obtaining the discharges?
type DischargeClient interface {
	PrepareDischarges(ctx *context.T, forcaveats []security.Caveat, impetus security.DischargeImpetus) []security.Discharge
	// Invalidate marks the provided discharges as invalid, and therefore unfit
	// for being returned by a subsequent PrepareDischarges call.
	Invalidate(discharges ...security.Discharge)
	RPCStreamListenerOpt()
}

// DialContext establishes the context under which a VC Dial was initiated.
type DialContext struct{ *context.T }

func (DialContext) RPCStreamVCOpt()       {}
func (DialContext) RPCStreamListenerOpt() {}

// InternalNew creates a new VC, which implements the stream.VC interface.
//
// As the name suggests, this method is intended for use only within packages
// placed inside v.io/x/ref/profiles/internal. Code outside the
// v.io/x/ref/profiles/internal/* packages should never call this method.
func InternalNew(p Params) *VC {
	fidOffset := 1
	if p.Dialed {
		fidOffset = 0
	}
	return &VC{
		vci:            p.VCI,
		localEP:        p.LocalEP,
		remoteEP:       p.RemoteEP,
		pool:           p.Pool,
		reserveBytes:   p.ReserveBytes,
		sharedCounters: vsync.NewSemaphore(),
		flowMap:        make(map[id.Flow]*flow),
		// Reserve flow IDs 0 thru NumReservedFlows for
		// possible future use.
		// Furthermore, flows created by Connect have an even
		// id if the VC was initiated by the local process,
		// and have an odd id if the VC was initiated by the
		// remote process.
		nextConnectFID: id.Flow(NumReservedFlows + fidOffset),
		crypter:        crypto.NewNullCrypter(),
		helper:         p.Helper,
		version:        p.Version,
		dataCache:      newDataCache(),
		closeCh:        make(chan struct{}),
	}
}

// Connect implements the stream.Connector.Connect method.
func (vc *VC) Connect(opts ...stream.FlowOpt) (stream.Flow, error) {
	return vc.connectFID(vc.allocFID(), opts...)
}

func (vc *VC) connectFID(fid id.Flow, opts ...stream.FlowOpt) (stream.Flow, error) {
	writer, err := vc.newWriter(fid)
	if err != nil {
		return nil, fmt.Errorf("failed to create writer for Flow: %v", err)
	}
	f := &flow{
		authN:          vc,
		reader:         newReader(readHandlerImpl{vc, fid}),
		writer:         writer,
		localEndpoint:  vc.localEP,
		remoteEndpoint: vc.remoteEP,
		dataCache:      vc.dataCache,
	}
	vc.mu.Lock()
	if vc.flowMap != nil {
		vc.flowMap[fid] = f
	} else {
		err = fmt.Errorf("Connect on closed VC(%q)", vc.closeReason)
	}
	vc.mu.Unlock()
	if err != nil {
		f.Shutdown()
		return nil, err
	}
	// New flow created, inform remote end that data can be received on it.
	vc.helper.NotifyOfNewFlow(vc.vci, fid, DefaultBytesBufferedPerFlow)
	return f, nil
}

// Listen implements the stream.VC.Listen method.
func (vc *VC) Listen() (stream.Listener, error) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	if vc.listener != nil {
		return nil, errAlreadyListening
	}
	vc.listener = newListener()
	return vc.listener, nil
}

// RemoteAddr returns the remote endpoint for this VC.
func (vc *VC) RemoteAddr() naming.Endpoint {
	return vc.remoteEP
}

// LocalAddr returns the local endpoint for this VC.
func (vc *VC) LocalAddr() naming.Endpoint {
	return vc.localEP
}

// DispatchPayload makes payload.Contents available to Read operations on the
// Flow identified by fid.
//
// Assumes ownership of payload, i.e., payload should not be used by the caller
// after this method returns (irrespective of the return value).
func (vc *VC) DispatchPayload(fid id.Flow, payload *iobuf.Slice) error {
	if payload.Size() == 0 {
		payload.Release()
		return nil
	}
	vc.mu.Lock()
	if vc.flowMap == nil {
		vc.mu.Unlock()
		payload.Release()
		return fmt.Errorf("ignoring message for Flow %d on closed VC %d", fid, vc.VCI())
	}
	// TLS decryption is stateful, so even if the message will be discarded
	// because of other checks further down in this method, go through with
	// the decryption.
	if fid != vc.handshakeFID && fid != vc.authFID {
		vc.waitForHandshakeLocked()
		var err error
		if payload, err = vc.crypter.Decrypt(payload); err != nil {
			vc.mu.Unlock()
			return fmt.Errorf("failed to decrypt payload: %v", err)
		}
	}
	if payload.Size() == 0 {
		vc.mu.Unlock()
		payload.Release()
		return nil
	}
	f := vc.flowMap[fid]
	if f == nil {
		vc.mu.Unlock()
		payload.Release()
		return errUnrecognizedFlow
	}
	vc.mu.Unlock()
	if err := f.reader.Put(payload); err != nil {
		payload.Release()
		return err
	}
	return nil
}

// AcceptFlow enqueues a new Flow for acceptance by the listener on the VC.
// Returns an error if the VC is not accepting flows initiated by the remote
// end.
func (vc *VC) AcceptFlow(fid id.Flow) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	if vc.listener == nil {
		return fmt.Errorf("no active listener on VC %d", vc.vci)
	}
	writer, err := vc.newWriter(fid)
	if err != nil {
		return fmt.Errorf("failed to create writer for new flow(%d): %v", fid, err)
	}
	f := &flow{
		authN:          vc,
		reader:         newReader(readHandlerImpl{vc, fid}),
		writer:         writer,
		localEndpoint:  vc.localEP,
		remoteEndpoint: vc.remoteEP,
		dataCache:      vc.dataCache,
	}
	if err = vc.listener.Enqueue(f); err != nil {
		f.Shutdown()
		return fmt.Errorf("failed to enqueue flow at listener: %v", err)
	}
	if _, exists := vc.flowMap[fid]; exists {
		return errDuplicateFlow
	}
	vc.flowMap[fid] = f
	// New flow accepted, notify remote end that it can send over data.
	// Do it in a goroutine in case the implementation of AddReceiveBuffers
	// ends up attempting to lock vc.mu
	go vc.helper.AddReceiveBuffers(vc.vci, fid, DefaultBytesBufferedPerFlow)
	vlog.VI(2).Infof("Added flow %d@%d to listener", fid, vc.vci)
	return nil
}

// ShutdownFlow closes the Flow identified by fid and discards any pending
// writes.
func (vc *VC) ShutdownFlow(fid id.Flow) {
	vc.mu.Lock()
	f := vc.flowMap[fid]
	delete(vc.flowMap, fid)
	vc.mu.Unlock()
	if f != nil {
		f.Shutdown()
	}
}

// ReleaseCounters informs the Flow (identified by fid) that the remote end is
// ready to receive up to 'bytes' more bytes of data.
func (vc *VC) ReleaseCounters(fid id.Flow, bytes uint32) {
	if fid == SharedFlowID {
		vc.sharedCounters.IncN(uint(bytes))
		return
	}
	var f *flow
	vc.mu.Lock()
	if vc.flowMap != nil {
		f = vc.flowMap[fid]
	}
	vc.mu.Unlock()
	if f == nil {
		vlog.VI(2).Infof("Ignoring ReleaseCounters(%d, %d) on VCI %d as the flow does not exist", fid, bytes, vc.vci)
		return
	}
	f.Release(int(bytes))
}

// Close closes the VC and all flows on it, allowing any pending writes in the
// flow to drain.
func (vc *VC) Close(reason string) error {
	vlog.VI(1).Infof("Closing VC %v. Reason:%q", vc, reason)
	vc.mu.Lock()
	flows := vc.flowMap
	if !vc.closed {
		close(vc.closeCh)
		vc.closed = true
	}
	vc.flowMap = nil
	if vc.listener != nil {
		vc.listener.Close()
		vc.listener = nil
	}
	vc.closeReason = reason
	vc.mu.Unlock()

	vc.sharedCounters.Close()
	for fid, flow := range flows {
		vlog.VI(2).Infof("Closing flow %d on VC %v as VC is being closed(%q)", fid, vc, reason)
		flow.Close()
	}
	return nil
}

// err prefers vc.closeReason over err.
func (vc *VC) err(err error) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	if vc.closeReason != "" {
		return errors.New(vc.closeReason)
	}
	return err
}

// HandshakeDialedVC completes initialization of the VC (setting up encryption,
// authentication etc.) under the assumption that the VC was initiated by the
// local process (i.e., the local process "Dial"ed to create the VC).
func (vc *VC) HandshakeDialedVC(principal security.Principal, opts ...stream.VCOpt) error {
	var (
		tlsSessionCache crypto.TLSClientSessionCache
		securityLevel   options.VCSecurityLevel
		auth            *ServerAuthorizer
	)
	for _, o := range opts {
		switch v := o.(type) {
		case options.VCSecurityLevel:
			securityLevel = v
		case crypto.TLSClientSessionCache:
			tlsSessionCache = v
		case *ServerAuthorizer:
			auth = v
		}
	}
	switch securityLevel {
	case options.VCSecurityConfidential:
		if principal == nil {
			return fmt.Errorf("principal required for VCSecurityConfidential")
		}
	case options.VCSecurityNone:
		return nil
	default:
		return fmt.Errorf("unrecognized VC security level: %v", securityLevel)
	}

	// Establish TLS
	handshakeFID := vc.allocFID()
	handshakeConn, err := vc.connectFID(handshakeFID)
	if err != nil {
		return vc.err(fmt.Errorf("failed to create a Flow for setting up TLS: %v", err))
	}
	crypter, err := crypto.NewTLSClient(handshakeConn, handshakeConn.LocalEndpoint(), handshakeConn.RemoteEndpoint(), tlsSessionCache, vc.pool)
	if err != nil {
		return vc.err(fmt.Errorf("failed to setup TLS: %v", err))
	}

	// Authenticate (exchange identities)
	// Unfortunately, handshakeConn cannot be used for the authentication protocol.
	// This is because the Crypter implementation uses crypto/tls.Conn,
	// which can consume data beyond the handshake message boundaries (call
	// to readFromUntil in
	// https://code.google.com/p/go/source/browse/src/pkg/crypto/tls/conn.go?spec=svn654b2703fcc466a29692068ab56efedd09fb3d05&r=654b2703fcc466a29692068ab56efedd09fb3d05#539).
	// This is not a problem when tls.Conn is used as intended (to wrap over a stream), but
	// becomes a problem when shoehorning a block encrypter (Crypter interface) over this
	// stream API.
	authFID := vc.allocFID()
	authConn, err := vc.connectFID(authFID)
	if err != nil {
		return vc.err(fmt.Errorf("failed to create a Flow for authentication: %v", err))
	}
	params := security.CallParams{
		LocalPrincipal: principal,
		LocalEndpoint:  vc.localEP,
		RemoteEndpoint: vc.remoteEP,
	}
	rBlessings, lBlessings, rDischarges, err := AuthenticateAsClient(authConn, crypter, params, auth, vc.version)
	if err != nil || len(rBlessings.ThirdPartyCaveats()) == 0 {
		authConn.Close()
		if err != nil {
			return vc.err(fmt.Errorf("authentication failed: %v", err))
		}
	} else {
		go vc.recvDischargesLoop(authConn)
	}

	vc.mu.Lock()
	vc.handshakeFID = handshakeFID
	vc.authFID = authFID
	vc.crypter = crypter
	vc.localPrincipal = principal
	vc.localBlessings = lBlessings
	vc.remoteBlessings = rBlessings
	vc.remoteDischarges = rDischarges
	vc.mu.Unlock()

	vlog.VI(1).Infof("Client VC %v authenticated. RemoteBlessings:%v, LocalBlessings:%v", vc, rBlessings, lBlessings)
	return nil
}

// HandshakeResult is sent by HandshakeAcceptedVC over the channel returned by it.
type HandshakeResult struct {
	Listener stream.Listener // Listener for accepting new Flows on the VC.
	Error    error           // Error, if any, during the handshake.
}

// HandshakeAcceptedVC completes initialization of the VC (setting up
// encryption, authentication etc.) under the assumption that the VC was
// initiated by a remote process (and the local process wishes to "accept" it).
//
// Since the handshaking process might involve several round trips, a bulk of the work
// is done asynchronously and the result of the handshake is written to the
// channel returned by this method.
//
// 'principal' is the principal used by the server used during authentication.
// If principal is nil, then the VC expects to be used for unauthenticated, unencrypted communication.
// 'lBlessings' is presented to the client during authentication.
func (vc *VC) HandshakeAcceptedVC(principal security.Principal, lBlessings security.Blessings, opts ...stream.ListenerOpt) <-chan HandshakeResult {
	result := make(chan HandshakeResult, 1)
	finish := func(ln stream.Listener, err error) chan HandshakeResult {
		result <- HandshakeResult{ln, err}
		return result
	}
	var (
		securityLevel   options.VCSecurityLevel
		dischargeClient DischargeClient

		dischargeExpiryBuffer = DefaultServerDischargeExpiryBuffer
	)
	for _, o := range opts {
		switch v := o.(type) {
		case DischargeClient:
			dischargeClient = v
		case options.VCSecurityLevel:
			securityLevel = v
		case DischargeExpiryBuffer:
			dischargeExpiryBuffer = time.Duration(v)
		}
	}
	// If the listener was setup asynchronously, there is a race between
	// the listener being setup and the caller of this method trying to
	// dispatch messages, thus it is setup synchronously.
	ln, err := vc.Listen()
	if err != nil {
		return finish(nil, err)
	}
	vc.helper.AddReceiveBuffers(vc.VCI(), SharedFlowID, DefaultBytesBufferedPerFlow)
	switch securityLevel {
	case options.VCSecurityConfidential:
		if principal == nil {
			return finish(nil, fmt.Errorf("principal required for VCSecurityConfidential"))
		}
		if lBlessings.IsZero() {
			return finish(nil, fmt.Errorf("blessings required for VCSecurityConfidential"))
		}
	case options.VCSecurityNone:
		return finish(ln, nil)
	default:
		ln.Close()
		return finish(nil, fmt.Errorf("unrecognized VC security level: %v", securityLevel))
	}
	go func() {
		sendErr := func(err error) {
			ln.Close()
			result <- HandshakeResult{nil, vc.err(err)}
		}
		// TODO(ashankar): There should be a timeout on this Accept
		// call.  Otherwise, a malicious (or incompetent) client can
		// consume server resources by sending many OpenVC messages but
		// not following up with the handshake protocol. Same holds for
		// the identity exchange protocol.
		handshakeConn, err := ln.Accept()
		if err != nil {
			sendErr(fmt.Errorf("TLS handshake Flow not accepted: %v", err))
			return
		}
		vc.mu.Lock()
		vc.acceptHandshakeDone = make(chan struct{})
		vc.handshakeFID = vc.findFlowLocked(handshakeConn)
		vc.mu.Unlock()

		// Establish TLS
		crypter, err := crypto.NewTLSServer(handshakeConn, handshakeConn.LocalEndpoint(), handshakeConn.RemoteEndpoint(), vc.pool)
		if err != nil {
			sendErr(fmt.Errorf("failed to setup TLS: %v", err))
			return
		}

		// Authenticate (exchange identities)
		authConn, err := ln.Accept()
		if err != nil {
			sendErr(fmt.Errorf("Authentication Flow not accepted: %v", err))
			return
		}
		vc.mu.Lock()
		vc.authFID = vc.findFlowLocked(authConn)
		vc.mu.Unlock()

		rBlessings, lDischarges, err := AuthenticateAsServer(authConn, principal, lBlessings, dischargeClient, crypter, vc.version)
		if err != nil {
			authConn.Close()
			sendErr(fmt.Errorf("authentication failed: %v", err))
			return
		}

		vc.mu.Lock()
		vc.crypter = crypter
		vc.localPrincipal = principal
		vc.localBlessings = lBlessings
		vc.remoteBlessings = rBlessings
		vc.localDischarges = lDischarges
		close(vc.acceptHandshakeDone)
		vc.acceptHandshakeDone = nil
		vc.mu.Unlock()
		vlog.VI(1).Infof("Server VC %v authenticated. RemoteBlessings:%v, LocalBlessings:%v", vc, rBlessings, lBlessings)
		result <- HandshakeResult{ln, nil}

		if len(lBlessings.ThirdPartyCaveats()) > 0 {
			go vc.sendDischargesLoop(authConn, dischargeClient, lBlessings.ThirdPartyCaveats(), dischargeExpiryBuffer)
		} else {
			authConn.Close()
		}
	}()
	return result
}

func (vc *VC) sendDischargesLoop(conn io.WriteCloser, dc DischargeClient, tpCavs []security.Caveat, dischargeExpiryBuffer time.Duration) {
	defer conn.Close()
	if dc == nil {
		return
	}
	enc, err := vom.NewEncoder(conn)
	if err != nil {
		vlog.Errorf("failed to create new encoder for discharges on VC %v: %v", vc, err)
		return
	}
	discharges := dc.PrepareDischarges(nil, tpCavs, security.DischargeImpetus{})
	for expiry := minExpiryTime(discharges, tpCavs); !expiry.IsZero(); expiry = minExpiryTime(discharges, tpCavs) {
		select {
		case <-time.After(fetchDuration(expiry, dischargeExpiryBuffer)):
			discharges = dc.PrepareDischarges(nil, tpCavs, security.DischargeImpetus{})
			if err := enc.Encode(discharges); err != nil {
				vlog.Errorf("encoding discharges on VC %v failed: %v", vc, err)
				return
			}
			if len(discharges) == 0 {
				continue
			}
			vc.mu.Lock()
			if vc.localDischarges == nil {
				vc.localDischarges = make(map[string]security.Discharge)
			}
			for _, d := range discharges {
				vc.localDischarges[d.ID()] = d
			}
			vc.mu.Unlock()
		case <-vc.closeCh:
			vlog.VI(3).Infof("closing sendDischargesLoop on VC %v", vc)
			return
		}
	}
}

func fetchDuration(expiry time.Time, buffer time.Duration) time.Duration {
	// Fetch the discharge earlier than the actual expiry to factor in for clock
	// skew and RPC time.
	return expiry.Sub(time.Now().Add(buffer))
}

func minExpiryTime(discharges []security.Discharge, tpCavs []security.Caveat) time.Time {
	var min time.Time
	// If some discharges were not fetched, retry again in a minute.
	if len(discharges) < len(tpCavs) {
		min = time.Now().Add(time.Minute)
	}
	for _, d := range discharges {
		if exp := d.Expiry(); min.IsZero() || (!exp.IsZero() && exp.Before(min)) {
			min = exp
		}
	}
	return min
}

func (vc *VC) recvDischargesLoop(conn io.ReadCloser) {
	defer conn.Close()
	dec, err := vom.NewDecoder(conn)
	if err != nil {
		vlog.Errorf("failed to create new decoder for discharges on %v: %v", vc, err)
		return
	}

	for {
		var discharges []security.Discharge
		if err := dec.Decode(&discharges); err != nil {
			vlog.VI(3).Infof("decoding discharges on %v failed: %v", vc, err)
			return
		}
		if len(discharges) == 0 {
			continue
		}
		vc.mu.Lock()
		if vc.remoteDischarges == nil {
			vc.remoteDischarges = make(map[string]security.Discharge)
		}
		for _, d := range discharges {
			vc.remoteDischarges[d.ID()] = d
		}
		vc.mu.Unlock()
	}
}

// Encrypt uses the VC's encryption scheme to encrypt the provided data payload.
// Always takes ownership of plaintext.
func (vc *VC) Encrypt(fid id.Flow, plaintext *iobuf.Slice) (cipherslice *iobuf.Slice, err error) {
	if plaintext == nil {
		return nil, nil
	}
	vc.mu.Lock()
	if fid == vc.handshakeFID || fid == vc.authFID {
		cipherslice = plaintext
	} else {
		cipherslice, err = vc.crypter.Encrypt(plaintext)
	}
	vc.mu.Unlock()
	return
}

func (vc *VC) allocFID() id.Flow {
	vc.mu.Lock()
	ret := vc.nextConnectFID
	vc.nextConnectFID += 2
	vc.mu.Unlock()
	return ret
}

func (vc *VC) newWriter(fid id.Flow) (*writer, error) {
	bq, err := vc.helper.NewWriter(vc.vci, fid)
	if err != nil {
		return nil, err
	}
	alloc := iobuf.NewAllocator(vc.pool, vc.reserveBytes)
	return newWriter(MaxPayloadSizeBytes, bq, alloc, vc.sharedCounters), nil
}

// findFlowLocked finds the flow id for the provided flow.
// REQUIRES: vc.mu is held
// Returns 0 if there is none.
func (vc *VC) findFlowLocked(flow interface{}) id.Flow {
	const invalidFlowID = 0
	// This operation is rare and early enough (called when there are <= 2
	// flows over the VC) that iteration to the map should be fine.
	for fid, f := range vc.flowMap {
		if f == flow {
			return fid
		}
	}
	return invalidFlowID
}

// VCI returns the identifier of this VC.
func (vc *VC) VCI() id.VC { return vc.vci }

// LocalPrincipal returns the principal that authenticated with the remote end of the VC.
func (vc *VC) LocalPrincipal() security.Principal {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.waitForHandshakeLocked()
	return vc.localPrincipal
}

// LocalBlessings returns the blessings (bound to LocalPrincipal) presented to the
// remote end of the VC during authentication.
func (vc *VC) LocalBlessings() security.Blessings {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.waitForHandshakeLocked()
	return vc.localBlessings
}

// RemoteBlessings returns the blessings presented by the remote end of the VC during
// authentication.
func (vc *VC) RemoteBlessings() security.Blessings {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.waitForHandshakeLocked()
	return vc.remoteBlessings
}

// LocalDischarges returns the discharges presented by the local end of the VC during
// authentication.
func (vc *VC) LocalDischarges() map[string]security.Discharge {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.waitForHandshakeLocked()
	if len(vc.localDischarges) == 0 {
		return nil
	}
	// Return a copy of the map to prevent racy reads.
	return copyDischargeMap(vc.localDischarges)
}

// RemoteDischarges returns the discharges presented by the remote end of the VC during
// authentication.
func (vc *VC) RemoteDischarges() map[string]security.Discharge {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.waitForHandshakeLocked()
	if len(vc.remoteDischarges) == 0 {
		return nil
	}
	// Return a copy of the map to prevent racy reads.
	return copyDischargeMap(vc.remoteDischarges)
}

// waitForHandshakeLocked blocks until an in-progress handshake (encryption
// setup and authentication) completes.
// REQUIRES: vc.mu is held.
func (vc *VC) waitForHandshakeLocked() {
	if hsd := vc.acceptHandshakeDone; hsd != nil {
		vc.mu.Unlock()
		<-hsd
		vc.mu.Lock()
	}
}

func (vc *VC) String() string {
	return fmt.Sprintf("VCI:%d (%v<->%v)", vc.vci, vc.localEP, vc.remoteEP)
}

// DebugString returns a string representation of the state of a VC.
//
// The format of the returned string is meant to be human-friendly and the
// specific format should not be relied upon for automated processing.
func (vc *VC) DebugString() string {
	vc.mu.Lock()
	l := make([]string, 0, len(vc.flowMap)+1)
	l = append(l, fmt.Sprintf("VCI:%d -- Endpoints:(Local:%q Remote:%q) #Flows:%d NextConnectFID:%d",
		vc.vci,
		vc.localEP,
		vc.remoteEP,
		len(vc.flowMap),
		vc.nextConnectFID))
	if vc.crypter == nil {
		l = append(l, "Handshake not completed yet")
	} else {
		l = append(l, "Encryption: "+vc.crypter.String())
		if vc.localPrincipal != nil {
			l = append(l, fmt.Sprintf("LocalPrincipal:%v LocalBlessings:%v RemoteBlessings:%v", vc.localPrincipal.PublicKey(), vc.localBlessings, vc.remoteBlessings))
		}
	}
	for fid, f := range vc.flowMap {
		l = append(l, fmt.Sprintf("  Flow:%3d BytesRead:%7d BytesWritten:%7d", fid, f.BytesRead(), f.BytesWritten()))
	}
	vc.mu.Unlock()
	sort.Strings(l[1:])
	return strings.Join(l, "\n")
}

// readHandlerImpl is an adapter for the readHandler interface required by
// the reader type.
type readHandlerImpl struct {
	vc  *VC
	fid id.Flow
}

func (r readHandlerImpl) HandleRead(bytes uint) {
	r.vc.helper.AddReceiveBuffers(r.vc.vci, r.fid, bytes)
}

func copyDischargeMap(m map[string]security.Discharge) map[string]security.Discharge {
	ret := make(map[string]security.Discharge)
	for id, d := range m {
		ret[id] = d
	}
	return ret

}
