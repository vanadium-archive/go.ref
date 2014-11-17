// +build nacl

package wslistener

import (
	"net"
)

// Websocket listeners are not supported in NaCl.
// This file is needed for compilation only.

const BinaryMagicByte byte = 0x90

func NewListener(netLn net.Listener) net.Listener {
	panic("Websocket NewListener called in nacl code!")
}
