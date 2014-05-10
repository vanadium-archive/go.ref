package main

import (
	"fmt"
	"log"

	"veyron/examples/wspr_sample/sampled/lib"
	"veyron/lib/signals"
	"veyron2/rt"
)

func main() {
	// Create the runtime
	r := rt.Init()
	defer r.Shutdown()

	s, endpoint, err := lib.StartServer(r)
	if err != nil {
		log.Fatal("", err)
	}
	defer s.Stop()

	fmt.Printf("Listening at: %v\n", endpoint)
	<-signals.ShutdownOnSignals()
}
