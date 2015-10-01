// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package generic implements a RuntimeFactory that is useful in tests. It
// prefers listening on localhost addresses.
package generic

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/runtime/internal"
	_ "v.io/x/ref/runtime/internal/flow/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/flow/protocols/ws"
	_ "v.io/x/ref/runtime/internal/flow/protocols/wsh"
	"v.io/x/ref/runtime/internal/lib/appcycle"
	"v.io/x/ref/runtime/internal/lib/websocket"
	grt "v.io/x/ref/runtime/internal/rt"

	// TODO(suharshs): Remove these once we switch to the flow protocols.
	_ "v.io/x/ref/runtime/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/wsh"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterRuntimeFactory(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener)
	flags.SetDefaultHostPort(":0")
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlagsAndConfigureGlobalLogger(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	ac := appcycle.New()

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs:          rpc.ListenAddrs(lf.Addrs),
		AddressChooser: internal.IPAddressChooser{},
		Proxy:          lf.Proxy,
	}

	runtime, ctx, shutdown, err := grt.Init(ctx,
		ac,
		nil,
		&listenSpec,
		nil,
		"",
		commonFlags.RuntimeFlags(),
		nil)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx.VI(1).Infof("Initializing generic RuntimeFactory.")

	runtimeFactoryShutdown := func() {
		ac.Shutdown()
		shutdown()
	}
	return runtime, ctx, runtimeFactoryShutdown, nil
}
