// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mocknet implements a mock net.Conn that can simulate a variety of
// network errors and/or be used for tracing.
package mocknet

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/naming"

	"v.io/x/ref/profiles/internal/lib/iobuf"
	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
	"v.io/x/ref/profiles/internal/rpc/stream/message"
)

// TODO(cnicolaou): consider extending Dialer/Listener API to include a cipher
// to allow access to encrypted data.

type Mode int

const (
	Trace Mode = iota // Log the sizes of each read/write call
	Close             // Close the connection after a specified #bytes are read/written
	Drop              // Drop byes as per a policy specified in opts
	// Close the connection based on the Vanadium protocol message
	V23CloseAtMessage
)

type Opts struct {
	// The underlying network protocol to use, e.g. "tcp", defaults to tcp.
	UnderlyingProtocol string

	// The mode to operate under.
	Mode Mode

	// Buffers to store the transmit and receive message sizes when
	// in Trace mode.
	Tx, Rx chan int

	// The number of rx and tx bytes respectively to be seen before the
	// connection is closed when in Close mode.
	RxCloseAt, TxCloseAt int

	// TXDropAfter is called to obtain the number of tx bytes to be sent
	// before dropping the rest of the data passed to that write call. The
	// number of bytes returned by TxDroptAfter will always be written,
	// but the number of bytes dropped is unspecified since it depends
	// on the size of the buffer passed to that write call. TxDropAfter
	// will be called again after each drop and the current count of
	// byte sent reset to zero.
	TxDropAfter func() (pos int)

	// V23MessageMatcher should return true if the connection
	// should be closed. read is true for a read call, false for a write,
	// and msg is a copy of the message just received or to be sent.
	V23MessageMatcher func(read bool, msg message.T) bool
}

// DialerWithOpts is intended for use with rpc.RegisterProtocol via
// a closure:
//
//  dialer := func(network, address string, timeout time.Duration) (net.Conn, error) {
//	    return mocknet.DialerWithOpts(mocknet.Opts{UnderlyingProtocol:"tcp"}, network, address, timeout)
//  }
// rpc.RegisterProtocol("brkDial", dialer, net.Listen)
//
func DialerWithOpts(opts Opts, network, address string, timeout time.Duration) (net.Conn, error) {
	protocol := opts.UnderlyingProtocol
	if len(protocol) == 0 {
		protocol = "tcp"
	}
	c, err := net.DialTimeout(protocol, address, timeout)
	if err != nil {
		return nil, err
	}
	return newMockConn(opts, c), nil
}

// ListenerWithOpts is intended for use with rpc.RegisterProtocol via
// a closure as per DialerWithOpts.
func ListenerWithOpts(opts Opts, network, laddr string) (net.Listener, error) {
	protocol := opts.UnderlyingProtocol
	if len(protocol) == 0 {
		protocol = "tcp"
	}
	ln, err := net.Listen(protocol, laddr)
	if err != nil {
		return nil, err
	}
	return &listener{opts, ln}, nil
}

func newMockConn(opts Opts, c net.Conn) net.Conn {
	switch opts.Mode {
	case Trace:
		return &traceConn{
			conn: c,
			rx:   opts.Rx,
			tx:   opts.Tx}
	case Close:
		return &closeConn{
			conn:      c,
			rxCloseAt: opts.RxCloseAt,
			txCloseAt: opts.TxCloseAt,
		}
	case Drop:
		return &dropConn{
			conn:        c,
			opts:        opts,
			txDropAfter: opts.TxDropAfter(),
		}
	case V23CloseAtMessage:
		return &v23Conn{
			conn:   c,
			opts:   opts,
			cipher: crypto.NewDisabledControlCipher(&crypto.NullControlCipher{}),
			pool:   iobuf.NewPool(1024),
		}
	}
	return nil
}

type dropConn struct {
	sync.Mutex
	opts        Opts
	conn        net.Conn
	tx          int
	txDropAfter int
}

func (c *dropConn) Read(b []byte) (n int, err error) {
	return c.conn.Read(b)
}

func (c *dropConn) Write(b []byte) (n int, err error) {
	c.Lock()
	defer c.Unlock()
	dropped := false
	if c.tx+len(b) >= c.txDropAfter {
		b = b[0 : c.txDropAfter-c.tx]
		c.txDropAfter = c.opts.TxDropAfter()
		dropped = true
	}
	n, err = c.conn.Write(b)
	if dropped {
		c.tx = 0
	} else {
		c.tx += n
	}
	return
}

func (c *dropConn) Close() error        { return c.conn.Close() }
func (c *dropConn) LocalAddr() net.Addr { return c.conn.LocalAddr() }
func (c *dropConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}
func (c *dropConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}
func (c *dropConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}
func (c *dropConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

type closeConn struct {
	sync.Mutex
	conn                 net.Conn
	rx, tx               int
	rxCloseAt, txCloseAt int
	closed               bool
}

func (c *closeConn) Read(b []byte) (n int, err error) {
	c.Lock()
	defer c.Unlock()
	n = len(b)
	if c.rx+n >= c.rxCloseAt {
		n = c.rxCloseAt - c.rx
	}
	b = b[:n]
	n, err = c.conn.Read(b[:n])
	c.rx += n
	if c.rx == c.rxCloseAt {
		c.conn.Close()
	}
	return
}

func (c *closeConn) Write(b []byte) (n int, err error) {
	c.Lock()
	defer c.Unlock()
	n = len(b)
	if c.tx+n >= c.txCloseAt {
		n = c.txCloseAt - c.tx
	}
	n, err = c.conn.Write(b[:n])
	c.tx += n
	if c.tx == c.txCloseAt {
		c.conn.Close()
	}
	return
}

func (c *closeConn) Close() error        { return c.conn.Close() }
func (c *closeConn) LocalAddr() net.Addr { return c.conn.LocalAddr() }
func (c *closeConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}
func (c *closeConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}
func (c *closeConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}
func (c *closeConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

type traceConn struct {
	conn   net.Conn
	tx, rx chan int
}

func (c *traceConn) Read(b []byte) (n int, err error) {
	n, err = c.conn.Read(b)
	c.rx <- n
	return n, err
}

func (c *traceConn) Write(b []byte) (n int, err error) {
	n, err = c.conn.Write(b)
	c.tx <- n
	return
}

func (c *traceConn) Close() error {
	c.rx <- -1
	c.tx <- -1
	return c.conn.Close()
}

func (c *traceConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *traceConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
func (c *traceConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}
func (c *traceConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}
func (c *traceConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

type v23Conn struct {
	conn   net.Conn
	opts   Opts
	cipher crypto.ControlCipher
	pool   *iobuf.Pool
}

func (c *v23Conn) Read(b []byte) (n int, err error) {
	n, err = c.conn.Read(b)
	buf := iobuf.NewReader(c.pool, bytes.NewBuffer(b[:n]))
	msg, err := message.ReadFrom(buf, c.cipher)
	if err == nil && c.opts.V23MessageMatcher(true, msg) {
		c.conn.Close()
		return 0, io.EOF
	}
	return n, err
}

func (c *v23Conn) Write(b []byte) (n int, err error) {
	buf := iobuf.NewReader(c.pool, bytes.NewBuffer(b))
	msg, err := message.ReadFrom(buf, c.cipher)
	if err == nil && c.opts.V23MessageMatcher(false, msg) {
		c.conn.Close()
		return 0, io.EOF
	}
	return c.conn.Write(b)
}

func (c *v23Conn) Close() error {
	return c.conn.Close()
}

func (c *v23Conn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *v23Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
func (c *v23Conn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}
func (c *v23Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}
func (c *v23Conn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// listener is a wrapper around net.Listener.
type listener struct {
	opts  Opts
	netLn net.Listener
}

func (ln *listener) Accept() (net.Conn, error) {
	c, err := ln.netLn.Accept()
	if err != nil {
		return nil, err
	}
	return newMockConn(ln.opts, c), nil
}

func (ln *listener) Close() error {
	return ln.netLn.Close()
}

func (ln *listener) Addr() net.Addr {
	return ln.netLn.Addr()
}

func RewriteEndpointProtocol(ep string, protocol string) (naming.Endpoint, error) {
	n, err := v23.NewEndpoint(ep)
	if err != nil {
		return nil, err
	}
	iep, ok := n.(*inaming.Endpoint)
	if !ok {
		return nil, fmt.Errorf("failed to convert %T to inaming.Endpoint", n)
	}
	iep.Protocol = protocol
	return iep, nil
}
