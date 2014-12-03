// +build nacl

package websocket

import (
	"fmt"
	"net"
)

// Websocket listeners are not supported in NaCl.
// This file is needed for compilation only.

const BinaryMagicByte byte = 0x90

func NewListener(netLn net.Listener) (net.Listener, error) {
	return nil, fmt.Errorf("Websocket NewListener called in nacl code!")
}
