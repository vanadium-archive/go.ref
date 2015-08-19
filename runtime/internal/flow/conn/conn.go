// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"reflect"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref/runtime/internal/flow/flowcontrol"
)

// flowID is a number assigned to identify a flow.
// Each flow on a given conn will have a unique number.
type flowID uint64

const (
	invalidFlowID = flowID(iota)
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

type MsgReadWriteCloser interface {
	flow.MsgReadWriter
	Close() error
}

// FlowHandlers process accepted flows.
type FlowHandler interface {
	// HandleFlow processes an accepted flow.
	HandleFlow(flow.Flow) error
}

// Conns are a multiplexing encrypted channels that can host Flows.
type Conn struct {
	fc                       *flowcontrol.FlowController
	mp                       *messagePipe
	handler                  FlowHandler
	version                  version.RPCVersion
	lBlessings, rBlessings   security.Blessings
	rDischarges, lDischarges map[string]security.Discharge
	local, remote            naming.Endpoint
	closed                   chan struct{}
	blessingsFlow            *blessingsFlow
	loopWG                   sync.WaitGroup

	mu      sync.Mutex
	nextFid flowID
	flows   map[flowID]*flw
}

// Ensure that *Conn implements flow.Conn.
var _ flow.Conn = &Conn{}

// NewDialed dials a new Conn on the given conn.
func NewDialed(
	ctx *context.T,
	conn MsgReadWriteCloser,
	local, remote naming.Endpoint,
	versions version.RPCVersionRange,
	handler FlowHandler) (*Conn, error) {
	c := &Conn{
		fc:         flowcontrol.New(defaultBufferSize, mtu),
		mp:         newMessagePipe(conn),
		handler:    handler,
		lBlessings: v23.GetPrincipal(ctx).BlessingStore().Default(),
		local:      local,
		remote:     remote,
		closed:     make(chan struct{}),
		nextFid:    reservedFlows,
		flows:      map[flowID]*flw{},
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
	conn MsgReadWriteCloser,
	local naming.Endpoint,
	versions version.RPCVersionRange,
	handler FlowHandler) (*Conn, error) {
	c := &Conn{
		fc:         flowcontrol.New(defaultBufferSize, mtu),
		mp:         newMessagePipe(conn),
		handler:    handler,
		lBlessings: v23.GetPrincipal(ctx).BlessingStore().Default(),
		local:      local,
		closed:     make(chan struct{}),
		nextFid:    reservedFlows + 1,
		flows:      map[flowID]*flw{},
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
	blessings, err := fn(ctx, c.local, c.remote, c.rBlessings, c.rDischarges)
	if err != nil {
		return nil, err
	}
	bkey, dkey, err := c.blessingsFlow.put(ctx, blessings, nil)
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

// Closed returns a channel that will be closed after the Conn is shutdown.
// After this channel is closed it is guaranteed that all Dial calls will fail
// with an error and no more flows will be sent to the FlowHandler.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// Close shuts down a conn.
func (c *Conn) Close(ctx *context.T, err error) {
	c.mu.Lock()
	var flows map[flowID]*flw
	flows, c.flows = c.flows, nil
	c.mu.Unlock()

	if flows == nil {
		// This conn is already being torn down.
		<-c.closed
		return
	}
	c.internalClose(ctx, err, flows)
}

func (c *Conn) internalClose(ctx *context.T, err error, flows map[flowID]*flw) {
	if verror.ErrorID(err) != ErrConnClosedRemotely.ID {
		message := ""
		if err != nil {
			message = err.Error()
		}
		cerr := c.fc.Run(ctx, "close", expressPriority, func(_ int) (int, bool, error) {
			return 0, true, c.mp.writeMsg(ctx, &tearDown{Message: message})
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
	counts := map[flowID]uint64{}
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
		err := c.mp.writeMsg(ctx, &release{
			counters: counts,
		})
		return 0, true, err
	})
	if err != nil {
		c.Close(ctx, NewErrSend(ctx, "release", c.remote.String(), err))
	}
}

func (c *Conn) handleMessage(ctx *context.T, x message) error {
	switch msg := x.(type) {
	case *tearDown:
		return NewErrConnClosedRemotely(ctx, msg.Message)

	case *openFlow:
		if c.handler == nil {
			return NewErrUnexpectedMsg(ctx, "openFlow")
		}
		c.mu.Lock()
		f := c.newFlowLocked(ctx, msg.id, msg.bkey, msg.dkey, false, false)
		c.mu.Unlock()
		c.handler.HandleFlow(f)

	case *release:
		release := make([]flowcontrol.Release, 0, len(msg.counters))
		c.mu.Lock()
		for fid, val := range msg.counters {
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

	case *data:
		c.mu.Lock()
		f := c.flows[msg.id]
		c.mu.Unlock()
		if f == nil {
			ctx.Infof("Ignoring data message for unknown flow on connection to %s: %d", c.remote, msg.id)
			return nil
		}
		if err := f.q.put(ctx, msg.payload); err != nil {
			return err
		}
		if msg.flags&closeFlag != 0 {
			f.close(ctx, NewErrFlowClosedRemotely(f.ctx))
		}

	case *unencryptedData:
		c.mu.Lock()
		f := c.flows[msg.id]
		c.mu.Unlock()
		if f == nil {
			ctx.Infof("Ignoring data message for unknown flow: %d", msg.id)
			return nil
		}
		if err := f.q.put(ctx, msg.payload); err != nil {
			return err
		}
		if msg.flags&closeFlag != 0 {
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
	var flows map[flowID]*flw
	flows, c.flows = c.flows, nil
	c.mu.Unlock()

	c.loopWG.Done()
	if flows != nil {
		c.internalClose(ctx, err, flows)
	}
}
