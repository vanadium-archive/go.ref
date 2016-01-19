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
	"v.io/v23/flow"
	"v.io/v23/rpc"

	dfactory "v.io/x/ref/lib/discovery/factory"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/runtime/internal"
	"v.io/x/ref/runtime/internal/lib/appcycle"
	"v.io/x/ref/runtime/internal/lib/xwebsocket"
	grt "v.io/x/ref/runtime/internal/rt"
	_ "v.io/x/ref/runtime/protocols/tcp"
	_ "v.io/x/ref/runtime/protocols/ws"
	_ "v.io/x/ref/runtime/protocols/wsh"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterRuntimeFactory(Init)
	flow.RegisterUnknownProtocol("wsh", xwebsocket.WSH{})
	flags.SetDefaultHostPort(":0")
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
		AddressChooser: internal.IPAddressChooser{},
		Proxy:          lf.Proxy,
	}

	ishutdown := func() {
		ac.Shutdown()
		discovery.Close()
	}

	runtime, ctx, shutdown, err := grt.Init(ctx,
		ac,
		discovery,
		nil,
		&listenSpec,
		nil,
		commonFlags.RuntimeFlags(),
		nil)
	if err != nil {
		ishutdown()
		return nil, nil, nil, err
	}
	ctx.VI(1).Infof("Initializing generic RuntimeFactory.")

	runtimeFactoryShutdown := func() {
		ishutdown()
		shutdown()
	}
	return runtime, ctx, runtimeFactoryShutdown, nil
}
