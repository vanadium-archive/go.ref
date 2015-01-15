package profiles

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"

	"v.io/core/veyron/lib/appcycle"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh"
	grt "v.io/core/veyron/runtimes/google/rt"
)

func init() {
	veyron2.RegisterProfileInit(Init)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	runtime, ctx, shutdown, err := grt.Init(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	veyron2.GetLogger(ctx).VI(1).Infof("Initializing generic profile.")

	ac := appcycle.New()
	ctx = runtime.SetAppCycle(ctx, ac)

	profileShutdown := func() {
		shutdown()
		ac.Shutdown()
	}
	return runtime, ctx, profileShutdown, nil
}
