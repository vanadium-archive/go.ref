// Package chrome implements a profile for use within Chrome, in particular
// for use by Chrome extensions.
package chrome

import (
	"flag"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/websocket"
	"v.io/core/veyron/profiles/internal"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh_nacl"
	grt "v.io/core/veyron/runtimes/google/rt"
)

var commonFlags *flags.Flags

func init() {
	veyron2.RegisterProfileInit(Init)
	stream.RegisterUnknownProtocol("wsh", websocket.Dial, websocket.Listener)
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
}

func Init(ctx *context.T) (veyron2.RuntimeX, *context.T, veyron2.Shutdown, error) {
	if err := internal.ParseFlags(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	runtime, ctx, shutdown, err := grt.Init(ctx, nil, nil, nil, commonFlags.RuntimeFlags(), nil)
	if err != nil {
		return nil, nil, shutdown, err
	}
	vlog.Log.VI(1).Infof("Initializing chrome profile.")
	return runtime, ctx, shutdown, nil
}
