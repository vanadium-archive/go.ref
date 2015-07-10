// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !nacl

package websocket

import (
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"v.io/x/ref/internal/logger"
	"v.io/x/ref/runtime/internal/lib/tcputil"
)

var errListenerIsClosed = errors.New("Listener has been Closed")

const (
	bufferSize         = 4096
	classificationTime = 10 * time.Second
)

// A listener that is able to handle either raw tcp or websocket requests.
type wsTCPListener struct {
	closed bool // GUARDED_BY(mu)
	mu     sync.Mutex

	acceptQ chan interface{} // net.Conn or error returned by netLn.Accept
	httpQ   chan net.Conn    // Candidates for websocket upgrades before being added to acceptQ
	netLn   net.Listener     // The underlying listener
	httpReq sync.WaitGroup   // Number of active HTTP requests
	hybrid  bool             // true if running in 'hybrid' mode
}

// chanListener implements net.Listener, with Accept reading from c.
type chanListener struct {
	net.Listener // Embedded for all other net.Listener functionality.
	c            <-chan net.Conn
}

func (ln *chanListener) Accept() (net.Conn, error) {
	conn, ok := <-ln.c
	if !ok {
		return nil, errListenerIsClosed
	}
	return conn, nil
}

func Listener(protocol, address string) (net.Listener, error) {
	return listener(protocol, address, false)
}

func listener(protocol, address string, hybrid bool) (net.Listener, error) {
	netLn, err := net.Listen(mapWebSocketToTCP[protocol], address)
	if err != nil {
		return nil, err
	}
	ln := &wsTCPListener{
		acceptQ: make(chan interface{}),
		httpQ:   make(chan net.Conn),
		netLn:   netLn,
		hybrid:  hybrid,
	}
	go ln.netAcceptLoop()
	httpsrv := http.Server{Handler: ln}
	go httpsrv.Serve(&chanListener{Listener: ln, c: ln.httpQ})
	return ln, nil
}

func (ln *wsTCPListener) Accept() (net.Conn, error) {
	for {
		item, ok := <-ln.acceptQ
		if !ok {
			return nil, errListenerIsClosed
		}
		switch v := item.(type) {
		case net.Conn:
			return v, nil
		case error:
			return nil, v
		default:
			logger.Global().Errorf("Unexpected type %T in channel (%v)", v, v)
		}
	}
}

func (ln *wsTCPListener) Close() error {
	ln.mu.Lock()
	if ln.closed {
		ln.mu.Unlock()
		return errListenerIsClosed
	}
	ln.closed = true
	ln.mu.Unlock()
	addr := ln.netLn.Addr()
	err := ln.netLn.Close()
	logger.Global().VI(1).Infof("Closed net.Listener on (%q, %q): %v", addr.Network(), addr, err)
	// netAcceptLoop might be trying to push new TCP connections that
	// arrived while the listener was being closed. Drop those.
	drainChan(ln.acceptQ)
	return nil
}

func (ln *wsTCPListener) netAcceptLoop() {
	var classifications sync.WaitGroup
	defer func() {
		// This sequence of closures is carefully curated based on the
		// following invariants:
		// (1) All calls to ln.classify have been added to classifications.
		// (2) Only ln.classify sends on ln.httpQ
		// (3) All calls to ln.ServeHTTP have been added to ln.httpReq
		// (4) Sends on ln.acceptQ are done by either ln.netAcceptLoop ro ln.ServeHTTP
		classifications.Wait()
		close(ln.httpQ)
		ln.httpReq.Wait()
		close(ln.acceptQ)
	}()
	for {
		conn, err := ln.netLn.Accept()
		if err != nil {
			// If the listener has been closed, quit - otherwise
			// propagate the error.
			ln.mu.Lock()
			closed := ln.closed
			ln.mu.Unlock()
			if closed {
				return
			}
			ln.acceptQ <- err
			continue
		}
		logger.Global().VI(1).Infof("New net.Conn accepted from %s (local address: %s)", conn.RemoteAddr(), conn.LocalAddr())
		if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
			logger.Global().Errorf("Failed to enable TCP keep alive: %v", err)
		}
		classifications.Add(1)
		go ln.classify(conn, &classifications)
	}
}

// classify classifies conn as either an HTTP connection or a non-HTTP one.
//
// If the latter, then the connection is added to ln.acceptQ.
// If the former, then the connection is queued up for a websocket upgrade.
func (ln *wsTCPListener) classify(conn net.Conn, done *sync.WaitGroup) {
	defer done.Done()
	isHTTP := true
	if ln.hybrid {
		conn.SetReadDeadline(time.Now().Add(classificationTime))
		defer conn.SetReadDeadline(time.Time{})
		var magic [1]byte
		n, err := io.ReadFull(conn, magic[:])
		if err != nil {
			// Unable to classify, ignore this connection.
			logger.Global().VI(1).Infof("Shutting down connection from %v since the magic bytes could not be read: %v", conn.RemoteAddr(), err)
			conn.Close()
			return
		}
		conn = &hybridConn{conn: conn, buffered: magic[:n]}
		isHTTP = magic[0] == 'G'
	}
	if isHTTP {
		ln.httpReq.Add(1)
		ln.httpQ <- conn
		return
	}
	ln.acceptQ <- conn
}

func (ln *wsTCPListener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer ln.httpReq.Done()
	if r.Method != "GET" {
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	ws, err := websocket.Upgrade(w, r, nil, bufferSize, bufferSize)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Not a websocket handshake", http.StatusBadRequest)
		logger.Global().Errorf("Rejected a non-websocket request: %v", err)
		return
	}
	if err != nil {
		http.Error(w, "Internal Error", http.StatusInternalServerError)
		logger.Global().Errorf("Rejected a non-websocket request: %v", err)
		return
	}
	ln.acceptQ <- WebsocketConn(ws)
}

type addr struct{ n, a string }

func (a addr) Network() string {
	return a.n
}

func (a addr) String() string {
	return a.a
}

func (ln *wsTCPListener) Addr() net.Addr {
	protocol := "ws"
	if ln.hybrid {
		protocol = "wsh"
	}
	return addr{protocol, ln.netLn.Addr().String()}
}

func drainChan(c <-chan interface{}) {
	for {
		item, ok := <-c
		if !ok {
			return
		}
		if conn, ok := item.(net.Conn); ok {
			conn.Close()
		}
	}
}
