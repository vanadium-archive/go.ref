// +build linux

// Package gce provides a profile for Google Compute Engine and should be
// used by binaries that only ever expect to be run on GCE.
package gce

import (
	"flag"
	"fmt"
	"net"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/appcycle"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/netstate"
	"v.io/x/ref/lib/websocket"
	"v.io/x/ref/profiles/internal"
	"v.io/x/ref/profiles/internal/gce"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/ws"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/wsh"
	grt "v.io/x/ref/profiles/internal/rt"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterProfileInit(Init)
	ipc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
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
	listenSpec := ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	if ip, err := gce.ExternalIPAddress(); err != nil {
		return nil, nil, nil, err
	} else {
		listenSpec.AddressChooser = func(network string, addrs []ipc.Address) ([]ipc.Address, error) {
			return []ipc.Address{&netstate.AddrIfc{&net.IPAddr{IP: ip}, "gce-nat", nil}}, nil
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
