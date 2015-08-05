// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
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
	conn flow.MsgReadWriter,
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
		nextFid:         reservedFlows,
		flows:           map[flowID]*flw{},
	}
	go c.readLoop(ctx)
	return c, nil
}

// NewAccepted accepts a new Conn on the given conn.
func NewAccepted(
	ctx *context.T,
	conn flow.MsgReadWriter,
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

	id := c.nextFid
	c.nextFid++

	return c.newFlowLocked(ctx, id), nil
}

// Closed returns a channel that will be closed after the Conn is shutdown.
// After this channel is closed it is guaranteed that all Dial calls will fail
// with an error and no more flows will be sent to the FlowHandler.
func (c *Conn) Closed() <-chan struct{} { return c.closed }

// Close marks the Conn as closed. All Dial calls will fail with an error and
// no more flows will be sent to the FlowHandler.
func (c *Conn) Close() { close(c.closed) }

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

func (c *Conn) readLoop(ctx *context.T) {
	for {
		x, err := c.mp.readMsg(ctx)
		if err != nil {
			ctx.Errorf("Error reading from connection to %s: %v", c.remote, err)
			// TODO(mattr): tear down the conn.
		}

		switch msg := x.(type) {
		case *tearDown:
			// TODO(mattr): tear down the conn.

		case *openFlow:
			c.mu.Lock()
			f := c.newFlowLocked(ctx, msg.id)
			c.mu.Unlock()

			c.handler.HandleFlow(f)
			err := c.fc.Run(ctx, expressPriority, func(_ int) (int, bool, error) {
				err := c.mp.writeMsg(ctx, &addRecieveBuffers{
					counters: map[flowID]uint64{msg.id: defaultBufferSize},
				})
				return 0, true, err
			})
			if err != nil {
				// TODO(mattr): Maybe in this case we should close the conn.
				ctx.Errorf("Error sending counters on connection to %s: %v", c.remote, err)
			}

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
			if err := c.fc.Release(release); err != nil {
				ctx.Errorf("Error releasing counters from connection to %s: %v", c.remote, err)
			}

		case *data:
			c.mu.Lock()
			f := c.flows[msg.id]
			c.mu.Unlock()
			if f == nil {
				ctx.Errorf("Ignoring data message for unknown flow on connection to %s: %d", c.remote, msg.id)
				continue
			}
			if err := f.q.Put(msg.payload); err != nil {
				ctx.Errorf("Ignoring data message for closed flow on connection to %s: %d", c.remote, msg.id)
			}
			// TODO(mattr): perhaps close the flow.
			// TODO(mattr): check if the q is full.

		case *unencryptedData:
			c.mu.Lock()
			f := c.flows[msg.id]
			c.mu.Unlock()
			if f == nil {
				ctx.Errorf("Ignoring data message for unknown flow: %d", msg.id)
				continue
			}
			if err := f.q.Put(msg.payload); err != nil {
				ctx.Errorf("Ignoring data message for closed flow: %d", msg.id)
			}
			// TODO(mattr): perhaps close the flow.
			// TODO(mattr): check if the q is full.

		default:
			// TODO(mattr): tearDown the conn.
		}
	}
}
