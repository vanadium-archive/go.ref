// Package chrome implements a profile for use within Chrome, in particular
// for use by Chrome extensions.
package chrome

import (
	"flag"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/vlog"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/websocket"
	"v.io/core/veyron/profiles/internal"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/core/veyron/runtimes/google/ipc/protocols/wsh_nacl"
	grt "v.io/core/veyron/runtimes/google/rt"
)

var commonFlags *flags.Flags

func init() {
	v23.RegisterProfileInit(Init)
	ipc.RegisterUnknownProtocol("wsh", websocket.Dial, websocket.Listener)
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlags(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	protocols := []string{"wsh", "ws"}
	listenSpec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{Protocol: "ws", Address: ""}}}
	runtime, ctx, shutdown, err := grt.Init(ctx, nil, protocols, &listenSpec, commonFlags.RuntimeFlags(), nil)
	if err != nil {
		return nil, nil, shutdown, err
	}
	vlog.Log.VI(1).Infof("Initializing chrome profile.")
	return runtime, ctx, shutdown, nil
}
