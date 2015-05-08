// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package unixfd provides provides support for Dialing and Listening
// on already connected file descriptors (like those returned by socketpair).
package unixfd

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"v.io/v23/rpc"
	"v.io/v23/verror"
)

const pkgPath = "v.io/x/ref/services/agent/internal/unixfd"

var (
	errListenerClosed            = verror.Register(pkgPath+".errListenerClosed", verror.NoRetry, "{1:}{2:} listener closed{:_}")
	errListenerAlreadyClosed     = verror.Register(pkgPath+".errListenerAlreadyClosed", verror.NoRetry, "{1:}{2:} listener already closed{:_}")
	errCantSendSocketWithoutData = verror.Register(pkgPath+".errCantSendSocketWithoutData", verror.NoRetry, "{1:}{2:} cannot send a socket without data.{:_}")
	errWrongSentLength           = verror.Register(pkgPath+".errWrongSentLength", verror.NoRetry, "{1:}{2:} expected to send {3}, {4} bytes,  sent {5}, {6}{:_}")
	errTooBigOOB                 = verror.Register(pkgPath+".errTooBigOOB", verror.NoRetry, "{1:}{2:} received too large oob data ({3}, max {4}){:_}")
	errBadNetwork                = verror.Register(pkgPath+".errBadNetwork", verror.NoRetry, "{1:}{2:} invalid network{:_}")
)

const Network string = "unixfd"

func init() {
	rpc.RegisterProtocol(Network, unixFDConn, unixFDResolve, unixFDListen)
}

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
		return nil, verror.New(errListenerClosed, nil)
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
		return verror.New(errListenerAlreadyClosed, nil)
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

func unixFDConn(protocol, address string, timeout time.Duration) (net.Conn, error) {
	// TODO(cnicolaou): have this respect the timeout. Possibly have a helper
	// function that can be generally used for this, but in practice, I think
	// it'll be cleaner to use the underlying protocol's deadline support of it
	// has it.
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
	return &fdConn{addr: addr(address), sock: file, Conn: conn}, nil
}

type fdConn struct {
	addr net.Addr
	sock *os.File
	net.Conn

	mu     sync.Mutex
	closed bool
}

func (c *fdConn) Close() (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	defer c.sock.Close()
	return c.Conn.Close()
}

func (c *fdConn) LocalAddr() net.Addr {
	return c.addr
}

func (c *fdConn) RemoteAddr() net.Addr {
	return c.addr
}

func unixFDResolve(_, address string) (string, string, error) {
	return Network, address, nil
}

func unixFDListen(protocol, address string) (net.Listener, error) {
	conn, err := unixFDConn(protocol, address, 0)
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

type fileDescriptor struct {
	fd   chan int
	name string
}

func newFd(fd int, name string) *fileDescriptor {
	ch := make(chan int, 1)
	ch <- fd
	close(ch)
	d := &fileDescriptor{ch, name}
	return d
}

func (f *fileDescriptor) releaseAddr() net.Addr {
	if fd, ok := <-f.fd; ok {
		return Addr(uintptr(fd))
	}
	return nil
}

func (f *fileDescriptor) releaseFile() *os.File {
	if fd, ok := <-f.fd; ok {
		return os.NewFile(uintptr(fd), f.name)
	}
	return nil
}

// maybeClose closes the file descriptor, if it hasn't been released.
func (f *fileDescriptor) maybeClose() {
	if file := f.releaseFile(); file != nil {
		file.Close()
	}
}

// Socketpair returns a pair of connected sockets for communicating with a child process.
func Socketpair() (*net.UnixConn, *os.File, error) {
	lfd, rfd, err := socketpair()
	if err != nil {
		return nil, nil, err
	}
	defer rfd.maybeClose()
	file := lfd.releaseFile()
	// FileConn dups the fd, so we still want to close the original one.
	defer file.Close()
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, nil, err
	}
	return conn.(*net.UnixConn), rfd.releaseFile(), nil
}

func socketpair() (local, remote *fileDescriptor, err error) {
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fds[0])
		syscall.CloseOnExec(fds[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, nil, err
	}
	return newFd(fds[0], "local"), newFd(fds[1], "remote"), nil
}

// SendConnection creates a new connected socket and sends
// one end over 'conn', along with 'data'. It returns the address for
// the local end of the socketpair.
// Note that the returned address is an open file descriptor,
// which you must close if you do not Dial or Listen to the address.
func SendConnection(conn *net.UnixConn, data []byte) (addr net.Addr, err error) {
	if len(data) < 1 {
		return nil, verror.New(errCantSendSocketWithoutData, nil)
	}
	remote, local, err := socketpair()
	if err != nil {
		return nil, err
	}
	defer local.maybeClose()
	rfile := remote.releaseFile()

	rights := syscall.UnixRights(int(rfile.Fd()))
	n, oobn, err := conn.WriteMsgUnix(data, rights, nil)
	if err != nil {
		rfile.Close()
		return nil, err
	} else if n != len(data) || oobn != len(rights) {
		rfile.Close()
		return nil, verror.New(errWrongSentLength, nil, len(data), len(rights), n, oobn)
	}
	// Wait for the other side to acknowledge.
	// This is to work around a race on OS X where it appears we can close
	// the file descriptor before it gets transfered over the socket.
	f := local.releaseFile()
	syscall.ForkLock.Lock()
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		syscall.ForkLock.Unlock()
		f.Close()
		rfile.Close()
		return nil, err
	}
	syscall.CloseOnExec(fd)
	syscall.ForkLock.Unlock()
	newConn, err := net.FileConn(f)
	f.Close()
	if err != nil {
		rfile.Close()
		return nil, err
	}
	newConn.Read(make([]byte, 1))
	newConn.Close()
	rfile.Close()

	return Addr(uintptr(fd)), nil
}

const cmsgDataLength = int(unsafe.Sizeof(int(1)))

// ReadConnection reads a connection and additional data sent on 'conn' via a call to SendConnection.
// 'buf' must be large enough to hold the data.
// The returned function must be called when you are ready for the other side
// to start sending data, but before writing anything to the connection.
// If there is an error you must still call the function before closing the connection.
func ReadConnection(conn *net.UnixConn, buf []byte) (net.Addr, int, func(), error) {
	oob := make([]byte, syscall.CmsgLen(cmsgDataLength))
	n, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, n, nil, err
	}
	if oobn > len(oob) {
		return nil, n, nil, verror.New(errTooBigOOB, nil, oobn, len(oob))
	}
	scms, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, n, nil, err
	}
	fd := -1
	// Loop through any file descriptors we are sent, and close
	// all extras.
	for _, scm := range scms {
		fds, err := syscall.ParseUnixRights(&scm)
		if err != nil {
			return nil, n, nil, err
		}
		for _, f := range fds {
			if fd == -1 {
				fd = f
			} else if f != -1 {
				syscall.Close(f)
			}
		}
	}
	if fd == -1 {
		return nil, n, nil, nil
	}
	result := Addr(uintptr(fd))
	syscall.ForkLock.Lock()
	fd, err = syscall.Dup(fd)
	if err != nil {
		syscall.ForkLock.Unlock()
		CloseUnixAddr(result)
		return nil, n, nil, err
	}
	syscall.CloseOnExec(fd)
	syscall.ForkLock.Unlock()
	file := os.NewFile(uintptr(fd), "newconn")
	newconn, err := net.FileConn(file)
	file.Close()
	if err != nil {
		CloseUnixAddr(result)
		return nil, n, nil, err
	}
	return result, n, func() {
		newconn.Write(make([]byte, 1))
		newconn.Close()
	}, nil
}

func CloseUnixAddr(addr net.Addr) error {
	if addr.Network() != Network {
		return verror.New(errBadNetwork, nil)
	}
	fd, err := strconv.ParseInt(addr.String(), 10, 32)
	if err != nil {
		return err
	}
	return syscall.Close(int(fd))
}
