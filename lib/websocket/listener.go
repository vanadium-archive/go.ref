// +build !nacl

package websocket

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/runtimes/google/lib/upcqueue"
)

var errListenerIsClosed = errors.New("Listener has been Closed")

// We picked 0xFF because it's obviously outside the range of ASCII,
// and is completely unused in UTF-8.
const BinaryMagicByte byte = 0xFF

const bufferSize int = 4096

// A listener that is able to handle either raw tcp request or websocket requests.
// The result of Accept is is a net.Conn interface.
type wsTCPListener struct {
	// The queue of net.Conn to be returned by Accept.
	q *upcqueue.T

	// The queue for the http listener when we detect an http request.
	httpQ *upcqueue.T

	// The underlying listener.
	netLn    net.Listener
	wsServer http.Server

	netLoop sync.WaitGroup
	wsLoop  sync.WaitGroup
}

// bufferedConn is used to allow us to Peek at the first byte to see if it
// is the magic byte used by veyron tcp requests.  Other than that it behaves
// like a normal net.Conn.
type bufferedConn struct {
	net.Conn
	// TODO(bjornick): Remove this buffering because we have way too much
	// buffering anyway.  We really only need to buffer the first byte.
	r *bufio.Reader
}

func newBufferedConn(c net.Conn) bufferedConn {
	return bufferedConn{Conn: c, r: bufio.NewReaderSize(c, bufferSize)}
}

func (c *bufferedConn) Peek(n int) ([]byte, error) {
	return c.r.Peek(n)
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

// queueListener is a listener that returns connections that are in q.
type queueListener struct {
	q *upcqueue.T
	// ln is needed to implement Close and Addr
	ln net.Listener
}

func (l *queueListener) Accept() (net.Conn, error) {
	item, err := l.q.Get(nil)
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, errListenerIsClosed
	case err != nil:
		return nil, fmt.Errorf("Accept failed: %v", err)
	default:
		return item.(net.Conn), nil
	}
}

func (l *queueListener) Close() error {
	l.q.Shutdown()
	return l.ln.Close()
}

func (l *queueListener) Addr() net.Addr {
	return l.ln.Addr()
}

func NewListener(netLn net.Listener) (net.Listener, error) {
	ln := &wsTCPListener{
		q:     upcqueue.New(),
		httpQ: upcqueue.New(),
		netLn: netLn,
	}
	ln.netLoop.Add(1)
	go ln.netAcceptLoop()
	httpListener := &queueListener{
		q:  ln.httpQ,
		ln: ln,
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		defer ln.wsLoop.Done()
		if r.Method != "GET" {
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		ws, err := websocket.Upgrade(w, r, nil, bufferSize, bufferSize)
		if _, ok := err.(websocket.HandshakeError); ok {
			http.Error(w, "Not a websocket handshake", 400)
			vlog.Errorf("Rejected a non-websocket request: %v", err)
			return
		} else if err != nil {
			http.Error(w, "Internal Error", 500)
			vlog.Errorf("Rejected a non-websocket request: %v", err)
			return
		}
		conn := WebsocketConn(ws)
		if err := ln.q.Put(conn); err != nil {
			vlog.VI(1).Infof("Shutting down conn from %s (local address: %s) as Put failed: %v", ws.RemoteAddr(), ws.LocalAddr(), err)
			ws.Close()
			return
		}
	}
	ln.wsServer = http.Server{
		Handler: http.HandlerFunc(handler),
	}
	go ln.wsServer.Serve(httpListener)
	return ln, nil
}

func (ln *wsTCPListener) netAcceptLoop() {
	defer ln.Close()
	defer ln.netLoop.Done()
	for {
		conn, err := ln.netLn.Accept()
		if err != nil {
			vlog.VI(1).Infof("Exiting netAcceptLoop: net.Listener.Accept() failed on %v with %v", ln.netLn, err)
			return
		}
		vlog.VI(1).Infof("New net.Conn accepted from %s (local address: %s)", conn.RemoteAddr(), conn.LocalAddr())
		bc := newBufferedConn(conn)
		magic, err := bc.Peek(1)
		if err != nil {
			vlog.VI(1).Infof("Shutting down conn from %s (local address: %s) as the magic byte failed to be read: %v", conn.RemoteAddr(), conn.LocalAddr(), err)
			bc.Close()
			continue
		}

		vlog.VI(1).Info("Got a connection from %s (local address: %s)", conn.RemoteAddr(), conn.LocalAddr())
		// Check to see if it is a regular connection or a http connection.
		if magic[0] == BinaryMagicByte {
			if _, err := bc.r.ReadByte(); err != nil {
				vlog.VI(1).Infof("Shutting down conn from %s (local address: %s), could read past the magic byte: %v", conn.RemoteAddr(), conn.LocalAddr(), err)
				bc.Close()
				continue
			}
			if err := ln.q.Put(&bc); err != nil {
				vlog.VI(1).Infof("Shutting down conn from %s (local address: %s) as Put failed in vifLoop: %v", conn.RemoteAddr(), conn.LocalAddr(), err)
				bc.Close()
				continue
			}
			continue
		}

		ln.wsLoop.Add(1)
		if err := ln.httpQ.Put(&bc); err != nil {
			ln.wsLoop.Done()
			vlog.VI(1).Infof("Shutting down conn from %s (local address: %s) as Put failed in vifLoop: %v", conn.RemoteAddr(), conn.LocalAddr(), err)
			bc.Close()
			continue
		}
	}
}

func (ln *wsTCPListener) Accept() (net.Conn, error) {
	item, err := ln.q.Get(nil)
	switch {
	case err == upcqueue.ErrQueueIsClosed:
		return nil, errListenerIsClosed
	case err != nil:
		return nil, fmt.Errorf("Accept failed: %v", err)
	default:
		return item.(net.Conn), nil
	}
}

func (ln *wsTCPListener) Close() error {
	addr := ln.netLn.Addr()
	err := ln.netLn.Close()
	vlog.VI(1).Infof("Closed net.Listener on (%q, %q): %v", addr.Network(), addr, err)
	ln.httpQ.Shutdown()
	ln.netLoop.Wait()
	ln.wsLoop.Wait()
	// q has to be shutdown after the netAcceptLoop finishes because that loop
	// could be in the process of accepting a websocket connection.  The ordering
	// relative to wsLoop is not really relevant because the wsLoop counter wil
	// decrement every time there a websocket connection has been handled and does
	// not block on gets from q.
	ln.q.Shutdown()
	vlog.VI(3).Infof("Close stream.wsTCPListener %s", ln)
	return nil
}

func (ln *wsTCPListener) Addr() net.Addr {
	return ln.netLn.Addr()
}
