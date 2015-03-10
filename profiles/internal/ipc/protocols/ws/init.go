package websocket

import (
	"v.io/v23/ipc"

	"v.io/x/ref/profiles/internal/lib/websocket"
)

func init() {
	// ws, ws4, ws6 represent websocket protocol instances.
	ipc.RegisterProtocol("ws", websocket.Dial, websocket.Listener, "ws4", "ws6")
	ipc.RegisterProtocol("ws4", websocket.Dial, websocket.Listener)
	ipc.RegisterProtocol("ws6", websocket.Dial, websocket.Listener)
}
