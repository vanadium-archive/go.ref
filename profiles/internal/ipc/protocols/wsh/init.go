// Package wsh registers the websocket 'hybrid' protocol.
// We prefer to use tcp whenever we can to avoid the overhead of websockets.
package wsh

import (
	"v.io/v23/ipc"

	"v.io/x/ref/lib/websocket"
)

func init() {
	ipc.RegisterProtocol("wsh", websocket.HybridDial, websocket.HybridListener, "tcp4", "tcp6", "ws4", "ws6")
	ipc.RegisterProtocol("wsh4", websocket.HybridDial, websocket.HybridListener, "tcp4", "ws4")
	ipc.RegisterProtocol("wsh6", websocket.HybridDial, websocket.HybridListener, "tcp6", "ws6")
}
