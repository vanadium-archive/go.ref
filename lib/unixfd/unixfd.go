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
	"unsafe"
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
	lfd, rfd, err := socketpair(false)
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

func socketpair(closeRemoteOnExec bool) (local, remote *fileDescriptor, err error) {
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fds[0])
		if closeRemoteOnExec {
			syscall.CloseOnExec(fds[1])
		}
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
// Note that the returned address refers to an open file descriptor,
// which you must close if you do not Dial or Listen to the address.
func SendConnection(conn *net.UnixConn, data []byte) (addr net.Addr, err error) {
	if len(data) < 1 {
		return nil, errors.New("cannot send a socket without data.")
	}
	local, remote, err := socketpair(true)
	if err != nil {
		return nil, err
	}
	defer local.maybeClose()
	rfile := remote.releaseFile()
	defer rfile.Close()

	rights := syscall.UnixRights(int(rfile.Fd()))
	n, oobn, err := conn.WriteMsgUnix(data, rights, nil)
	if err != nil {
		return nil, err
	} else if n != len(data) || oobn != len(rights) {
		return nil, fmt.Errorf("expected to send %d, %d bytes,  sent %d, %d", len(data), len(rights), n, oobn)
	}
	return local.releaseAddr(), nil
}

const cmsgDataLength = int(unsafe.Sizeof(int(1)))

// ReadConnection reads a connection and additional data sent on 'conn' via a call to SendConnection.
// 'buf' must be large enough to hold the data.
func ReadConnection(conn *net.UnixConn, buf []byte) (net.Addr, int, error) {
	oob := make([]byte, syscall.CmsgLen(cmsgDataLength))
	n, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, n, err
	}
	if oobn > len(oob) {
		return nil, n, fmt.Errorf("received too large oob data (%d, max %d)", oobn, len(oob))
	}
	scms, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, n, err
	}
	fd := -1
	for _, scm := range scms {
		fds, err := syscall.ParseUnixRights(&scm)
		if err != nil {
			return nil, n, err
		}
		for _, f := range fds {
			if fd != -1 {
				syscall.Close(fd)
			}
			fd = f
		}
	}
	if fd == -1 {
		return nil, n, nil
	}
	return Addr(uintptr(fd)), n, nil
}

func init() {
	stream.RegisterProtocol(Network, unixFDConn, unixFDListen)
}
