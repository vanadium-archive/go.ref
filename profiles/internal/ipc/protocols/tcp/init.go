package tcp

import (
	"net"
	"time"

	"v.io/x/ref/profiles/internal/lib/tcputil"

	"v.io/v23/ipc"
	"v.io/x/lib/vlog"
)

func init() {
	ipc.RegisterProtocol("tcp", net.DialTimeout, net.Listen, "tcp4", "tcp6")
	ipc.RegisterProtocol("tcp4", net.DialTimeout, net.Listen)
	ipc.RegisterProtocol("tcp6", net.DialTimeout, net.Listen)
}

func tcpDial(network, address string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, err
	}
	if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
		return nil, err
	}
	return conn, nil
}

// tcpListen returns a listener that sets KeepAlive on all accepted connections.
func tcpListen(network, laddr string) (net.Listener, error) {
	ln, err := net.Listen(network, laddr)
	if err != nil {
		return nil, err
	}
	return &tcpListener{ln}, nil
}

// tcpListener is a wrapper around net.Listener that sets KeepAlive on all
// accepted connections.
type tcpListener struct {
	netLn net.Listener
}

func (ln *tcpListener) Accept() (net.Conn, error) {
	conn, err := ln.netLn.Accept()
	if err != nil {
		return nil, err
	}
	if err := tcputil.EnableTCPKeepAlive(conn); err != nil {
		vlog.Errorf("Failed to enable TCP keep alive: %v", err)
	}
	return conn, nil
}

func (ln *tcpListener) Close() error {
	return ln.netLn.Close()
}

func (ln *tcpListener) Addr() net.Addr {
	return ln.netLn.Addr()
}
