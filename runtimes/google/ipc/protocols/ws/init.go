package websocket

import (
	"v.io/core/veyron2/ipc/stream"

	"v.io/core/veyron/lib/websocket"
)

func init() {
	// ws, ws4, ws6 represent websocket protocol instances.
	for _, p := range []string{"ws", "ws4", "ws6"} {
		stream.RegisterProtocol(p, websocket.Dial, websocket.Listener)
	}
}
