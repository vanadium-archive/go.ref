// +build !nacl
package websocket

import (
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"sync"

	"time"
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

			if t != websocket.BinaryMessage {
				return 0, fmt.Errorf("Unexpected message type %d", t)
			}
			if err != nil {
				return 0, err
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
	return c.ws.Close()
}

func (c *wrappedConn) LocalAddr() net.Addr {
	return websocketAddr{s: c.ws.LocalAddr().String()}
}

func (c *wrappedConn) RemoteAddr() net.Addr {
	return websocketAddr{s: c.ws.RemoteAddr().String()}
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

type websocketAddr struct {
	s string
}

func (websocketAddr) Network() string {
	return "ws"
}

func (w websocketAddr) String() string {
	return w.s
}
