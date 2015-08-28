// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xserver provides an alternate RPC server API with the goal of
// being simpler to use and understand.
package xrpc

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

type server struct {
	s rpc.Server
}

// NewServer creates a new Server instance to serve a service object.
//
// The server will listen for network connections as specified by the
// ListenSpec attached to ctx. Depending on your RuntimeFactory, 'roaming'
// support may be enabled. In this mode the server will listen for
// changes in the network configuration using a Stream created on the
// supplied Publisher and change the set of Endpoints it publishes to
// the mount table accordingly.
//
// The server associates object with name by publishing the address of
// this server in the namespace under the supplied name and using
// authorizer to authorize access to it. RPCs invoked on the supplied
// name will be delivered to methods implemented by the supplied
// object.  Reflection is used to match requests to the object's
// method set.  As a special-case, if the object implements the
// Invoker interface, the Invoker is used to invoke methods directly,
// without reflection.  If name is an empty string, no attempt will
// made to publish.
func NewServer(ctx *context.T, name string, object interface{}, auth security.Authorizer, opts ...rpc.ServerOpt) (rpc.XServer, error) {
	s, err := v23.NewServer(ctx, opts...)
	if err != nil {
		return nil, err
	}
	if _, err = s.Listen(v23.GetListenSpec(ctx)); err != nil {
		s.Stop()
		return nil, err
	}
	if err = s.Serve(name, object, auth); err != nil {
		s.Stop()
		return nil, err
	}
	return &server{s: s}, nil
}

// NewDispatchingServer creates a new Server instance to serve a given dispatcher.
//
// The server will listen for network connections as specified by the
// ListenSpec attached to ctx. Depending on your RuntimeFactory, 'roaming'
// support may be enabled. In this mode the server will listen for
// changes in the network configuration using a Stream created on the
// supplied Publisher and change the set of Endpoints it publishes to
// the mount table accordingly.
//
// The server associates dispatcher with the portion of the namespace
// for which name is a prefix by publishing the address of this server
// to the namespace under the supplied name. If name is an empty
// string, no attempt will made to publish. RPCs invoked on the
// supplied name will be delivered to the supplied Dispatcher's Lookup
// method which will in turn return the object and security.Authorizer
// used to serve the actual RPC call.  If name is an empty string, no
// attempt will made to publish that name to a mount table.
func NewDispatchingServer(ctx *context.T, name string, disp rpc.Dispatcher, opts ...rpc.ServerOpt) (rpc.XServer, error) {
	s, err := v23.NewServer(ctx, opts...)
	if err != nil {
		return nil, err
	}
	if _, err = s.Listen(v23.GetListenSpec(ctx)); err != nil {
		return nil, err
	}
	if err = s.ServeDispatcher(name, disp); err != nil {
		return nil, err
	}
	return &server{s: s}, nil
}

// AddName adds the specified name to the mount table for this server.
// AddName may be called multiple times.
func (s *server) AddName(name string) error {
	return s.s.AddName(name)
}

// RemoveName removes the specified name from the mount table.
// RemoveName may be called multiple times.
func (s *server) RemoveName(name string) {
	s.s.RemoveName(name)
}

// Status returns the current status of the server, see ServerStatus
// for details.
func (s *server) Status() rpc.ServerStatus {
	return s.s.Status()
}

// WatchNetwork registers a channel over which NetworkChange's will
// be sent. The Server will not block sending data over this channel
// and hence change events may be lost if the caller doesn't ensure
// there is sufficient buffering in the channel.
func (s *server) WatchNetwork(ch chan<- rpc.NetworkChange) {
	s.s.WatchNetwork(ch)
}

// UnwatchNetwork unregisters a channel previously registered using
// WatchNetwork.
func (s *server) UnwatchNetwork(ch chan<- rpc.NetworkChange) {
	s.s.UnwatchNetwork(ch)
}

// Stop gracefully stops all services on this Server.  New calls are
// rejected, but any in-flight calls are allowed to complete.  All
// published mountpoints are unmounted.  This call waits for this
// process to complete, and returns once the server has been shut down.
func (s *server) Stop() error {
	return s.s.Stop()
}
