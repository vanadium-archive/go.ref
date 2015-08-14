// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build nacl

package websocket

import (
	"fmt"
	"net"

	"v.io/v23/context"
)

// Websocket listeners are not supported in NaCl.
// This file is needed for compilation only.
func listener(protocol, address string, hybrid bool) (net.Listener, error) {
	return nil, fmt.Errorf("Websocket Listener called in nacl code!")
}

func Listener(ctx *context.T, protocol, address string) (net.Listener, error) {
	return nil, fmt.Errorf("Websocket Listener called in nacl code!")
}
