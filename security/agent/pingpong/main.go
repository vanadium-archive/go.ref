package main

import (
	"flag"
	"fmt"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/security"
	"v.io/veyron/veyron2/vlog"

	"v.io/veyron/veyron/lib/signals"
	_ "v.io/veyron/veyron/profiles"
)

var runServer = flag.Bool("server", false, "Whether to run in server mode")

type pongd struct{}

func (f *pongd) Ping(_ ipc.ServerContext, message string) (result string, err error) {
	return "pong", nil
}

func clientMain(runtime veyron2.Runtime) {
	log := runtime.Logger()
	log.Info("Pinging...")

	s := PingPongClient("pingpong")
	pong, err := s.Ping(runtime.NewContext(), "ping")
	if err != nil {
		log.Fatal("error pinging: ", err)
	}
	fmt.Println(pong)
}

func serverMain(r veyron2.Runtime) {
	log := r.Logger()
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("failure creating server: ", err)
	}
	log.Info("Waiting for ping")

	serverPong := PingPongServer(&pongd{})

	spec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	if endpoint, err := s.Listen(spec); err == nil {
		fmt.Printf("Listening at: %v\n", endpoint)
	} else {
		log.Fatal("error listening to service: ", err)
	}

	if err := s.Serve("pingpong", serverPong, allowEveryone{}); err != nil {
		log.Fatal("error serving service: ", err)
	}

	// Wait forever.
	<-signals.ShutdownOnSignals(r)
}

type allowEveryone struct{}

func (allowEveryone) Authorize(security.Context) error { return nil }

func main() {
	flag.Parse()

	runtime, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %s", err)
	}
	defer runtime.Cleanup()

	if *runServer {
		serverMain(runtime)
	} else {
		clientMain(runtime)
	}
}
