// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tcp

import (
	"net"
	"time"

	"v.io/v23/context"
	"v.io/v23/flow"

	"v.io/x/ref/runtime/internal/lib/framer"
	"v.io/x/ref/runtime/internal/lib/tcputil"
)

func init() {
	tcp := tcpProtocol{}
	flow.RegisterProtocol("tcp", tcp, "tcp4", "tcp6")
	flow.RegisterProtocol("tcp4", tcp)
	flow.RegisterProtocol("tcp6", tcp)
}

type tcpProtocol struct{}

// Dial dials a net.Conn to a the specific address and adds framing to the connection.
func (tcpProtocol) Dial(ctx *context.T, network, address string, timeout time.Duration) (flow.MsgReadWriteCloser, error) {
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, err
	}
	if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
		return nil, err
	}
	return framer.New(conn), nil
}

// Resolve performs a DNS resolution on the provided network and address.
func (tcpProtocol) Resolve(ctx *context.T, network, address string) (string, string, error) {
	tcpAddr, err := net.ResolveTCPAddr(network, address)
	if err != nil {
		return "", "", err
	}
	return tcpAddr.Network(), tcpAddr.String(), nil
}

// Listen returns a listener that sets KeepAlive on all accepted connections.
// Connections returned from the listener will be framed.
func (tcpProtocol) Listen(ctx *context.T, network, address string) (flow.MsgListener, error) {
	ln, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return &tcpListener{ln}, nil
}

// tcpListener is a wrapper around net.Listener that sets KeepAlive on all
// accepted connections and returns framed flow.MsgReadWriteClosers.
type tcpListener struct {
	netLn net.Listener
}

func (ln *tcpListener) Accept(ctx *context.T) (flow.MsgReadWriteCloser, error) {
	conn, err := ln.netLn.Accept()
	if err != nil {
		return nil, err
	}
	if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
		return nil, err
	}
	return framer.New(conn), nil
}

func (ln *tcpListener) Addr() net.Addr {
	return ln.netLn.Addr()
}
