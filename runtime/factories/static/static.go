// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package static implements a RuntimeFactory for static network configurations.
package static

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc"

	"v.io/x/ref/internal/logger"
	dfactory "v.io/x/ref/lib/discovery/factory"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/runtime/internal"
	_ "v.io/x/ref/runtime/internal/flow/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/flow/protocols/ws"
	_ "v.io/x/ref/runtime/internal/flow/protocols/wsh"
	"v.io/x/ref/runtime/internal/lib/appcycle"
	"v.io/x/ref/runtime/internal/lib/websocket"
	"v.io/x/ref/runtime/internal/lib/xwebsocket"

	"v.io/x/ref/runtime/internal/rt"
	"v.io/x/ref/services/debug/debuglib"

	// TODO(suharshs): Remove these once we switch to the flow protocols.
	_ "v.io/x/ref/runtime/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/wsh"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterRuntimeFactory(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener)
	flow.RegisterUnknownProtocol("wsh", xwebsocket.WSH{})
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlagsAndConfigureGlobalLogger(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	ac := appcycle.New()
	discovery, err := dfactory.New()
	if err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	}

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs:          rpc.ListenAddrs(lf.Addrs),
		Proxy:          lf.Proxy,
		AddressChooser: internal.NewAddressChooser(logger.Global()),
	}
	reservedDispatcher := debuglib.NewDispatcher(securityflag.NewAuthorizerOrDie())
	ishutdown := func() {
		ac.Shutdown()
		discovery.Close()
	}
	runtime, ctx, shutdown, err := rt.Init(ctx, ac, discovery, nil, &listenSpec, nil, "", commonFlags.RuntimeFlags(), reservedDispatcher)
	if err != nil {
		ishutdown()
		return nil, nil, nil, err
	}
	runtimeFactoryShutdown := func() {
		ishutdown()
		shutdown()
	}
	return runtime, ctx, runtimeFactoryShutdown, nil
}
