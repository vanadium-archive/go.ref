package blackbox

import (
	"io"
	"net"
	"sync"
	"time"

	"veyron.io/veyron/veyron2/vlog"
)

type syncBool struct {
	mu    sync.Mutex
	value bool
}

func (s *syncBool) get() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}
func (s *syncBool) set(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = value
}

// Wrap a net.Conn and to force a failure when some amount of data has been transfered.
type faultyConn struct {
	conn       net.Conn
	readBytes  int
	readMax    int
	writeBytes int
	writeMax   int
	forcedErr  syncBool
}

func NewFaultyConn(conn net.Conn, readMax, writeMax int) net.Conn {
	return &faultyConn{conn: conn, readMax: readMax, writeMax: writeMax}
}

func (conn *faultyConn) Close() error {
	return conn.conn.Close()
}

func (conn *faultyConn) Read(buf []byte) (int, error) {
	if conn.forcedErr.get() {
		return 0, io.EOF
	}
	amount := conn.readMax - conn.readBytes
	if amount == 0 {
		vlog.VI(2).Info(formatLogLine("faultyConn(%p): closed", conn))
		// subsequent Read/Write's should fail
		conn.forcedErr.set(true)
		return 0, io.EOF
	}
	if amount < len(buf) {
		buf = buf[0:amount]
	}
	num, err := conn.conn.Read(buf)
	conn.readBytes += num
	return num, err
}

func (conn *faultyConn) Write(buf []byte) (int, error) {
	if conn.forcedErr.get() {
		return 0, io.EOF
	}
	amount := conn.writeMax - conn.writeBytes
	shortWrite := false
	if amount < len(buf) {
		buf = buf[0:amount]
		shortWrite = true
	}
	num, err := conn.conn.Write(buf)
	conn.writeBytes += num
	if shortWrite && err == nil {
		vlog.VI(2).Info(formatLogLine("faultyConn(%p): write closed", conn))
		// subsequent Read/Write's should fail
		conn.forcedErr.set(true)
		err = io.ErrShortWrite
	}
	return num, err
}

func (conn *faultyConn) LocalAddr() net.Addr {
	return conn.conn.LocalAddr()
}

func (conn *faultyConn) RemoteAddr() net.Addr {
	return conn.conn.RemoteAddr()
}

func (conn *faultyConn) SetDeadline(t time.Time) error {
	return conn.conn.SetDeadline(t)
}

func (conn *faultyConn) SetReadDeadline(t time.Time) error {
	return conn.conn.SetReadDeadline(t)
}

func (conn *faultyConn) SetWriteDeadline(t time.Time) error {
	return conn.conn.SetWriteDeadline(t)
}
