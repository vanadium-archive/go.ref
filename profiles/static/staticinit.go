// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package static

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"

	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/profiles/internal"
	"v.io/x/ref/profiles/internal/lib/appcycle"
	"v.io/x/ref/profiles/internal/lib/websocket"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/ws"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/wsh"
	grt "v.io/x/ref/profiles/internal/rt"
	"v.io/x/ref/services/mgmt/debug"

	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "v.io/x/ref/security/flag"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterProfile(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
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
	reservedDispatcher := debug.NewDispatcher(vlog.Log.LogDir, sflag.NewAuthorizerOrDie())

	ac := appcycle.New()

	// Our address is private, so we test for running on GCE and for its 1:1 NAT
	// configuration. GCEPublicAddress returns a non-nil addr if we are running on GCE.
	if !internal.HasPublicIP(vlog.Log) {
		if addr := internal.GCEPublicAddress(vlog.Log); addr != nil {
			listenSpec.AddressChooser = func(string, []rpc.Address) ([]rpc.Address, error) {
				return []rpc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, &listenSpec, commonFlags.RuntimeFlags(), reservedDispatcher)
			if err != nil {
				return nil, nil, nil, err
			}
			profileShutdown := func() {
				ac.Shutdown()
				shutdown()
			}
			return runtime, ctx, profileShutdown, nil
		}
	}
	listenSpec.AddressChooser = internal.IPAddressChooser

	runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, &listenSpec, commonFlags.RuntimeFlags(), reservedDispatcher)
	if err != nil {
		return nil, nil, shutdown, err
	}

	profileShutdown := func() {
		ac.Shutdown()
		shutdown()
	}
	return runtime, ctx, profileShutdown, nil
}
