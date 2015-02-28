// +build !nacl

package websocket

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/tcputil"
	"v.io/x/ref/runtimes/google/lib/upcqueue"
)

var errListenerIsClosed = errors.New("Listener has been Closed")

const bufferSize int = 4096

// A listener that is able to handle either raw tcp request or websocket requests.
// The result of Accept is is a net.Conn interface.
type wsTCPListener struct {
	closed bool
	mu     sync.Mutex // Guards closed

	// The queue of net.Conn to be returned by Accept.
	q *upcqueue.T

	// The queue for the http listener when we detect an http request.
	httpQ *upcqueue.T

	// The underlying listener.
	netLn    net.Listener
	wsServer http.Server

	netLoop sync.WaitGroup
	wsLoop  sync.WaitGroup

	hybrid bool // true if we're running in 'hybrid' mode
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

func Listener(protocol, address string) (net.Listener, error) {
	return listener(protocol, address, false)
}

func listener(protocol, address string, hybrid bool) (net.Listener, error) {
	tcp := mapWebSocketToTCP[protocol]
	netLn, err := net.Listen(tcp, address)
	if err != nil {
		return nil, err
	}
	ln := &wsTCPListener{
		q:      upcqueue.New(),
		httpQ:  upcqueue.New(),
		netLn:  netLn,
		hybrid: hybrid,
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
	defer ln.netLoop.Done()
	for {
		netConn, err := ln.netLn.Accept()
		if err != nil {
			vlog.VI(1).Infof("Exiting netAcceptLoop: net.Listener.Accept() failed on %v with %v", ln.netLn, err)
			return
		}
		vlog.VI(1).Infof("New net.Conn accepted from %s (local address: %s)", netConn.RemoteAddr(), netConn.LocalAddr())
		if err := tcputil.EnableTCPKeepAlive(netConn); err != nil {
			vlog.Errorf("Failed to enable TCP keep alive: %v", err)
		}

		conn := netConn
		if ln.hybrid {
			hconn := &hybridConn{conn: netConn}
			conn = hconn
			magicbuf := [1]byte{}
			n, err := io.ReadFull(netConn, magicbuf[:])
			if err != nil {
				vlog.VI(1).Infof("Shutting down conn from %s (local address: %s) since we failed to read the first byte: %v", netConn.RemoteAddr(), netConn.LocalAddr(), err)
				continue
			}
			hconn.buffered = magicbuf[:n]
			if magicbuf[0] != 'G' {
				// Can't possibly be a websocket connection
				if err := ln.q.Put(conn); err != nil {
					vlog.VI(1).Infof("Shutting down conn from %s (local address: %s) as Put failed in vifLoop: %v", netConn.RemoteAddr(), netConn.LocalAddr(), err)
				}
				continue
			}
			// Maybe be a websocket connection now.
		}
		ln.wsLoop.Add(1)
		if err := ln.httpQ.Put(conn); err != nil {
			ln.wsLoop.Done()
			vlog.VI(1).Infof("Shutting down conn from %s (local address: %s) as Put failed in vifLoop: %v", conn.RemoteAddr(), conn.LocalAddr(), err)
			conn.Close()
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
	ln.mu.Lock()
	if ln.closed {
		ln.mu.Unlock()
		return errListenerIsClosed
	}
	ln.closed = true
	ln.mu.Unlock()
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

type addr struct{ n, a string }

func (a *addr) Network() string {
	return a.n
}

func (a *addr) String() string {
	return a.a
}

func (ln *wsTCPListener) Addr() net.Addr {
	protocol := "ws"
	if ln.hybrid {
		protocol = "wsh"
	}
	a := &addr{protocol, ln.netLn.Addr().String()}
	return a
}
