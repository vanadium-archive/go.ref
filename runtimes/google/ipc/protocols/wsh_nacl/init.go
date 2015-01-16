// Package wsh_nacl registers the websocket 'hybrid' protocol for nacl
// architectures.
package wsh_nacl

import (
	"v.io/core/veyron2/ipc/stream"

	"v.io/core/veyron/lib/websocket"
)

func init() {
	for _, p := range []string{"wsh", "wsh4", "wsh6"} {
		stream.RegisterProtocol(p, websocket.Dial, websocket.Listener)
	}
}
