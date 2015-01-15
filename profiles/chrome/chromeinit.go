// Package chrome implements a profile for use within Chrome, in particular
// for use by Chrome extensions.
package chrome

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"

	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	grt "v.io/core/veyron/runtimes/google/rt"
)

func init() {
	veyron2.RegisterProfileInit(Init)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	runtime := &grt.RuntimeX{}
	ctx, shutdown, err := runtime.Init(ctx, nil)
	if err != nil {
		return nil, nil, shutdown, err
	}
	veyron2.GetLogger(ctx).VI(1).Infof("Initializing chrome profile.")
	return runtime, ctx, shutdown, nil
}
