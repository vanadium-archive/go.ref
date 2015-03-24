package profiles

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/profiles/internal"
	"v.io/x/ref/profiles/internal/lib/appcycle"
	"v.io/x/ref/profiles/internal/lib/websocket"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/ws"
	_ "v.io/x/ref/profiles/internal/rpc/protocols/wsh"
	grt "v.io/x/ref/profiles/internal/rt"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterProfileInit(Init)
	rpc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
	flags.SetDefaultHostPort(":0")
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlags(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	ac := appcycle.New()

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs:          rpc.ListenAddrs(lf.Addrs),
		AddressChooser: internal.IPAddressChooser,
		Proxy:          lf.ListenProxy,
	}

	runtime, ctx, shutdown, err := grt.Init(ctx,
		ac,
		nil,
		&listenSpec,
		commonFlags.RuntimeFlags(),
		nil)
	if err != nil {
		return nil, nil, nil, err
	}
	vlog.Log.VI(1).Infof("Initializing generic profile.")

	profileShutdown := func() {
		ac.Shutdown()
		shutdown()
	}
	return runtime, ctx, profileShutdown, nil
}
