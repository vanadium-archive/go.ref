package websocket

import (
	"v.io/v23/ipc"

	"v.io/core/veyron/lib/websocket"
)

func init() {
	// ws, ws4, ws6 represent websocket protocol instances.
	for _, p := range []string{"ws", "ws4", "ws6"} {
		ipc.RegisterProtocol(p, websocket.Dial, websocket.Listener)
	}
}
