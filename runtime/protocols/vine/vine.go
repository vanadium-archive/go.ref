// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package vine contains Vanadium's Implementation of Network Emulation (VINE).
// VINE provides the ability to dynamically specific a network topology
// (e.g. A can reach B, but A cannot reach C) with various network
// charcteristics (e.g. A can reach B with latency of 500ms).
// This can be useful for testing Vanadium applications under unpredictable and
// unfriendly network conditions.
package vine

import (
	"net"
	"strings"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

func init() {
	v := &vine{outgoing: make(map[string]ConnBehavior)}
	flow.RegisterProtocol("vine", v)
}

// Vine initializes the vine server mounted under tag using auth as its
// authorization policy and registers the vine protocol.
func Init(ctx *context.T, tag string, auth security.Authorizer) error {
	v, _ := flow.RegisteredProtocol("vine")
	_, _, err := v23.WithNewServer(ctx, tag, VineServer(v.(*vine)), auth)
	return err
}

// CreateVineAddress creates a vine address of the form "network/address/tag".
func CreateVineAddress(network, address, tag string) string {
	return strings.Join([]string{network, address, tag}, "/")
}

type vine struct {
	mu sync.Mutex
	// outgoing maps outgoing server tag to the corresponding connection behavior.
	// If a tag isn't in the map, the connection will be created under normal
	// network characteristics.
	outgoing map[string]ConnBehavior
}

// SetOutgoingBehaviors sets the policy that the accepting vine service's process
// will use on outgoing connections.
// outgoing is a map from server tag to the desired connection behavior.
// For example,
//   client.SetOutgoingBehaviors(map[string]ConnBehavior{"foo", ConnBehavior{Reachable: false}})
// will cause all vine protocol dial calls on the accepting vine service's
// process to fail when dialing out to tag "foo".
func (v *vine) SetOutgoingBehaviors(ctx *context.T, call rpc.ServerCall, outgoing map[string]ConnBehavior) error {
	v.mu.Lock()
	v.outgoing = outgoing
	v.mu.Unlock()
	return nil
}

// Dial returns a flow.Conn to the specified address.
// protocol should always be "vine".
// address is of the form "network/ipaddress/tag". (i.e. A tcp server tagd
// "foo" could have the vine address "tcp/127.0.0.1:8888/foo"). We include the
// server tag in the address so that the vine protocol can have network
// characteristics based on the tag of the server rather than the less
// user-friendly host:port format.
func (v *vine) Dial(ctx *context.T, protocol, address string, timeout time.Duration) (flow.Conn, error) {
	n, a, tag, baseProtocol, err := parseVineAddress(ctx, address)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	behavior, ok := v.outgoing[tag]
	v.mu.Unlock()
	// If the tag has been marked as not reachable, we can't create the connection.
	if ok && !behavior.Reachable {
		return nil, NewErrAddressNotReachable(ctx, a)
	}
	c, err := baseProtocol.Dial(ctx, n, a, timeout)
	if err != nil {
		return nil, err
	}
	return &conn{base: c, addr: addr(address)}, nil
}

// Resolve returns the resolved protocol and addresses. For example,
// if address is "tag/domain/tag", it will return "tag/ipaddress1/tag",
// "tag/ipaddress2/tag", etc.
func (v *vine) Resolve(ctx *context.T, protocol, address string) (string, []string, error) {
	n, a, tag, baseProtocol, err := parseVineAddress(ctx, address)
	if err != nil {
		return "", nil, err
	}
	n, resAddresses, err := baseProtocol.Resolve(ctx, n, a)
	if err != nil {
		return "", nil, err
	}
	addresses := make([]string, 0, len(resAddresses))
	for _, a := range resAddresses {
		addresses = append(addresses, CreateVineAddress(n, a, tag))
	}
	return protocol, addresses, nil
}

// Listen returns a flow.Listener that the caller can accept flow.Conns on.
// protocol should always be "vine".
// address is of the form "network/ipaddress/tag". (i.e. A tcp server tagd
// "foo" could have the vine address "tcp/127.0.0.1:8888/foo"). The tag only
// makes sense in the address in the context of Dial, but we include it here
// for consistency.
func (v *vine) Listen(ctx *context.T, protocol, address string) (flow.Listener, error) {
	n, a, tag, baseProtocol, err := parseVineAddress(ctx, address)
	if err != nil {
		return nil, err
	}
	l, err := baseProtocol.Listen(ctx, n, a)
	if err != nil {
		return nil, err
	}
	laddr := l.Addr()
	return &listener{base: l, addr: addr(CreateVineAddress(laddr.Network(), laddr.String(), tag))}, nil
}

type conn struct {
	base flow.Conn
	addr addr
}

// WriteMsg wraps the base flow.Conn's WriteMsg method to allow injection of
// various network characteristics.
func (c *conn) WriteMsg(data ...[]byte) (int, error) {
	return c.base.WriteMsg(data...)
}

// ReadMsg wraps the base flow.Conn's ReadMsg method to allow injection of
// various network characteristics.
func (c *conn) ReadMsg() ([]byte, error) {
	return c.base.ReadMsg()
}

func (c *conn) Close() error {
	return c.base.Close()
}

func (c *conn) LocalAddr() net.Addr {
	return c.addr
}

type listener struct {
	base flow.Listener
	addr addr
}

func (l *listener) Accept(ctx *context.T) (flow.Conn, error) {
	c, err := l.base.Accept(ctx)
	if err != nil {
		return nil, err
	}
	return &conn{base: c, addr: l.addr}, nil
}

func (l *listener) Addr() net.Addr {
	return l.addr
}

func (l *listener) Close() error {
	return l.base.Close()
}

// parseVineAddress takes vine addresses of the form "network/address/tag" and
// returns the embedded network, address, server tag, and the embedded network's
// registered flow.Protocol.
func parseVineAddress(ctx *context.T, vaddress string) (network string, address string, tag string, p flow.Protocol, err error) {
	parts := strings.SplitN(vaddress, "/", 3)
	if len(parts) != 3 {
		return "", "", "", nil, NewErrInvalidAddress(ctx, vaddress)
	}
	p, _ = flow.RegisteredProtocol(parts[0])
	if p == nil {
		return "", "", "", nil, NewErrNoRegisteredProtocol(ctx, parts[0])
	}
	return parts[0], parts[1], parts[2], p, nil
}

type addr string

func (a addr) Network() string { return "vine" }
func (a addr) String() string  { return string(a) }
