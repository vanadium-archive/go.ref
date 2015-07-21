// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"io"

	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
)

// FlowHandlers process accepted flows.
type FlowHandler interface {
	// HandleFlow processes an accepted flow.
	HandleFlow(flow.Flow) error
}

// Conns are a multiplexing encrypted channels that can host Flows.
type Conn struct {
}

// Ensure that *Conn implements flow.Conn
var _ flow.Conn = &Conn{}

// NewDialed dials a new Conn on the given conn.
// TODO(mattr): Add the flow.BlessingsForPeer (or whatever we
// called it) as the last param once that is added.
func NewDialed(
	ctx *context.T,
	conn io.ReadWriter,
	principal security.Principal,
	local, remote naming.Endpoint,
	versions version.RPCVersionRange,
	handler FlowHandler) (*Conn, error) {
	return nil, nil
}

// NewAccepted accepts a new Conn on the given conn.
func NewAccepted(
	ctx *context.T,
	conn io.ReadWriter,
	principal security.Principal,
	local naming.Endpoint,
	lBlessings security.Blessings,
	versions version.RPCVersionRange,
	handler FlowHandler,
	skipDischarges bool) (*Conn, error) {
	return nil, nil
}

// Dial dials a new flow on the Conn.
func (c *Conn) Dial() (flow.Flow, error) { return nil, nil }

// Closed returns a channel that will be closed after the Conn is shutdown.
// After this channel is closed it is guaranteed that all Dial calls will fail
// with an error and no more flows will be sent to the FlowHandler.
func (c *Conn) Closed() <-chan struct{} { return nil }

// LocalEndpoint returns the local vanadium Endpoint
func (c *Conn) LocalEndpoint() naming.Endpoint { return nil }

// RemoteEndpoint returns the remote vanadium Endpoint
func (c *Conn) RemoteEndpoint() naming.Endpoint { return nil }

// DialerPublicKey returns the public key presented by the dialer during authentication.
func (c *Conn) DialerPublicKey() security.PublicKey { return nil }

// AcceptorBlessings returns the blessings presented by the acceptor during authentication.
func (c *Conn) AcceptorBlessings() security.Blessings { return security.Blessings{} }

// AcceptorDischarges returns the discharges presented by the acceptor during authentication.
// Discharges are organized in a map keyed by the discharge-identifier.
func (c *Conn) AcceptorDischarges() map[string]security.Discharge { return nil }
