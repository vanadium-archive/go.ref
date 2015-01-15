package gce

import (
	"flag"
	"fmt"
	"net"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"

	"v.io/core/veyron/lib/appcycle"
	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/profiles/internal/gce"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh"
	grt "v.io/core/veyron/runtimes/google/rt"
)

var (
	commonFlags *flags.Flags
)

func init() {
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Listen)
	veyron2.RegisterProfileInit(Init)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	if !gce.RunningOnGCE() {
		return nil, nil, nil, fmt.Errorf("GCE profile used on a non-GCE system")
	}

	runtime := &grt.RuntimeX{}
	ctx, shutdown, err := runtime.Init(ctx, nil)
	if err != nil {
		return nil, nil, shutdown, err
	}
	veyron2.GetLogger(ctx).VI(1).Infof("Initializing GCE profile.")

	lf := commonFlags.ListenFlags()
	listenSpec := ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	if ip, err := gce.ExternalIPAddress(); err != nil {
		return nil, nil, shutdown, err
	} else {
		listenSpec.AddressChooser = func(network string, addrs []ipc.Address) ([]ipc.Address, error) {
			return []ipc.Address{&netstate.AddrIfc{&net.IPAddr{IP: ip}, "gce-nat", nil}}, nil
		}
	}
	ctx = runtime.SetListenSpec(ctx, listenSpec)

	ac := appcycle.New()
	ctx = runtime.SetAppCycle(ctx, ac)

	profileShutdown := func() {
		shutdown()
		ac.Shutdown()
	}

	return runtime, ctx, profileShutdown, nil
}
