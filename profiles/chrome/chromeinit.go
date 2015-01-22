// Package chrome implements a profile for use within Chrome, in particular
// for use by Chrome extensions.
package chrome

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc/stream"

	"v.io/core/veyron/lib/websocket"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	grt "v.io/core/veyron/runtimes/google/rt"
)

func init() {
	veyron2.RegisterProfileInit(Init)
	stream.RegisterUnknownProtocol("wsh", websocket.Dial, websocket.Listener)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	runtime, ctx, shutdown, err := grt.Init(ctx, nil, nil, nil, nil)
	if err != nil {
		return nil, nil, shutdown, err
	}
	runtime.GetLogger(ctx).VI(1).Infof("Initializing chrome profile.")
	return runtime, ctx, shutdown, nil
}
