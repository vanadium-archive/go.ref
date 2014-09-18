// +build OMIT
package main

import (
	"fmt"
	"pingpong"
	"veyron.io/veyron/veyron2/rt"
)

func main() {
	runtime := rt.Init()
	log := runtime.Logger()

	s, err := pingpong.BindPingPong("pingpong")
	if err != nil {
		log.Fatal("error binding to server: ", err)
	}

	pong, err := s.Ping(runtime.NewContext(), "PING")
	if err != nil {
		log.Fatal("error pinging: ", err)
	}
	fmt.Println(pong)
}
