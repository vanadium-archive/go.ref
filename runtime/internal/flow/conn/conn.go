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

	"v.io/x/ref/runtime/internal/flow/flowcontrol"
)

// flowID is a number assigned to identify a flow.
// Each flow on a given conn will have a unique number.
type flowID uint64

const mtu = 1 << 16
const defaultBufferSize = 1 << 20
const reservedFlows = 10

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
	fc                *flowcontrol.FlowController
	mp                *messagePipe
	handler           FlowHandler
	versions          version.RPCVersionRange
	acceptorBlessings security.Blessings
	dialerPublicKey   security.PublicKey
	local, remote     naming.Endpoint
	closed            chan struct{}

	mu      sync.Mutex
	nextFid flowID
	flows   map[flowID]*flw
}

// Ensure that *Conn implements flow.Conn
var _ flow.Conn = &Conn{}

// NewDialed dials a new Conn on the given conn.
func NewDialed(
	ctx *context.T,
	conn MsgReadWriteCloser,
	local, remote naming.Endpoint,
	versions version.RPCVersionRange,
	handler FlowHandler,
	fn flow.BlessingsForPeer) (*Conn, error) {
	principal := v23.GetPrincipal(ctx)
	c := &Conn{
		fc:              flowcontrol.New(defaultBufferSize, mtu),
		mp:              newMessagePipe(conn),
		handler:         handler,
		versions:        versions,
		dialerPublicKey: principal.PublicKey(),
		local:           local,
		remote:          remote,
		closed:          make(chan struct{}),
		nextFid:         reservedFlows,
		flows:           map[flowID]*flw{},
	}
	go c.readLoop(ctx)
	return c, nil
}

// NewAccepted accepts a new Conn on the given conn.
func NewAccepted(
	ctx *context.T,
	conn MsgReadWriteCloser,
	local naming.Endpoint,
	lBlessings security.Blessings,
	versions version.RPCVersionRange,
	handler FlowHandler) (*Conn, error) {
	c := &Conn{
		fc:                flowcontrol.New(defaultBufferSize, mtu),
		mp:                newMessagePipe(conn),
		handler:           handler,
		versions:          versions,
		acceptorBlessings: lBlessings,
		local:             local,
		remote:            local, // TODO(mattr): Get the real remote endpoint.
		closed:            make(chan struct{}),
		nextFid:           reservedFlows + 1,
		flows:             map[flowID]*flw{},
	}
	go c.readLoop(ctx)
	return c, nil
}

// Dial dials a new flow on the Conn.
func (c *Conn) Dial(ctx *context.T) (flow.Flow, error) {
	defer c.mu.Unlock()
	c.mu.Lock()
	if c.flows == nil {
		return nil, NewErrConnectionClosed(ctx)
	}
	id := c.nextFid
	c.nextFid++
	return c.newFlowLocked(ctx, id), nil
}

// LocalEndpoint returns the local vanadium Endpoint
func (c *Conn) LocalEndpoint() naming.Endpoint { return c.local }

// RemoteEndpoint returns the remote vanadium Endpoint
func (c *Conn) RemoteEndpoint() naming.Endpoint { return c.remote }

// DialerPublicKey returns the public key presented by the dialer during authentication.
func (c *Conn) DialerPublicKey() security.PublicKey { return c.dialerPublicKey }

// AcceptorBlessings returns the blessings presented by the acceptor during authentication.
func (c *Conn) AcceptorBlessings() security.Blessings { return c.acceptorBlessings }

// AcceptorDischarges returns the discharges presented by the acceptor during authentication.
// Discharges are organized in a map keyed by the discharge-identifier.
func (c *Conn) AcceptorDischarges() map[string]security.Discharge { return nil }

// Closed returns a channel that will be closed after the Conn is shutdown.
// After this channel is closed it is guaranteed that all Dial calls will fail
// with an error and no more flows will be sent to the FlowHandler.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// Close shuts down a conn.  This will cause the read loop
// to exit.
func (c *Conn) Close(ctx *context.T, err error) {
	c.mu.Lock()
	var flows map[flowID]*flw
	flows, c.flows = c.flows, nil
	c.mu.Unlock()
	if flows == nil {
		// We've already torn this conn down.
		return
	}
	for _, f := range flows {
		f.close(err)
	}
	err = c.fc.Run(ctx, expressPriority, func(_ int) (int, bool, error) {
		return 0, true, c.mp.writeMsg(ctx, &tearDown{Err: err})
	})
	if err != nil {
		ctx.Errorf("Error sending tearDown on connection to %s: %v", c.remote, err)
	}
	if err = c.mp.close(); err != nil {
		ctx.Errorf("Error closing underlying connection for %s: %v", c.remote, err)
	}
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

	err := c.fc.Run(ctx, expressPriority, func(_ int) (int, bool, error) {
		err := c.mp.writeMsg(ctx, &addRecieveBuffers{
			counters: counts,
		})
		return 0, true, err
	})
	if err != nil {
		c.Close(ctx, NewErrSend(ctx, "addRecieveBuffers", c.remote.String(), err))
	}
}

func (c *Conn) readLoop(ctx *context.T) {
	var terr error
	defer c.Close(ctx, terr)

	for {
		x, err := c.mp.readMsg(ctx)
		if err != nil {
			c.Close(ctx, NewErrRecv(ctx, c.remote.String(), err))
			return
		}

		switch msg := x.(type) {
		case *tearDown:
			terr = msg.Err
			return

		case *openFlow:
			c.mu.Lock()
			f := c.newFlowLocked(ctx, msg.id)
			c.mu.Unlock()
			c.handler.HandleFlow(f)

		case *addRecieveBuffers:
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
			if terr = c.fc.Release(ctx, release); terr != nil {
				return
			}

		case *data:
			c.mu.Lock()
			f := c.flows[msg.id]
			c.mu.Unlock()
			if f == nil {
				ctx.Infof("Ignoring data message for unknown flow on connection to %s: %d", c.remote, msg.id)
				continue
			}
			if terr = f.q.put(ctx, msg.payload); terr != nil {
				return
			}
			if msg.flags&closeFlag != 0 {
				f.close(nil)
			}

		case *unencryptedData:
			c.mu.Lock()
			f := c.flows[msg.id]
			c.mu.Unlock()
			if f == nil {
				ctx.Infof("Ignoring data message for unknown flow: %d", msg.id)
				continue
			}
			if terr = f.q.put(ctx, msg.payload); terr != nil {
				return
			}
			if msg.flags&closeFlag != 0 {
				f.close(nil)
			}

		default:
			terr = NewErrUnexpectedMsg(ctx, reflect.TypeOf(msg).Name())
			return
		}
	}
}
