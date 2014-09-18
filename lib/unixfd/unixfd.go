// Package unixfd provides provides support for Dialing and Listening
// on already connected file descriptors (like those returned by socketpair).
package unixfd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"veyron.io/veyron/veyron2/ipc/stream"
)

const Network string = "unixfd"

// singleConnListener implements net.Listener for an already-connected socket.
// This is different from net.FileListener, which calls syscall.Listen
// on an unconnected socket.
type singleConnListener struct {
	c    chan net.Conn
	addr net.Addr
	sync.Mutex
}

func (l *singleConnListener) getChan() chan net.Conn {
	l.Lock()
	defer l.Unlock()
	return l.c
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	c := l.getChan()
	if c == nil {
		return nil, errors.New("listener closed")
	}
	if conn, ok := <-c; ok {
		return conn, nil
	}
	return nil, io.EOF
}

func (l *singleConnListener) Close() error {
	l.Lock()
	defer l.Unlock()
	lc := l.c
	if lc == nil {
		return errors.New("listener already closed")
	}
	close(l.c)
	l.c = nil
	// If the socket was never Accept'ed we need to close it.
	if c, ok := <-lc; ok {
		return c.Close()
	}
	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return l.addr
}

func unixFDConn(address string) (net.Conn, error) {
	fd, err := strconv.ParseInt(address, 10, 32)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "tmp")
	conn, err := net.FileConn(file)
	// 'file' is not used after this point, but we keep it open
	// so that 'address' remains valid.
	if err != nil {
		file.Close()
		return nil, err
	}
	// We wrap 'conn' so we can customize the address, and also
	// to close 'file'.
	return &fdConn{addr(address), file, conn}, nil
}

type fdConn struct {
	addr net.Addr
	sock *os.File
	net.Conn
}

func (c *fdConn) Close() (err error) {
	defer c.sock.Close()
	return c.Conn.Close()
}

func (c *fdConn) LocalAddr() net.Addr {
	return c.addr
}

func (c *fdConn) RemoteAddr() net.Addr {
	return c.addr
}

func unixFDListen(address string) (net.Listener, error) {
	conn, err := unixFDConn(address)
	if err != nil {
		return nil, err
	}
	c := make(chan net.Conn, 1)
	c <- conn
	return &singleConnListener{c, conn.LocalAddr(), sync.Mutex{}}, nil
}

type addr string

func (a addr) Network() string { return Network }
func (a addr) String() string  { return string(a) }

// Addr returns a net.Addr for the unixfd network for the given file descriptor.
func Addr(fd uintptr) net.Addr {
	return addr(fmt.Sprintf("%d", fd))
}

// Socketpair returns two connected unix domain sockets, or an error.
func Socketpair() ([]*os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	return []*os.File{os.NewFile(uintptr(fds[0]), "local"), os.NewFile(uintptr(fds[1]), "remote")}, nil
}

func init() {
	stream.RegisterProtocol(Network, unixFDConn, unixFDListen)
}
