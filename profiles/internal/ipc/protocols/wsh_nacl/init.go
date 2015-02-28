// Package wsh_nacl registers the websocket 'hybrid' protocol for nacl
// architectures.
package wsh_nacl

import (
	"v.io/v23/ipc"

	"v.io/x/ref/lib/websocket"
)

func init() {
	// We limit wsh to ws since in general nacl does not allow direct access
	// to TCP/UDP networking.
	ipc.RegisterProtocol("wsh", websocket.Dial, websocket.Listener, "ws4", "ws6")
	ipc.RegisterProtocol("wsh4", websocket.Dial, websocket.Listener, "ws4")
	ipc.RegisterProtocol("wsh6", websocket.Dial, websocket.Listener, "ws6")
}
