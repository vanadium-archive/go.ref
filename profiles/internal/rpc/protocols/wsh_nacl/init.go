// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package wsh_nacl registers the websocket 'hybrid' protocol for nacl
// architectures.
package wsh_nacl

import (
	"v.io/v23/rpc"

	"v.io/x/ref/profiles/internal/lib/websocket"
)

func init() {
	// We limit wsh to ws since in general nacl does not allow direct access
	// to TCP/UDP networking.
	rpc.RegisterProtocol("wsh", websocket.Dial, websocket.Resolve, websocket.Listener, "ws4", "ws6")
	rpc.RegisterProtocol("wsh4", websocket.Dial, websocket.Resolve, websocket.Listener, "ws4")
	rpc.RegisterProtocol("wsh6", websocket.Dial, websocket.Resolve, websocket.Listener, "ws6")
}
