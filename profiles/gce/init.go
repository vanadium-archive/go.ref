// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build linux

// Package gce implements a profile for binaries that only run on Google Compute
// Engine.
package gce

import (
	"flag"
	"fmt"
	"net"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"

	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/profiles/internal"
	"v.io/x/ref/profiles/internal/gce"
	"v.io/x/ref/profiles/internal/lib/appcycle"
	"v.io/x/ref/profiles/internal/lib/websocket"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/ws"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/wsh"
	grt "v.io/x/ref/profiles/internal/rt"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterProfile(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if !gce.RunningOnGCE() {
		return nil, nil, nil, fmt.Errorf("GCE profile used on a non-GCE system")
	}

	if err := internal.ParseFlags(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	ac := appcycle.New()

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs: rpc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	if ip, err := gce.ExternalIPAddress(); err != nil {
		return nil, nil, nil, err
	} else {
		listenSpec.AddressChooser = func(network string, addrs []net.Addr) ([]net.Addr, error) {
			return []net.Addr{netstate.NewNetAddr("wsh", ip.String())}, nil
		}
	}

	runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, &listenSpec, commonFlags.RuntimeFlags(), nil)
	if err != nil {
		return nil, nil, shutdown, err
	}

	vlog.Log.VI(1).Infof("Initializing GCE profile.")

	profileShutdown := func() {
		ac.Shutdown()
		shutdown()
	}

	return runtime, ctx, profileShutdown, nil
}
