package tcp

import (
	"fmt"
	"net"
	"time"

	"veyron.io/veyron/veyron2/ipc/stream"

	"veyron.io/veyron/veyron/lib/websocket"
)

func dialer(network, address string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, err
	}
	// For tcp connections we add an extra magic byte so we can differentiate between
	// raw tcp and websocket on the same port.
	switch n, err := conn.Write([]byte{websocket.BinaryMagicByte}); {
	case err != nil:
		return nil, err
	case n != 1:
		return nil, fmt.Errorf("Unable to write the magic byte")
	}
	return conn, nil
}

func listener(network, address string) (net.Listener, error) {
	return net.Listen(network, address)
}

func init() {
	for _, p := range []string{"tcp", "tcp4", "tcp6"} {
		stream.RegisterProtocol(p, dialer, listener)
	}
}
