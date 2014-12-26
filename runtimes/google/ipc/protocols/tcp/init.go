package tcp

import (
	"net"

	"v.io/core/veyron2/ipc/stream"
)

func init() {
	for _, p := range []string{"tcp", "tcp4", "tcp6"} {
		stream.RegisterProtocol(p, net.DialTimeout, net.Listen)
	}
}
