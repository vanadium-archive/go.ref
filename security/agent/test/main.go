package main

import (
	"flag"
	"fmt"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/signals"
	_ "veyron.io/veyron/veyron/profiles"
	sflag "veyron.io/veyron/veyron/security/flag"
)

var runServer = flag.Bool("server", false, "Whether to run in server mode")

type pongd struct{}

func (f *pongd) Ping(_ ipc.ServerContext, message string) (result string, err error) {
	return "pong", nil
}

func clientMain() {
	runtime := rt.Init()
	log := runtime.Logger()
	log.Info("Pinging...")

	s, err := BindPingPong("pingpong")
	if err != nil {
		log.Fatal("error binding to server: ", err)
	}

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

	serverPong := NewServerPingPong(&pongd{})

	if endpoint, err := s.Listen("tcp", "127.0.0.1:0"); err == nil {
		fmt.Printf("Listening at: %v\n", endpoint)
	} else {
		log.Fatal("error listening to service: ", err)
	}

	if err := s.Serve("pingpong", ipc.LeafDispatcher(serverPong, sflag.NewAuthorizerOrDie())); err != nil {
		log.Fatal("error serving service: ", err)
	}

	// Wait forever.
	<-signals.ShutdownOnSignals()
}

func main() {
	flag.Parse()
	if *runServer {
		serverMain()
	} else {
		clientMain()
	}
}
