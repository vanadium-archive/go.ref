// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package wsh registers the websocket 'hybrid' protocol.
// We prefer to use tcp whenever we can to avoid the overhead of websockets.
package wsh

import (
	"v.io/v23/rpc"

	"v.io/x/ref/profiles/internal/lib/websocket"
)

func init() {
	rpc.RegisterProtocol("wsh", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener, "tcp4", "tcp6", "ws4", "ws6")
	rpc.RegisterProtocol("wsh4", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener, "tcp4", "ws4")
	rpc.RegisterProtocol("wsh6", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener, "tcp6", "ws6")
}
