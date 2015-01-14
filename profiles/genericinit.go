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

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, error) {
	var err error
	runtime := &grt.RuntimeX{}
	ctx, err = runtime.Init(ctx, nil)
	if err != nil {
		return nil, nil, err
	}

	ac := appcycle.New()
	ctx = runtime.SetAppCycle(ctx, ac)
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			ac.Shutdown()
		}()
	}
	return runtime, ctx, nil
}
