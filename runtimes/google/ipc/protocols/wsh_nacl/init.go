// Package wsh_nacl registers the websocket 'hybrid' protocol for nacl
// architectures.
package wsh_nacl

import (
	"v.io/v23/ipc"

	"v.io/core/veyron/lib/websocket"
)

func init() {
	for _, p := range []string{"wsh", "wsh4", "wsh6"} {
		ipc.RegisterProtocol(p, websocket.Dial, websocket.Listener)
	}
}
