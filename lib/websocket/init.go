package websocket

import (
	"net"
	"time"

	"veyron.io/veyron/veyron2/ipc/stream"
)

var mapWebSocketToTCP = map[string]string{"ws": "tcp", "ws4": "tcp4", "ws6": "tcp6"}

func wsListener(protocol, address string) (net.Listener, error) {
	tcp := mapWebSocketToTCP[protocol]
	ln, err := net.Listen(tcp, address)
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
	// ws, ws4, ws6 represent websocket protocol instances.
	for _, p := range []string{"ws", "ws4", "ws6"} {
		stream.RegisterProtocol(p, wsDialer, wsListener)
	}

	// TODO(cnicolaou): fully enable and test this 'hybrid mode'.
	// hws, hws4, hws6 represent a 'hybrid' protocol that can accept
	// both websockets and tcp, using a 'magic' byte to discriminate
	// between the two. These are needed when a single network port must
	// be use to serve both websocket and tcp clients, we prefer to use
	// tcp whenever we can to avoid the overhead of websockets. Clients
	// decide whether to use hybrid tcp or websockets by electing to dial
	// using the hws protocol or the ws protocol respectively.
	//for _, p := range []string{"wsh", "wsh4", "wsh6"} {
	//	stream.RegisterProtocol(p, tcpHybridDialer, wsHybridListener)
	//}

	// The implementation strategy is as follows:
	// tcpHybridDialer will create and return a wrapped net.Conn which will
	// write the 'magic' time the first time that its Write method is called
	// but will otherwise be indistinguishable from the underlying net.Conn.
	// This first write will require an extra copy, but avoid potentially
	// sending two packets.
	// wsHybridListener is essentially the same as the current wsTCPListener,
	// but the magic byte handling implemented on a conditional basis.
}

/*
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
}
*/
