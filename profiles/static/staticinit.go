package static

import (
	"flag"
	"os"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/appcycle"
	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/lib/websocket"
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
	veyron2.RegisterProfileInit(Init)
	stream.RegisterUnknownProtocol("wsh", websocket.HybridDial, websocket.HybridListener)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	log := vlog.Log

	handle, err := exec.GetChildHandle()
	switch err {
	case exec.ErrNoVersion:
		// The process has not been started through the veyron exec
		// library. No further action is needed.
	case nil:
		// The process has been started through the veyron exec
		// library.
	default:
		return nil, nil, nil, err
	}
	var execConfig map[string]string
	if handle != nil {
		execConfig = handle.Config.Dump()
	}
	commonFlags.Parse(os.Args[1:], execConfig)
	lf := commonFlags.ListenFlags()
	listenSpec := ipc.ListenSpec{
		Addrs: ipc.ListenAddrs(lf.Addrs),
		Proxy: lf.ListenProxy,
	}
	reservedDispatcher := debug.NewDispatcher(log.LogDir(), sflag.NewAuthorizerOrDie())

	ac := appcycle.New()

	// Our address is private, so we test for running on GCE and for its 1:1 NAT
	// configuration. GCEPublicAddress returns a non-nil addr if we are running on GCE.
	if !internal.HasPublicIP(log) {
		if addr := internal.GCEPublicAddress(log); addr != nil {
			listenSpec.AddressChooser = func(string, []ipc.Address) ([]ipc.Address, error) {
				return []ipc.Address{&netstate.AddrIfc{addr, "nat", nil}}, nil
			}
			runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, &listenSpec, reservedDispatcher)
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

	runtime, ctx, shutdown, err := grt.Init(ctx, ac, nil, &listenSpec, reservedDispatcher)
	if err != nil {
		return nil, nil, shutdown, err
	}

	profileShutdown := func() {
		ac.Shutdown()
		shutdown()
	}
	return runtime, ctx, profileShutdown, nil
}
