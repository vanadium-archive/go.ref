// Package wsh registers the websocket 'hybrid' protocol.
// We prefer to use tcp whenever we can to avoid the overhead of websockets.
package wsh

import (
	"v.io/core/veyron2/ipc"

	"v.io/core/veyron/lib/websocket"
)

func init() {
	for _, p := range []string{"wsh", "wsh4", "wsh6"} {
		ipc.RegisterProtocol(p, websocket.HybridDial, websocket.HybridListener)
	}
}
