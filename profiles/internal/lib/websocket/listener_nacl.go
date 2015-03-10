// +build nacl

package websocket

import (
	"fmt"
	"net"
)

// Websocket listeners are not supported in NaCl.
// This file is needed for compilation only.
func listener(protocol, address string, hybrid bool) (net.Listener, error) {
	return nil, fmt.Errorf("Websocket Listener called in nacl code!")
}

func Listener(protocol, address string) (net.Listener, error) {
	return nil, fmt.Errorf("Websocket Listener called in nacl code!")
}
