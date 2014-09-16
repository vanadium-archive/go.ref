package unixfd

import (
	"bytes"
	"io"
	"net"
	"os"
	"reflect"
	"testing"
)

type nothing struct{}

func dial(fd *os.File) (net.Conn, error) {
	addr := Addr(fd.Fd())
	return unixFDConn(addr.String())
}

func listen(fd *os.File) (net.Listener, error) {
	addr := Addr(fd.Fd())
	return unixFDListen(addr.String())
}

func testWrite(t *testing.T, c net.Conn, data string) {
	n, err := c.Write([]byte(data))
	if err != nil {
		t.Errorf("Write: %v", err)
		return
	}
	if n != len(data) {
		t.Errorf("Wrote %d bytes, expected %d", n, len(data))
	}
}

func testRead(t *testing.T, c net.Conn, expected string) {
	buf := make([]byte, len(expected)+2)
	n, err := c.Read(buf)
	if err != nil {
		t.Errorf("Read: %v", err)
		return
	}
	if n != len(expected) || !bytes.Equal(buf[0:n], []byte(expected)) {
		t.Errorf("got %q, expected %q", buf[0:n], expected)
	}
}

func TestDial(t *testing.T) {
	fds, err := Socketpair()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	a, err := dial(fds[0])
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	b, err := dial(fds[1])
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	testWrite(t, a, "TEST1")
	testRead(t, b, "TEST1")
	testWrite(t, b, "TEST2")
	testRead(t, a, "TEST2")

	if !reflect.DeepEqual(a.LocalAddr(), Addr(fds[0].Fd())) {
		t.Errorf("Invalid address %v, expected %d", a.LocalAddr(), fds[0].Fd())
	}
	if !reflect.DeepEqual(a.RemoteAddr(), Addr(fds[0].Fd())) {
		t.Errorf("Invalid address %v, expected %d", a.RemoteAddr(), fds[0].Fd())
	}
	if !reflect.DeepEqual(b.LocalAddr(), Addr(fds[1].Fd())) {
		t.Errorf("Invalid address %v, expected %d", a.LocalAddr(), fds[1].Fd())
	}
	if !reflect.DeepEqual(b.RemoteAddr(), Addr(fds[1].Fd())) {
		t.Errorf("Invalid address %v, expected %d", a.RemoteAddr(), fds[1].Fd())
	}
}

func TestListen(t *testing.T) {
	fds, err := Socketpair()
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	a, err := dial(fds[0])
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	l, err := listen(fds[1])
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b, err := l.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	start := make(chan nothing, 0)
	done := make(chan nothing)
	go func() {
		defer close(done)
		<-start
		if _, err := l.Accept(); err != io.EOF {
			t.Fatalf("accept: expected EOF, got %v", err)
		}
	}()

	// block until the goroutine starts running
	start <- nothing{}
	testWrite(t, a, "LISTEN")
	testRead(t, b, "LISTEN")

	err = l.Close()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	<-done

	// After closed, accept should fail immediately
	_, err = l.Accept()
	if err == nil {
		t.Fatalf("Accept succeeded after close")
	}
	err = l.Close()
	if err == nil {
		t.Fatalf("Close succeeded twice")
	}
}
