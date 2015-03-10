// +build !nacl

package websocket

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebsocketConn provides a net.Conn interface for a websocket connection.
func WebsocketConn(ws *websocket.Conn) net.Conn {
	return &wrappedConn{ws: ws}
}

// wrappedConn provides a net.Conn interface to a websocket.
// The underlying websocket connection needs regular calls to Read to make sure
// websocket control messages (such as pings) are processed by the websocket
// library.
type wrappedConn struct {
	ws         *websocket.Conn
	currReader io.Reader

	// The gorilla docs aren't explicit about reading and writing from
	// different goroutines.  It is explicit that only one goroutine can
	// do a write at any given time and only one goroutine can do a read
	// at any given time.  Based on inspection it seems that using a reader
	// and writer simultaneously is safe, but this might change with
	// future changes.  We can't actually share the lock, because this means
	// that we can't write while we are waiting for a message, causing some
	// deadlocks where a write is need to unblock a read.
	writeLock sync.Mutex
	readLock  sync.Mutex
}

func (c *wrappedConn) readFromCurrReader(b []byte) (int, error) {
	n, err := c.currReader.Read(b)
	if err == io.EOF {
		err = nil
		c.currReader = nil
	}
	return n, err
}

func (c *wrappedConn) Read(b []byte) (int, error) {
	c.readLock.Lock()
	defer c.readLock.Unlock()
	var n int
	var err error

	// TODO(bjornick): It would be nice to be able to read multiple messages at
	// a time in case the first message is not big enough to fill b and another
	// message is ready.
	// Loop until we either get data or an error.  This exists
	// mostly to avoid return 0, nil.
	for n == 0 && err == nil {
		if c.currReader == nil {
			t, r, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if t != websocket.BinaryMessage {
				return 0, fmt.Errorf("Unexpected message type %d", t)
			}
			c.currReader = r
		}
		n, err = c.readFromCurrReader(b)
	}
	return n, err
}

func (c *wrappedConn) Write(b []byte) (int, error) {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *wrappedConn) Close() error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	// Send an EOF control message to the remote end so that it can
	// handle the close gracefully.
	msg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "EOF")
	c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
	return c.ws.Close()
}

func (c *wrappedConn) LocalAddr() net.Addr {
	return &addr{"ws", c.ws.LocalAddr().String()}
}

func (c *wrappedConn) RemoteAddr() net.Addr {
	return &addr{"ws", c.ws.RemoteAddr().String()}
}

func (c *wrappedConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *wrappedConn) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}

func (c *wrappedConn) SetWriteDeadline(t time.Time) error {
	return c.ws.SetWriteDeadline(t)
}

// hybridConn is used by the 'hybrid' protocol that can accept
// either 'tcp' or 'websocket' connections. In particular, it allows
// for the reader to peek and buffer the first n bytes of a stream
// in order to determine what the connection type is.
type hybridConn struct {
	conn     net.Conn
	buffered []byte
}

func (wc *hybridConn) Read(b []byte) (int, error) {
	lbuf := len(wc.buffered)
	if lbuf == 0 {
		return wc.conn.Read(b)
	}
	copyn := copy(b, wc.buffered)
	wc.buffered = wc.buffered[copyn:]
	if len(b) > copyn {
		n, err := wc.conn.Read(b[copyn:])
		return copyn + n, err
	}
	return copyn, nil
}

func (wc *hybridConn) Write(b []byte) (n int, err error) {
	return wc.conn.Write(b)
}

func (wc *hybridConn) Close() error {
	return wc.conn.Close()
}

func (wc *hybridConn) LocalAddr() net.Addr {
	return &addr{"wsh", wc.conn.LocalAddr().String()}
}

func (wc *hybridConn) RemoteAddr() net.Addr {
	return &addr{"wsh", wc.conn.RemoteAddr().String()}
}

func (wc *hybridConn) SetDeadline(t time.Time) error {
	return wc.conn.SetDeadline(t)
}

func (wc *hybridConn) SetReadDeadline(t time.Time) error {
	return wc.conn.SetReadDeadline(t)
}

func (wc *hybridConn) SetWriteDeadline(t time.Time) error {
	return wc.conn.SetWriteDeadline(t)
}
