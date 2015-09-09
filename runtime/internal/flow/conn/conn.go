// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"reflect"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref/runtime/internal/flow/flowcontrol"
)

// flowID is a number assigned to identify a flow.
// Each flow on a given conn will have a unique number.
const (
	invalidFlowID = iota
	blessingsFlowID
	reservedFlows = 10
)

const mtu = 1 << 16
const defaultBufferSize = 1 << 20

const (
	expressPriority = iota
	flowPriority
	tearDownPriority
)

// FlowHandlers process accepted flows.
type FlowHandler interface {
	// HandleFlow processes an accepted flow.
	HandleFlow(flow.Flow) error
}

// Conns are a multiplexing encrypted channels that can host Flows.
type Conn struct {
	fc                     *flowcontrol.FlowController
	mp                     *messagePipe
	handler                FlowHandler
	version                version.RPCVersion
	lBlessings, rBlessings security.Blessings
	local, remote          naming.Endpoint
	closed                 chan struct{}
	blessingsFlow          *blessingsFlow
	loopWG                 sync.WaitGroup

	mu             sync.Mutex
	nextFid        uint64
	flows          map[uint64]*flw
	dischargeTimer *time.Timer
	lastUsedTime   time.Time
}

// Ensure that *Conn implements flow.ManagedConn.
var _ flow.ManagedConn = &Conn{}

// NewDialed dials a new Conn on the given conn.
func NewDialed(
	ctx *context.T,
	conn flow.MsgReadWriteCloser,
	local, remote naming.Endpoint,
	versions version.RPCVersionRange,
	handler FlowHandler) (*Conn, error) {
	c := &Conn{
		fc:           flowcontrol.New(defaultBufferSize, mtu),
		mp:           newMessagePipe(conn),
		handler:      handler,
		lBlessings:   v23.GetPrincipal(ctx).BlessingStore().Default(),
		local:        local,
		remote:       remote,
		closed:       make(chan struct{}),
		nextFid:      reservedFlows,
		flows:        map[uint64]*flw{},
		lastUsedTime: time.Now(),
	}
	if err := c.dialHandshake(ctx, versions); err != nil {
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
	conn flow.MsgReadWriteCloser,
	local naming.Endpoint,
	versions version.RPCVersionRange,
	handler FlowHandler) (*Conn, error) {
	c := &Conn{
		fc:           flowcontrol.New(defaultBufferSize, mtu),
		mp:           newMessagePipe(conn),
		handler:      handler,
		lBlessings:   v23.GetPrincipal(ctx).BlessingStore().Default(),
		local:        local,
		closed:       make(chan struct{}),
		nextFid:      reservedFlows + 1,
		flows:        map[uint64]*flw{},
		lastUsedTime: time.Now(),
	}
	if err := c.acceptHandshake(ctx, versions); err != nil {
		c.Close(ctx, err)
		return nil, err
	}
	c.loopWG.Add(1)
	go c.readLoop(ctx)
	return c, nil
}

// Dial dials a new flow on the Conn.
func (c *Conn) Dial(ctx *context.T, fn flow.BlessingsForPeer) (flow.Flow, error) {
	if c.rBlessings.IsZero() {
		return nil, NewErrDialingNonServer(ctx)
	}
	rDischarges, err := c.blessingsFlow.getLatestDischarges(ctx, c.rBlessings)
	if err != nil {
		return nil, err
	}
	blessings, discharges, err := fn(ctx, c.local, c.remote, c.rBlessings, rDischarges)
	if err != nil {
		return nil, err
	}
	bkey, dkey, err := c.blessingsFlow.put(ctx, blessings, discharges)
	if err != nil {
		return nil, err
	}
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.flows == nil {
		return nil, NewErrConnectionClosed(ctx)
	}
	id := c.nextFid
	c.nextFid++
	return c.newFlowLocked(ctx, id, bkey, dkey, true, false), nil
}

// LocalEndpoint returns the local vanadium Endpoint
func (c *Conn) LocalEndpoint() naming.Endpoint { return c.local }

// RemoteEndpoint returns the remote vanadium Endpoint
func (c *Conn) RemoteEndpoint() naming.Endpoint { return c.remote }

// CommonVersion returns the RPCVersion negotiated between the local and remote endpoints.
func (c *Conn) CommonVersion() version.RPCVersion { return c.version }

// LastUsedTime returns the time at which the Conn had bytes read or written on it.
func (c *Conn) LastUsedTime() time.Time {
	defer c.mu.Unlock()
	c.mu.Lock()
	return c.lastUsedTime
}

// Closed returns a channel that will be closed after the Conn is shutdown.
// After this channel is closed it is guaranteed that all Dial calls will fail
// with an error and no more flows will be sent to the FlowHandler.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// Close shuts down a conn.
func (c *Conn) Close(ctx *context.T, err error) {
	c.mu.Lock()
	var flows map[uint64]*flw
	flows, c.flows = c.flows, nil
	if c.dischargeTimer != nil {
		c.dischargeTimer.Stop()
		c.dischargeTimer = nil
	}
	c.mu.Unlock()

	if flows == nil {
		// This conn is already being torn down.
		<-c.closed
		return
	}
	c.internalClose(ctx, err, flows)
}

func (c *Conn) internalClose(ctx *context.T, err error, flows map[uint64]*flw) {
	ctx.VI(2).Infof("Closing connection: %v", err)
	if verror.ErrorID(err) != ErrConnClosedRemotely.ID {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		cerr := c.fc.Run(ctx, "close", expressPriority, func(_ int) (int, bool, error) {
			return 0, true, c.mp.writeMsg(ctx, &message.TearDown{Message: msg})
		})
		if cerr != nil {
			ctx.Errorf("Error sending tearDown on connection to %s: %v", c.remote, cerr)
		}
	}
	for _, f := range flows {
		f.close(ctx, NewErrConnectionClosed(ctx))
	}
	if cerr := c.mp.close(); cerr != nil {
		ctx.Errorf("Error closing underlying connection for %s: %v", c.remote, cerr)
	}
	c.loopWG.Wait()
	close(c.closed)
}

func (c *Conn) release(ctx *context.T) {
	counts := map[uint64]uint64{}
	c.mu.Lock()
	for fid, f := range c.flows {
		if release := f.q.release(); release > 0 {
			counts[fid] = uint64(release)
		}
	}
	c.mu.Unlock()
	if len(counts) == 0 {
		return
	}

	err := c.fc.Run(ctx, "release", expressPriority, func(_ int) (int, bool, error) {
		err := c.mp.writeMsg(ctx, &message.Release{
			Counters: counts,
		})
		return 0, true, err
	})
	if err != nil {
		c.Close(ctx, NewErrSend(ctx, "release", c.remote.String(), err))
	}
}

func (c *Conn) handleMessage(ctx *context.T, m message.Message) error {
	switch msg := m.(type) {
	case *message.TearDown:
		return NewErrConnClosedRemotely(ctx, msg.Message)

	case *message.OpenFlow:
		if c.handler == nil {
			return NewErrUnexpectedMsg(ctx, "openFlow")
		}
		c.mu.Lock()
		f := c.newFlowLocked(ctx, msg.ID, msg.BlessingsKey, msg.DischargeKey, false, true)
		c.mu.Unlock()
		c.handler.HandleFlow(f)

	case *message.Release:
		release := make([]flowcontrol.Release, 0, len(msg.Counters))
		c.mu.Lock()
		for fid, val := range msg.Counters {
			if f := c.flows[fid]; f != nil {
				release = append(release, flowcontrol.Release{
					Worker: f.worker,
					Tokens: int(val),
				})
			}
		}
		c.mu.Unlock()
		if err := c.fc.Release(ctx, release); err != nil {
			return err
		}

	case *message.Data:
		c.mu.Lock()
		f := c.flows[msg.ID]
		c.mu.Unlock()
		if f == nil {
			ctx.Infof("Ignoring data message for unknown flow on connection to %s: %d", c.remote, msg.ID)
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
	var flows map[uint64]*flw
	flows, c.flows = c.flows, nil
	c.mu.Unlock()

	c.loopWG.Done()
	if flows != nil {
		c.internalClose(ctx, err, flows)
	}
}

func (c *Conn) markUsed() {
	c.mu.Lock()
	c.lastUsedTime = time.Now()
	c.mu.Unlock()
}
