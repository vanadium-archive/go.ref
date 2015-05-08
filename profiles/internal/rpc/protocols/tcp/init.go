// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tcp

import (
	"net"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23/rpc"

	"v.io/x/ref/profiles/internal/lib/tcputil"
)

func init() {
	rpc.RegisterProtocol("tcp", tcpDial, tcpResolve, tcpListen, "tcp4", "tcp6")
	rpc.RegisterProtocol("tcp4", tcpDial, tcpResolve, tcpListen)
	rpc.RegisterProtocol("tcp6", tcpDial, tcpResolve, tcpListen)
}

func tcpDial(network, address string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, err
	}
	if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
		return nil, err
	}
	return conn, nil
}

// tcpResolve performs a DNS resolution on the provided network and address.
func tcpResolve(network, address string) (string, string, error) {
	tcpAddr, err := net.ResolveTCPAddr(network, address)
	if err != nil {
		return "", "", err
	}
	return tcpAddr.Network(), tcpAddr.String(), nil
}

// tcpListen returns a listener that sets KeepAlive on all accepted connections.
func tcpListen(network, laddr string) (net.Listener, error) {
	ln, err := net.Listen(network, laddr)
	if err != nil {
		return nil, err
	}
	return &tcpListener{ln}, nil
}

// tcpListener is a wrapper around net.Listener that sets KeepAlive on all
// accepted connections.
type tcpListener struct {
	netLn net.Listener
}

func (ln *tcpListener) Accept() (net.Conn, error) {
	conn, err := ln.netLn.Accept()
	if err != nil {
		return nil, err
	}
	if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
		vlog.Errorf("Failed to enable TCP keep alive: %v", err)
	}
	return conn, nil
}

func (ln *tcpListener) Close() error {
	return ln.netLn.Close()
}

func (ln *tcpListener) Addr() net.Addr {
	return ln.netLn.Addr()
}
