package profiles

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"

	"v.io/core/veyron/lib/appcycle"
	"v.io/core/veyron/profiles/internal"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh"
	grt "v.io/core/veyron/runtimes/google/rt"
)

func init() {
	veyron2.RegisterProfileInit(Init)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	ac := appcycle.New()

	runtime, ctx, shutdown, err := grt.Init(ctx,
		ac,
		nil,
		&ipc.ListenSpec{
			Addrs:          ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}},
			AddressChooser: internal.IPAddressChooser,
		},
		nil)
	if err != nil {
		return nil, nil, nil, err
	}
	runtime.GetLogger(ctx).VI(1).Infof("Initializing generic profile.")

	profileShutdown := func() {
		ac.Shutdown()
		shutdown()
	}
	return runtime, ctx, profileShutdown, nil
}
