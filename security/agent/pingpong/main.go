package main

import (
	"flag"
	"fmt"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles"
)

var runServer = flag.Bool("server", false, "Whether to run in server mode")

type pongd struct{}

func (f *pongd) Ping(_ ipc.ServerContext, message string) (result string, err error) {
	return "pong", nil
}

func clientMain(ctx *context.T) {
	vlog.Info("Pinging...")

	s := PingPongClient("pingpong")
	pong, err := s.Ping(ctx, "ping")
	if err != nil {
		vlog.Fatal("error pinging: ", err)
	}
	fmt.Println(pong)
}

func serverMain(ctx *context.T) {
	s, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Fatal("failure creating server: ", err)
	}
	vlog.Info("Waiting for ping")

	serverPong := PingPongServer(&pongd{})

	spec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	if endpoint, err := s.Listen(spec); err == nil {
		fmt.Printf("Listening at: %v\n", endpoint)
	} else {
		vlog.Fatal("error listening to service: ", err)
	}

	if err := s.Serve("pingpong", serverPong, allowEveryone{}); err != nil {
		vlog.Fatal("error serving service: ", err)
	}

	// Wait forever.
	<-signals.ShutdownOnSignals(ctx)
}

type allowEveryone struct{}

func (allowEveryone) Authorize(security.Context) error { return nil }

func main() {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	if *runServer {
		serverMain(ctx)
	} else {
		clientMain(ctx)
	}
}
