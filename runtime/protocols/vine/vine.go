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
	v := &vine{
		behaviors: make(map[ConnKey]ConnBehavior),
		conns:     make(map[ConnKey]map[*conn]bool),
	}
	flow.RegisterProtocol("vine", v)
}

// Init initializes the vine server mounted under name using auth as its
// authorization policy and registers the vine protocol.
// The ctx returned from Init:
// (1) has localTag as the default localTag for dialers and acceptors.
// (2) has all addresses in the listenspec altered to listen on the vine protocol.
func Init(ctx *context.T, name string, auth security.Authorizer, localTag string) (*context.T, error) {
	v, _ := flow.RegisteredProtocol("vine")
	_, _, err := v23.WithNewServer(ctx, name, VineServer(v.(*vine)), auth)
	if err != nil {
		return nil, err
	}
	lspec := v23.GetListenSpec(ctx)
	for i, addr := range lspec.Addrs {
		lspec.Addrs[i].Protocol = "vine"
		lspec.Addrs[i].Address = createListeningAddress(addr.Protocol, addr.Address)
	}
	ctx = v23.WithListenSpec(ctx, lspec)
	ctx = WithLocalTag(ctx, localTag)
	return ctx, nil
}

// WithLocalTag returns a ctx that will have localTag as the default localTag for
// dialers and acceptors. This local tag will be inserted into any listening
// endpoints. i.e "net/address" -> "net/address/tag"
func WithLocalTag(ctx *context.T, tag string) *context.T {
	return context.WithValue(ctx, localTagKey{}, tag)
}

func getLocalTag(ctx *context.T) string {
	tag, _ := ctx.Value(localTagKey{}).(string)
	return tag
}

type localTagKey struct{}

type vine struct {
	mu sync.Mutex
	// behaviors maps ConnKeys to the corresponding connection's behavior.
	// If a ConnKey isn't in the map, the connection will be created under normal
	// network characteristics.
	behaviors map[ConnKey]ConnBehavior
	// conns stores all the vine connections. Sets of *conns are keyed by their
	// corresponding ConnKey
	conns map[ConnKey]map[*conn]bool
}

// SetBehaviors sets the policy that the accepting vine service's process
// will use on connections.
// behaviors is a map from server tag to the desired connection behavior.
// For example,
//   client.SetBehaviors(map[ConnKey]ConnBehavior{ConnKey{"foo", "bar"}, ConnBehavior{Reachable: false}})
// will cause all vine protocol dial calls from "foo" to "bar" to fail.
func (v *vine) SetBehaviors(ctx *context.T, call rpc.ServerCall, behaviors map[ConnKey]ConnBehavior) error {
	var toKill []flow.Conn
	v.mu.Lock()
	v.behaviors = behaviors
	for key, behavior := range behaviors {
		if !behavior.Reachable {
			for conn := range v.conns[key] {
				toKill = append(toKill, conn)
			}
		}
	}
	v.mu.Unlock()
	for _, conn := range toKill {
		conn.Close()
	}
	return nil
}

// Dial returns a flow.Conn to the specified address.
// protocol should always be "vine".
// address is of the form "network/ipaddress/tag". (i.e. A tcp server named
// "foo" could have the vine address "tcp/127.0.0.1:8888/foo"). We include the
// server tag in the address so that the vine protocol can have network
// characteristics based on the tag of the server rather than the less
// user-friendly host:port format.
func (v *vine) Dial(ctx *context.T, protocol, address string, timeout time.Duration) (flow.Conn, error) {
	n, a, remoteTag, baseProtocol, err := parseDialingAddress(ctx, address)
	if err != nil {
		return nil, err
	}
	localTag := getLocalTag(ctx)
	key := ConnKey{localTag, remoteTag}
	v.mu.Lock()
	behavior, ok := v.behaviors[key]
	v.mu.Unlock()
	// If the tag has been marked as not reachable, we can't create the connection.
	if ok && !behavior.Reachable {
		return nil, NewErrAddressNotReachable(ctx, a)
	}
	c, err := baseProtocol.Dial(ctx, n, a, timeout)
	if err != nil {
		return nil, err
	}
	laddr := c.LocalAddr()
	if err := sendLocalTag(ctx, c); err != nil {
		return nil, err
	}
	conn := &conn{
		base: c,
		addr: addr(createDialingAddress(laddr.Network(), laddr.String(), localTag)),
		key:  key,
		vine: v,
	}
	v.insertConn(conn)
	return conn, nil
}

// Resolve returns the resolved protocol and addresses. For example,
// if address is "net/domain/tag", it will return "net/ipaddress1/tag",
// "net/ipaddress2/tag", etc.
func (v *vine) Resolve(ctx *context.T, protocol, address string) (string, []string, error) {
	n, a, tag, baseProtocol, err := parseDialingAddress(ctx, address)
	if err != nil {
		return "", nil, err
	}
	n, resAddresses, err := baseProtocol.Resolve(ctx, n, a)
	if err != nil {
		return "", nil, err
	}
	addresses := make([]string, 0, len(resAddresses))
	for _, a := range resAddresses {
		addresses = append(addresses, createDialingAddress(n, a, tag))
	}
	return protocol, addresses, nil
}

// Listen returns a flow.Listener that the caller can accept flow.Conns on.
// protocol should always be "vine".
// address is of the form "network/ipaddress".
// The local tag set in ctx using WithLocalTag will be inserted into the listening
// address. i.e. "net/address" -> "net/address/tag"
func (v *vine) Listen(ctx *context.T, protocol, address string) (flow.Listener, error) {
	n, a, baseProtocol, err := parseListeningAddress(ctx, address)
	if err != nil {
		return nil, err
	}
	l, err := baseProtocol.Listen(ctx, n, a)
	if err != nil {
		return nil, err
	}
	laddr := l.Addr()
	localTag := getLocalTag(ctx)
	return &listener{
		base:     l,
		addr:     addr(createDialingAddress(laddr.Network(), laddr.String(), localTag)),
		vine:     v,
		localTag: localTag,
	}, nil
}

func (v *vine) insertConn(c *conn) {
	key := c.key
	v.mu.Lock()
	if m, ok := v.conns[key]; !ok {
		v.conns[key] = make(map[*conn]bool)
		v.conns[key][c] = true
	} else {
		m[c] = true
	}
	v.mu.Unlock()
}

func (v *vine) removeConn(c *conn) {
	key := c.key
	v.mu.Lock()
	if m, ok := v.conns[key]; ok {
		if _, ok := m[c]; ok {
			delete(m, c)
		}
	}
	v.mu.Unlock()
}

type conn struct {
	base flow.Conn
	addr addr
	key  ConnKey
	vine *vine
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
	c.vine.removeConn(c)
	return c.base.Close()
}

func (c *conn) LocalAddr() net.Addr {
	return c.addr
}

type listener struct {
	base     flow.Listener
	addr     addr
	vine     *vine
	localTag string
}

func (l *listener) Accept(ctx *context.T) (flow.Conn, error) {
	c, err := l.base.Accept(ctx)
	if err != nil {
		return nil, err
	}
	remoteTag, err := readRemoteTag(ctx, c)
	if err != nil {
		return nil, err
	}
	key := ConnKey{remoteTag, l.localTag}
	l.vine.mu.Lock()
	behavior, ok := l.vine.behaviors[key]
	l.vine.mu.Unlock()
	if ok && !behavior.Reachable {
		return nil, NewErrCantAcceptFromTag(ctx, remoteTag)
	}
	conn := &conn{
		base: c,
		addr: l.addr,
		key:  key,
		vine: l.vine,
	}
	l.vine.insertConn(conn)
	return conn, nil
}

func (l *listener) Addr() net.Addr {
	return l.addr
}

func (l *listener) Close() error {
	return l.base.Close()
}

type addr string

func (a addr) Network() string { return "vine" }
func (a addr) String() string  { return string(a) }

func sendLocalTag(ctx *context.T, c flow.Conn) error {
	tag := getLocalTag(ctx)
	_, err := c.WriteMsg([]byte(tag))
	return err
}

func readRemoteTag(ctx *context.T, c flow.Conn) (string, error) {
	msg, err := c.ReadMsg()
	if err != nil {
		return "", err
	}
	return string(msg), nil
}

// createDialingAddress creates a vine address of the form "network/address/tag".
func createDialingAddress(network, address, tag string) string {
	return strings.Join([]string{network, address, tag}, "/")
}

// createListeningAddress creates a vine address of the form "network/address".
func createListeningAddress(network, address string) string {
	return strings.Join([]string{network, address}, "/")
}

// parseDialingAddress takes vine addresses of the form "network/address/tag" and
// returns the embedded network, address, server tag, and the embedded network's
// registered flow.Protocol.
func parseDialingAddress(ctx *context.T, vaddress string) (network string, address string, tag string, p flow.Protocol, err error) {
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

// parseListeningAddress takes vine addresses of the form "network/address" and
// returns the embedded network, address, and the embedded network's
// registered flow.Protocol.
func parseListeningAddress(ctx *context.T, laddress string) (network string, address string, p flow.Protocol, err error) {
	parts := strings.SplitN(laddress, "/", 2)
	if len(parts) != 2 {
		return "", "", nil, NewErrInvalidAddress(ctx, laddress)
	}
	p, _ = flow.RegisteredProtocol(parts[0])
	if p == nil {
		return "", "", nil, NewErrNoRegisteredProtocol(ctx, parts[0])
	}
	return parts[0], parts[1], p, nil
}
