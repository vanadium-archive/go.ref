package websocket

import (
	"veyron.io/veyron/veyron2/ipc/stream"

	"veyron.io/veyron/veyron/lib/websocket"
)

func init() {
	// ws, ws4, ws6 represent websocket protocol instances.
	for _, p := range []string{"ws", "ws4", "ws6"} {
		stream.RegisterProtocol(p, websocket.Dial, websocket.Listener)
	}
}
