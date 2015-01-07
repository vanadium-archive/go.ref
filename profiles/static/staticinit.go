package static

import (
	"flag"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"

	"v.io/core/veyron/lib/appcycle"
	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/profiles/internal"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh"
	grt "v.io/core/veyron/runtimes/google/rt"
	"v.io/core/veyron/services/mgmt/debug"

	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "v.io/core/veyron/security/flag"
)

var (
	commonFlags *flags.Flags
)

func init() {
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Listen)
	// TODO(suharshs): Register the Init function here.
	veyron2.RegisterProfileInit(Init)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T) {
	runtime := &grt.RuntimeX{}
	ctx = runtime.Init(ctx)
	log := runtime.GetLogger(ctx)

	ctx = runtime.SetReservedNameDispatcher(ctx, debug.NewDispatcher(log.LogDir(), sflag.NewAuthorizerOrDie(), runtime.GetVtraceStore(ctx)))

	lf := commonFlags.ListenFlags()
	listenSpec := ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}

	ac := appcycle.New()
	ctx = runtime.SetAppCycle(ctx, ac)
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			ac.Shutdown()
		}()
	}

	// Our address is private, so we test for running on GCE and for its 1:1 NAT
	// configuration. GCEPublicAddress returns a non-nil addr if we are running on GCE.
	if !internal.HasPublicIP(log) {
		if addr := internal.GCEPublicAddress(log); addr != nil {
			listenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			return runtime, ctx
		}
	}
	listenSpec.AddressChooser = internal.IPAddressChooser
	ctx = runtime.SetListenSpec(ctx, listenSpec)
	return runtime, ctx
}
