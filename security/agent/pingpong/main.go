package main

import (
	"flag"
	"fmt"

	"veyron.io/veyron/veyron/lib/signals"
	_ "veyron.io/veyron/veyron/profiles"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
)

var runServer = flag.Bool("server", false, "Whether to run in server mode")

type pongd struct{}

func (f *pongd) Ping(_ ipc.ServerContext, message string) (result string, err error) {
	return "pong", nil
}

func clientMain() {
	runtime := rt.Init()
	defer runtime.Cleanup()

	log := runtime.Logger()
	log.Info("Pinging...")

	s := PingPongClient("pingpong")
	pong, err := s.Ping(runtime.NewContext(), "ping")
	if err != nil {
		log.Fatal("error pinging: ", err)
	}
	fmt.Println(pong)
}

func serverMain() {
	r := rt.Init()
	log := r.Logger()
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("failure creating server: ", err)
	}
	log.Info("Waiting for ping")

	serverPong := PingPongServer(&pongd{})

	if endpoint, err := s.Listen(ipc.ListenSpec{Protocol: "tcp", Address: "127.0.0.1:0"}); err == nil {
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
	if *runServer {
		serverMain()
	} else {
		clientMain()
	}
}
