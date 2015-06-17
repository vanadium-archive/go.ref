// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package static implements a RuntimeFactory for static network configurations.
package static

import (
	"flag"
	"net"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"

	"v.io/x/ref/internal/logger"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/runtime/internal"
	"v.io/x/ref/runtime/internal/lib/appcycle"
	"v.io/x/ref/runtime/internal/lib/websocket"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/wsh"
	"v.io/x/ref/runtime/internal/rt"
	"v.io/x/ref/services/debug/debuglib"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterRuntimeFactory(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridResolve, websocket.HybridListener)
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlags(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}
	reservedDispatcher := debuglib.NewDispatcher(logger.Manager(logger.Global()).LogDir, securityflag.NewAuthorizerOrDie())

	ac := appcycle.New()

	// Our address is private, so we test for running on GCE and for its 1:1 NAT
	// configuration. GCEPublicAddress returns a non-nil addr if we are
	// running on GCE.
	if !internal.HasPublicIP(logger.Global()) {
		if addr := internal.GCEPublicAddress(logger.Global()); addr != nil {
			listenSpec.AddressChooser = rpc.AddressChooserFunc(func(string, []net.Addr) ([]net.Addr, error) {
				return []net.Addr{addr}, nil
			})
			runtime, ctx, shutdown, err := rt.Init(ctx, ac, nil, &listenSpec, nil, "", commonFlags.RuntimeFlags(), reservedDispatcher)
			if err != nil {
				return nil, nil, nil, err
			}
			runtimeFactoryShutdown := func() {
				ac.Shutdown()
				shutdown()
			}
			return runtime, ctx, runtimeFactoryShutdown, nil
		}
	}
	listenSpec.AddressChooser = internal.IPAddressChooser{}

	runtime, ctx, shutdown, err := rt.Init(ctx, ac, nil, &listenSpec, nil, "", commonFlags.RuntimeFlags(), reservedDispatcher)
	if err != nil {
		return nil, nil, shutdown, err
	}

	runtimeFactoryShutdown := func() {
		ac.Shutdown()
		shutdown()
	}
	return runtime, ctx, runtimeFactoryShutdown, nil
}
