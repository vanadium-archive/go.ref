// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flow

import (
	"io"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
)

// Manager is the interface for managing the creation of Flows.
type Manager interface {
	// Listen creates a Listener that can be used to listen on addresses
	// and Accept flows initiates to those addresses.
	//
	// For example:
	//   ln, err := sm.NewListener(ctx, blessings)
	//   ep, err := ln.Listen("tcp", ":0")
	//   for {
	//     flow, err := ln.Accept(ctx)
	//     // process flow
	//   }
	// can be used to accept Flows initiated by remote processes to the endpoint
	// returned by ln.Listen.
	//
	// blessings are the Blessings presented to the Client during authentication.
	NewListener(ctx *context.T, blessings security.Blessings) (Listener, error)

	// Dial creates a Flow to the provided remote endpoint.
	Dial(ctx *context.T, remote naming.Endpoint) (Flow, error)

	// Closed returns a channel that remains open for the lifetime of the Manager
	// object. Once the channel is closed any operations on the Manager will
	// necessarily fail.
	Closed() <-chan struct{}
}

// Flow is the interface for a flow-controlled channel multiplexed on connection.
type Flow interface {
	io.ReadWriter

	// LocalEndpoint returns the local vanadium Endpoint
	LocalEndpoint() naming.Endpoint
	// RemoteEndpoint returns the remote vanadium Endpoint
	RemoteEndpoint() naming.Endpoint
	// LocalBlessings returns the blessings presented by the local end of the flow during authentication.
	LocalBlessings() security.Blessings
	// RemoteBlessings returns the blessings presented by the remote end of the flow during authentication.
	RemoteBlessings() security.Blessings
	// LocalDischarges returns the discharges presented by the local end of the flow during authentication.
	//
	// The discharges are organized in a map keyed by the discharge-identifier.
	LocalDischarges() map[string]security.Discharge
	// RemoteDischarges returns the discharges presented by the remote end of the flow during authentication.
	//
	// The discharges are organized in a map keyed by the discharge-identifier.
	RemoteDischarges() map[string]security.Discharge

	// Closed returns a channel that remains open until the flow has been closed or
	// the ctx to the Dail or Accept call used to create the flow has been cancelled.
	Closed() <-chan struct{}
}

// Listener is the interface for accepting Flows created by a remote process.
type Listener interface {
	// Listen listens on the protocol and address specified. This may be called
	// multiple times.
	Listen(protocol, address string) (naming.Endpoint, error)
	// Accept blocks until a new Flow has been initiated by a remote process.
	Accept(ctx *context.T) (Flow, error)
	// Closed returns a channel that remains open until the Listener has been closed.
	// i.e. the context provided to Manager.NewListener() has been cancelled.
	Closed() <-chan struct{}
}
