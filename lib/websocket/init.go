package websocket

import (
	"net"
	"time"

	"veyron.io/veyron/veyron2/ipc/stream"
)

func wsListener(protocol, address string) (net.Listener, error) {
	ln, err := net.Listen(protocol, address)
	if err != nil {
		return nil, err
	}
	return NewListener(ln)
}

func wsDialer(protocol, address string, timeout time.Duration) (net.Conn, error) {
	// TODO(cnicolaou): implement timeout support.
	return Dial(address)
}

func init() {
	for _, p := range []string{"ws", "ws4", "ws6"} {
		stream.RegisterProtocol(p, wsDialer, wsListener)
	}
}
