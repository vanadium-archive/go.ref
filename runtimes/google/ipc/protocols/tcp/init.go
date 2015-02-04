package tcp

import (
	"net"

	"v.io/core/veyron2/ipc"
)

func init() {
	for _, p := range []string{"tcp", "tcp4", "tcp6"} {
		ipc.RegisterProtocol(p, net.DialTimeout, net.Listen)
	}
}
