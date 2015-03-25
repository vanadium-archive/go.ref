// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build nacl

package websocket

import (
	"net"
	"net/url"
	"runtime/ppapi"
	"sync"
	"time"
)

// Ppapi instance which must be set before the Dial is called.
var PpapiInstance ppapi.Instance

func WebsocketConn(address string, ws *ppapi.WebsocketConn) net.Conn {
	return &wrappedConn{
		address: address,
		ws:      ws,
	}
}

type wrappedConn struct {
	address    string
	ws         *ppapi.WebsocketConn
	readLock   sync.Mutex
	writeLock  sync.Mutex
	currBuffer []byte
}

func Dial(protocol, address string, timeout time.Duration) (net.Conn, error) {
	inst := PpapiInstance
	u, err := url.Parse("ws://" + address)
	if err != nil {
		return nil, err
	}

	ws, err := inst.DialWebsocket(u.String())
	if err != nil {
		return nil, err
	}
	return WebsocketConn(address, ws), nil
}

func (c *wrappedConn) Read(b []byte) (int, error) {
	c.readLock.Lock()
	defer c.readLock.Unlock()

	var err error
	if len(c.currBuffer) == 0 {
		c.currBuffer, err = c.ws.ReceiveMessage()
		if err != nil {
			return 0, err
		}
	}

	n := copy(b, c.currBuffer)
	c.currBuffer = c.currBuffer[n:]
	return n, nil
}

func (c *wrappedConn) Write(b []byte) (int, error) {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if err := c.ws.SendMessage(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *wrappedConn) Close() error {
	return c.ws.Close()
}

func (c *wrappedConn) LocalAddr() net.Addr {
	return websocketAddr{s: c.address}
}

func (c *wrappedConn) RemoteAddr() net.Addr {
	return websocketAddr{s: c.address}
}

func (c *wrappedConn) SetDeadline(t time.Time) error {
	panic("SetDeadline not implemented.")
}

func (c *wrappedConn) SetReadDeadline(t time.Time) error {
	panic("SetReadDeadline not implemented.")
}

func (c *wrappedConn) SetWriteDeadline(t time.Time) error {
	panic("SetWriteDeadline not implemented.")
}

type websocketAddr struct {
	s string
}

func (websocketAddr) Network() string {
	return "ws"
}

func (w websocketAddr) String() string {
	return w.s
}
