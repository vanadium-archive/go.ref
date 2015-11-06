// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"reflect"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"
	iflow "v.io/x/ref/runtime/internal/flow"
	inaming "v.io/x/ref/runtime/internal/naming"
)

// flowID is a number assigned to identify a flow.
// Each flow on a given conn will have a unique number.
const (
	invalidFlowID = iota
	blessingsFlowID
	reservedFlows = 10
)

const mtu = 1 << 16
const DefaultBytesBufferedPerFlow = 1 << 20

const (
	expressPriority = iota
	flowPriority
	tearDownPriority

	// Must be last.
	numPriorities
)

// FlowHandlers process accepted flows.
type FlowHandler interface {
	// HandleFlow processes an accepted flow.
	HandleFlow(flow.Flow) error
}

type Status int

const (
	// Note that this is a progression of states that can only
	// go in one direction.  We use inequality operators to see
	// how far along in the progression we are, so the order of
	// these is important.
	Active Status = iota
	EnteringLameDuck
	LameDuckAcknowledged
	Closing
	Closed
)

// Conns are a multiplexing encrypted channels that can host Flows.
type Conn struct {
	// All the variables here are set before the constructor returns
	// and never changed after that.
	mp            *messagePipe
	version       version.RPCVersion
	lBlessings    security.Blessings
	rBKey         uint64
	local, remote naming.Endpoint
	closed        chan struct{}
	lameDucked    chan struct{}
	blessingsFlow *blessingsFlow
	loopWG        sync.WaitGroup
	unopenedFlows sync.WaitGroup
	isProxy       bool
	cancel        context.CancelFunc

	mu sync.Mutex // All the variables below here are protected by mu.

	rPublicKey     security.PublicKey
	status         Status
	remoteLameDuck bool
	handler        FlowHandler
	nextFid        uint64
	dischargeTimer *time.Timer
	lastUsedTime   time.Time
	flows          map[uint64]*flw

	// TODO(mattr): Integrate these maps back into the flows themselves as
	// has been done with the sending counts.
	// toRelease is a map from flowID to a number of tokens which are pending
	// to be released.  We only send release messages when some flow has
	// used up at least half it's buffer, and then we send the counters for
	// every flow.  This reduces the number of release messages that are sent.
	toRelease map[uint64]uint64
	// borrowing is a map from flowID to a boolean indicating whether the remote
	// dialer of the flow is using shared counters for his sends because we've not
	// yet sent a release for this flow.
	borrowing map[uint64]bool

	// In our protocol new flows are opened by the dialer by immediately
	// starting to write data for that flow (in an OpenFlow message).
	// Since the other side doesn't yet know of the existence of this new
	// flow, it couldn't have allocated us any counters via a Release message.
	// In order to deal with this the conn maintains a pool of shared tokens
	// which are used by dialers of new flows.
	// lshared is the number of shared tokens available for new flows dialed
	// locally.
	lshared uint64

	// activeWriters keeps track of all the flows that are currently
	// trying to write, indexed by priority.  activeWriters[0] is a list
	// (note that writers form a linked list for this purpose)
	// of all the highest priority flows.  activeWriters[len-1] is a list
	// of all the lowest priority writing flows.
	activeWriters []writer
	writing       writer
}

// Ensure that *Conn implements flow.ManagedConn.
var _ flow.ManagedConn = &Conn{}

// NewDialed dials a new Conn on the given conn.
func NewDialed(
	ctx *context.T,
	lBlessings security.Blessings,
	conn flow.MsgReadWriteCloser,
	local, remote naming.Endpoint,
	versions version.RPCVersionRange,
	auth flow.PeerAuthorizer,
	handshakeTimeout time.Duration,
	handler FlowHandler) (*Conn, error) {
	ctx, cancel := context.WithRootCancel(ctx)
	c := &Conn{
		mp:           newMessagePipe(conn),
		handler:      handler,
		lBlessings:   lBlessings,
		local:        endpointCopy(local),
		remote:       endpointCopy(remote),
		closed:       make(chan struct{}),
		lameDucked:   make(chan struct{}),
		nextFid:      reservedFlows,
		flows:        map[uint64]*flw{},
		lastUsedTime: time.Now(),
		toRelease:    map[uint64]uint64{},
		borrowing:    map[uint64]bool{},
		cancel:       cancel,
		// TODO(mattr): We should negotiate the shared counter pool size with the
		// other end.
		lshared:       DefaultBytesBufferedPerFlow,
		activeWriters: make([]writer, numPriorities),
	}
	// TODO(mattr): This scheme for deadlines is nice, but it doesn't
	// provide for cancellation when ctx is canceled.
	t := time.AfterFunc(handshakeTimeout, func() { conn.Close() })
	err := c.dialHandshake(ctx, versions, auth)
	if stopped := t.Stop(); !stopped {
		err = verror.NewErrTimeout(ctx)
	}
	if err != nil {
		c.Close(ctx, err)
		return nil, err
	}
	c.loopWG.Add(1)
	go c.readLoop(ctx)
	return c, nil
}

// NewAccepted accepts a new Conn on the given conn.
func NewAccepted(
	ctx *context.T,
	lBlessings security.Blessings,
	lAuthorizedPeers []security.BlessingPattern,
	conn flow.MsgReadWriteCloser,
	local naming.Endpoint,
	versions version.RPCVersionRange,
	handshakeTimeout time.Duration,
	handler FlowHandler) (*Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	c := &Conn{
		mp:            newMessagePipe(conn),
		handler:       handler,
		lBlessings:    lBlessings,
		local:         endpointCopy(local),
		closed:        make(chan struct{}),
		lameDucked:    make(chan struct{}),
		nextFid:       reservedFlows + 1,
		flows:         map[uint64]*flw{},
		lastUsedTime:  time.Now(),
		toRelease:     map[uint64]uint64{},
		borrowing:     map[uint64]bool{},
		cancel:        cancel,
		lshared:       DefaultBytesBufferedPerFlow,
		activeWriters: make([]writer, numPriorities),
	}
	// FIXME(mattr): This scheme for deadlines is nice, but it doesn't
	// provide for cancellation when ctx is canceled.
	t := time.AfterFunc(handshakeTimeout, func() { conn.Close() })
	err := c.acceptHandshake(ctx, versions, lAuthorizedPeers)
	if stopped := t.Stop(); !stopped {
		err = verror.NewErrTimeout(ctx)
	}
	if err != nil {
		c.Close(ctx, err)
		return nil, err
	}
	c.loopWG.Add(1)
	go c.readLoop(ctx)
	return c, nil
}

// Enter LameDuck mode, the returned channel will be closed when the remote
// end has ack'd or the Conn is closed.
func (c *Conn) EnterLameDuck(ctx *context.T) chan struct{} {
	var err error
	c.mu.Lock()
	if c.status < EnteringLameDuck {
		c.status = EnteringLameDuck
		err = c.sendMessageLocked(ctx, false, expressPriority, &message.EnterLameDuck{})
	}
	c.mu.Unlock()
	if err != nil {
		c.Close(ctx, NewErrSend(ctx, "release", c.remote.String(), err))
	}
	return c.lameDucked
}

// Dial dials a new flow on the Conn.
func (c *Conn) Dial(ctx *context.T, auth flow.PeerAuthorizer, remote naming.Endpoint) (flow.Flow, error) {
	if c.remote.RoutingID() == naming.NullRoutingID {
		return nil, NewErrDialingNonServer(ctx)
	}
	rBlessings, rDischarges, err := c.blessingsFlow.getLatestRemote(ctx, c.rBKey)
	if err != nil {
		return nil, err
	}
	var bkey, dkey uint64
	var blessings security.Blessings
	var discharges map[string]security.Discharge
	if !c.isProxy {
		// TODO(suharshs): On the first flow dial, find a way to not call this twice.
		rbnames, rejected, err := auth.AuthorizePeer(ctx, c.local, remote, rBlessings, rDischarges)
		if err != nil {
			return nil, iflow.MaybeWrapError(verror.ErrNotTrusted, ctx, err)
		}
		blessings, discharges, err = auth.BlessingsForPeer(ctx, rbnames)
		if err != nil {
			return nil, NewErrNoBlessingsForPeer(ctx, rbnames, rejected, err)
		}
	}
	if blessings.IsZero() {
		// its safe to ignore this error since c.lBlessings must be valid, so the
		// encoding of the publicKey can never error out.
		blessings, _ = security.NamelessBlessing(c.lBlessings.PublicKey())
	}
	if bkey, dkey, err = c.blessingsFlow.send(ctx, blessings, discharges, nil); err != nil {
		return nil, err
	}
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.remoteLameDuck || c.status >= Closing {
		return nil, NewErrConnectionClosed(ctx)
	}
	id := c.nextFid
	c.nextFid += 2
	return c.newFlowLocked(ctx, id, bkey, dkey, remote, true, false), nil
}

// LocalEndpoint returns the local vanadium Endpoint
func (c *Conn) LocalEndpoint() naming.Endpoint { return c.local }

// RemoteEndpoint returns the remote vanadium Endpoint
func (c *Conn) RemoteEndpoint() naming.Endpoint { return c.remote }

// LocalBlessings returns the local blessings.
func (c *Conn) LocalBlessings() security.Blessings { return c.lBlessings }

// RemoteBlessings returns the remote blessings.
func (c *Conn) RemoteBlessings() security.Blessings {
	// Its safe to ignore this error. It means that this conn is closed.
	blessings, _, _ := c.blessingsFlow.getLatestRemote(nil, c.rBKey)
	return blessings
}

// CommonVersion returns the RPCVersion negotiated between the local and remote endpoints.
func (c *Conn) CommonVersion() version.RPCVersion { return c.version }

// LastUsedTime returns the time at which the Conn had bytes read or written on it.
func (c *Conn) LastUsedTime() time.Time {
	defer c.mu.Unlock()
	c.mu.Lock()
	return c.lastUsedTime
}

// RemoteLameDuck returns true if the other end of the connection has announced that
// it is in lame duck mode indicating that new flows should not be dialed on this
// conn.
func (c *Conn) RemoteLameDuck() bool {
	defer c.mu.Unlock()
	c.mu.Lock()
	return c.remoteLameDuck
}

// Closed returns a channel that will be closed after the Conn is shutdown.
// After this channel is closed it is guaranteed that all Dial calls will fail
// with an error and no more flows will be sent to the FlowHandler.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

func (c *Conn) Status() Status {
	c.mu.Lock()
	status := c.status
	c.mu.Unlock()
	return status
}

// Close shuts down a conn.
func (c *Conn) Close(ctx *context.T, err error) {
	c.mu.Lock()
	c.internalCloseLocked(ctx, err)
	c.mu.Unlock()
	<-c.closed
}

func (c *Conn) internalCloseLocked(ctx *context.T, err error) {
	ctx.VI(2).Infof("Closing connection: %v", err)

	flows := c.flows
	if c.dischargeTimer != nil {
		if c.dischargeTimer.Stop() {
			c.loopWG.Done()
		}
		c.dischargeTimer = nil
	}
	if c.status >= Closing {
		// This conn is already being torn down.
		return
	}
	if c.status < LameDuckAcknowledged {
		close(c.lameDucked)
	}
	c.status = Closing

	go func(c *Conn) {
		if verror.ErrorID(err) != ErrConnClosedRemotely.ID {
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			c.mu.Lock()
			cerr := c.sendMessageLocked(ctx, false, tearDownPriority, &message.TearDown{
				Message: msg,
			})
			c.mu.Unlock()
			if cerr != nil {
				ctx.Errorf("Error sending tearDown on connection to %s: %v", c.remote, cerr)
			}
		}
		if err == nil {
			err = NewErrConnectionClosed(ctx)
		}
		for _, f := range flows {
			f.close(ctx, err)
		}
		if c.blessingsFlow != nil {
			c.blessingsFlow.close(ctx, err)
		}
		if cerr := c.mp.rw.Close(); cerr != nil {
			ctx.Errorf("Error closing underlying connection for %s: %v", c.remote, cerr)
		}
		c.loopWG.Wait()
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		c.status = Closed
		close(c.closed)
		c.mu.Unlock()
	}(c)
}

func (c *Conn) release(ctx *context.T, fid, count uint64) {
	var toRelease map[uint64]uint64
	var release bool
	c.mu.Lock()
	c.toRelease[fid] += count
	if c.borrowing[fid] {
		c.toRelease[invalidFlowID] += count
		release = c.toRelease[invalidFlowID] > DefaultBytesBufferedPerFlow/2
	} else {
		release = c.toRelease[fid] > DefaultBytesBufferedPerFlow/2
	}
	if release {
		toRelease = c.toRelease
		c.toRelease = make(map[uint64]uint64, len(c.toRelease))
		c.borrowing = make(map[uint64]bool, len(c.borrowing))
	}
	var err error
	if toRelease != nil {
		delete(toRelease, invalidFlowID)
		err = c.sendMessageLocked(ctx, true, expressPriority, &message.Release{
			Counters: toRelease,
		})
	}
	c.mu.Unlock()
	if err != nil {
		c.Close(ctx, NewErrSend(ctx, "release", c.remote.String(), err))
	}
}

func (c *Conn) handleMessage(ctx *context.T, m message.Message) error {
	switch msg := m.(type) {
	case *message.TearDown:
		c.mu.Lock()
		c.internalCloseLocked(ctx, NewErrConnClosedRemotely(ctx, msg.Message))
		c.mu.Unlock()
		return nil

	case *message.EnterLameDuck:
		c.mu.Lock()
		c.remoteLameDuck = true
		c.mu.Unlock()
		go func() {
			// We only want to send the lame duck acknowledgment after all outstanding
			// OpenFlows are sent.
			c.unopenedFlows.Wait()
			c.mu.Lock()
			err := c.sendMessageLocked(ctx, true, expressPriority, &message.AckLameDuck{})
			c.mu.Unlock()
			if err != nil {
				c.Close(ctx, NewErrSend(ctx, "release", c.remote.String(), err))
			}
		}()

	case *message.AckLameDuck:
		c.mu.Lock()
		if c.status < LameDuckAcknowledged {
			c.status = LameDuckAcknowledged
			close(c.lameDucked)
		}
		c.mu.Unlock()

	case *message.OpenFlow:
		c.mu.Lock()
		if c.nextFid%2 == msg.ID%2 {
			c.mu.Unlock()
			return NewErrInvalidPeerFlow(ctx)
		}
		if c.handler == nil {
			c.mu.Unlock()
			return NewErrUnexpectedMsg(ctx, "openFlow")
		} else if c.status == Closing {
			c.mu.Unlock()
			return nil // Conn is already being closed.
		}
		handler := c.handler
		f := c.newFlowLocked(ctx, msg.ID, msg.BlessingsKey, msg.DischargeKey, nil, false, true)
		f.releaseLocked(msg.InitialCounters)
		c.toRelease[msg.ID] = DefaultBytesBufferedPerFlow
		c.borrowing[msg.ID] = true
		c.mu.Unlock()

		handler.HandleFlow(f)
		if err := f.q.put(ctx, msg.Payload); err != nil {
			return err
		}
		if msg.Flags&message.CloseFlag != 0 {
			f.close(ctx, NewErrFlowClosedRemotely(f.ctx))
		}

	case *message.Release:
		c.mu.Lock()
		for fid, val := range msg.Counters {
			if f := c.flows[fid]; f != nil {
				f.releaseLocked(val)
			}
		}
		c.mu.Unlock()

	case *message.Data:
		c.mu.Lock()
		if c.status == Closing {
			c.mu.Unlock()
			return nil // Conn is already being shut down.
		}
		f := c.flows[msg.ID]
		c.mu.Unlock()
		if f == nil {
			ctx.Infof("Ignoring data message for unknown flow on connection to %s: %d",
				c.remote, msg.ID)
			return nil
		}
		if err := f.q.put(ctx, msg.Payload); err != nil {
			return err
		}
		if msg.Flags&message.CloseFlag != 0 {
			f.close(ctx, NewErrFlowClosedRemotely(f.ctx))
		}

	default:
		return NewErrUnexpectedMsg(ctx, reflect.TypeOf(msg).String())
	}
	return nil
}

func (c *Conn) readLoop(ctx *context.T) {
	defer c.loopWG.Done()
	var err error
	for {
		msg, rerr := c.mp.readMsg(ctx)
		if rerr != nil {
			err = NewErrRecv(ctx, c.remote.String(), rerr)
			break
		}
		if err = c.handleMessage(ctx, msg); err != nil {
			break
		}
	}
	c.mu.Lock()
	c.internalCloseLocked(ctx, err)
	c.mu.Unlock()
}

func (c *Conn) markUsed() {
	c.mu.Lock()
	c.markUsedLocked()
	c.mu.Unlock()
}

func (c *Conn) markUsedLocked() {
	c.lastUsedTime = time.Now()
}

func (c *Conn) IsEncapsulated() bool {
	_, ok := c.mp.rw.(*flw)
	return ok
}

func (c *Conn) UpdateFlowHandler(ctx *context.T, handler FlowHandler) error {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.handler == nil && handler != nil {
		return NewErrUpdatingNilFlowHandler(ctx)
	}
	c.handler = handler
	return nil
}

type writer interface {
	notify()
	priority() int
	neighbors() (prev, next writer)
	setNeighbors(prev, next writer)
}

// activateWriterLocked adds a given writer to the list of active writers.
// The writer will be given a turn when the channel becomes available.
// You should try to only have writers with actual work to do in the
// list of activeWriters because we will switch to that thread to allow it
// to do work, and it will be wasteful if it turns out there is no work to do.
// After calling this you should typically call notifyNextWriterLocked.
func (c *Conn) activateWriterLocked(w writer) {
	priority := w.priority()
	_, wn := w.neighbors()
	head := c.activeWriters[priority]
	if head == w || wn != w {
		// We're already active.
		return
	}
	if head == nil { // We're the head of the list.
		c.activeWriters[priority] = w
	} else { // Insert us before head, which is the end of the list.
		hp, _ := head.neighbors()
		w.setNeighbors(hp, head)
		hp.setNeighbors(nil, w)
		head.setNeighbors(w, nil)
	}
}

// deactivateWriterLocked removes a writer from the active writer list.  After
// this function is called it is certain that the writer will not be given any
// new turns.  If the writer is already in the middle of a turn, that turn is
// not terminated, workers must end their turn explicitly by calling
// notifyNextWriterLocked.
func (c *Conn) deactivateWriterLocked(w writer) {
	priority := w.priority()
	p, n := w.neighbors()
	if head := c.activeWriters[priority]; head == w {
		if w == n { // We're the only one in the list.
			c.activeWriters[priority] = nil
		} else {
			c.activeWriters[priority] = n
		}
	}
	n.setNeighbors(p, nil)
	p.setNeighbors(nil, n)
	w.setNeighbors(w, w)
}

// notifyNextWriterLocked notifies the highest priority activeWriter to take
// a turn writing.  If w is the active writer give up w's claim and choose
// the next writer.  If there is already an active writer != w, this function does
// nothing.
func (c *Conn) notifyNextWriterLocked(w writer) {
	if c.writing == w {
		c.writing = nil
	}
	if c.writing == nil {
		for p, head := range c.activeWriters {
			if head != nil {
				_, c.activeWriters[p] = head.neighbors()
				c.writing = head
				head.notify()
				return
			}
		}
	}
}

type writerList struct {
	// next and prev are protected by c.mu
	next, prev writer
}

func (s *writerList) neighbors() (prev, next writer) { return s.prev, s.next }
func (s *writerList) setNeighbors(prev, next writer) {
	if prev != nil {
		s.prev = prev
	}
	if next != nil {
		s.next = next
	}
}

// singleMessageWriter is used to send a single message with a given priority.
type singleMessageWriter struct {
	writeCh chan struct{}
	p       int
	writerList
}

func (s *singleMessageWriter) notify()       { close(s.writeCh) }
func (s *singleMessageWriter) priority() int { return s.p }

// sendMessageLocked sends a single message on the conn with the given priority.
// if cancelWithContext is true, then this write attempt will fail when the context
// is canceled.  Otherwise context cancellation will have no effect and this call
// will block until the message is sent.
// NOTE: The mutex is not held for the entirety of this call,
// therefore this call will interrupt your critical section. This
// should be called only at the end of a mutex protected region.
func (c *Conn) sendMessageLocked(
	ctx *context.T,
	cancelWithContext bool,
	priority int,
	m message.Message) (err error) {
	s := &singleMessageWriter{writeCh: make(chan struct{}), p: priority}
	s.next, s.prev = s, s
	c.activateWriterLocked(s)
	c.notifyNextWriterLocked(s)
	c.mu.Unlock()
	// wait for my turn.
	if cancelWithContext {
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-s.writeCh:
		}
	} else {
		<-s.writeCh
	}
	// send the actual message.
	if err == nil {
		err = c.mp.writeMsg(ctx, m)
	}
	c.mu.Lock()
	c.deactivateWriterLocked(s)
	c.notifyNextWriterLocked(s)
	return err
}

func endpointCopy(ep naming.Endpoint) naming.Endpoint {
	var cp inaming.Endpoint = *(ep.(*inaming.Endpoint))
	return &cp
}
