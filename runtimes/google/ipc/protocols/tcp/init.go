package tcp

import (
	"net"
	"time"

	"v.io/core/veyron/lib/tcputil"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/vlog"
)

func init() {
	for _, p := range []string{"tcp", "tcp4", "tcp6"} {
		ipc.RegisterProtocol(p, tcpDial, tcpListen)
	}
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
