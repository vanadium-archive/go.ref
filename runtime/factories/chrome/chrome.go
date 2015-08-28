// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package chrome implements a RuntimeFactory for use within Chrome, in
// particular for use by Chrome extensions.
package chrome

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/runtime/internal"
	_ "v.io/x/ref/runtime/internal/flow/protocols/ws"
	_ "v.io/x/ref/runtime/internal/flow/protocols/wsh_nacl"
	"v.io/x/ref/runtime/internal/lib/websocket"
	grt "v.io/x/ref/runtime/internal/rt"

	// TODO(suharshs): Remove this after we switch to the flow protocols.
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/wsh_nacl"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterRuntimeFactory(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.Dial, websocket.Resolve, websocket.Listener)
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlagsAndConfigureGlobalLogger(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	protocols := []string{"wsh", "ws"}
	listenSpec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{Protocol: "ws", Address: ""}}}
	runtime, ctx, shutdown, err := grt.Init(ctx, nil, protocols, &listenSpec, nil, "", commonFlags.RuntimeFlags(), nil)
	if err != nil {
		return nil, nil, shutdown, err
	}
	ctx.VI(1).Infof("Initializing chrome RuntimeFactory.")
	return runtime, ctx, shutdown, nil
}
