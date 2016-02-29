// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build linux

// Package gce implements a RuntimeFactory for binaries that only run on Google
// Compute Engine (GCE).
package gce

import (
	"flag"
	"fmt"
	"net"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/rpc"

	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/runtime/internal"
	"v.io/x/ref/runtime/internal/gce"
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
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlagsAndConfigureGlobalLogger(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	if !gce.RunningOnGCE() {
		return nil, nil, nil, fmt.Errorf("GCE profile used on a non-GCE system")
	}

	ac := appcycle.New()

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs(lf.Addrs),
		Proxy: lf.Proxy,
	}

	if ip, err := gce.ExternalIPAddress(); err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	} else {
		listenSpec.AddressChooser = netstate.AddressChooserFunc(func(network string, addrs []net.Addr) ([]net.Addr, error) {
			return []net.Addr{netstate.NewNetAddr("wsh", ip.String())}, nil
		})
	}

	runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, nil, nil, &listenSpec, nil, commonFlags.RuntimeFlags(), nil, 0)
	if err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	}

	ctx.VI(1).Infof("Initializing GCE RuntimeFactory.")

	runtimeFactoryShutdown := func() {
		ac.Shutdown()
		shutdown()
	}
	return runtime, ctx, runtimeFactoryShutdown, nil
}
