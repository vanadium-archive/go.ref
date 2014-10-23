// +build OMIT
package main

import (
	"fmt"

	"veyron.io/veyron/veyron/lib/signals"
	_ "veyron.io/veyron/veyron/profiles"
	sflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"

	"pingpong"
)

type pongd struct{}

func (f *pongd) Ping(_ ipc.ServerContext, message string) (result string, err error) {
	fmt.Println(message)
	return "PONG", nil
}

func main() {
	r := rt.Init()
	log := r.Logger()
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("failure creating server: ", err)
	}
	log.Info("Waiting for ping")

	serverPong := pingpong.NewServerPingPong(&pongd{})

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
