package static

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/netstate"
	"v.io/x/ref/profiles/internal"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/ws"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/wsh"
	"v.io/x/ref/profiles/internal/lib/appcycle"
	"v.io/x/ref/profiles/internal/lib/websocket"
	grt "v.io/x/ref/profiles/internal/rt"
	"v.io/x/ref/services/mgmt/debug"

	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "v.io/x/ref/security/flag"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterProfileInit(Init)
	ipc.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlags(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	lf := commonFlags.ListenFlags()
	listenSpec := ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}
	reservedDispatcher := debug.NewDispatcher(vlog.Log.LogDir, sflag.NewAuthorizerOrDie())

	ac := appcycle.New()

	// Our address is private, so we test for running on GCE and for its 1:1 NAT
	// configuration. GCEPublicAddress returns a non-nil addr if we are running on GCE.
	if !internal.HasPublicIP(vlog.Log) {
		if addr := internal.GCEPublicAddress(vlog.Log); addr != nil {
			listenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
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
