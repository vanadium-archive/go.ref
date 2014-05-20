// Binary fortune is a simple client of the fortune service.  See
// http://go/veyron:code-lab for a thorough explanation.
package main

import (
	"flag"
	"fmt"

	"veyron/examples/fortune"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"
)

var (
	// TODO(rthellend): Remove the address flag when the config manager is working.
	address       = flag.String("address", "", "the address/endpoint of the fortune server")
	newFortune    = flag.String("new_fortune", "", "an optional, new fortune to add to the server's set")
	serverPattern = flag.String("server_pattern", "*", "server_pattern is an optional pattern for the expected identity of the fortune server. Example: the pattern \"myorg/fortune\" matches identities with names \"myorg/fortune\" or \"myorg\". If the flag is absent then the default pattern \"*\" matches all identities.")
)

func main() {
	runtime := rt.Init()
	log := runtime.Logger()
	if *address == "" {
		log.Fatal("--address needs to be specified")
	}

	// Construct a new stub that binds to serverEndpoint without
	// using the name service
	s, err := fortune.BindFortune(naming.JoinAddressNameFixed(*address, "fortune"))
	if err != nil {
		log.Fatal("error binding to server: ", err)
	}

	// Issue a Get() rpc specifying the provided pattern for the server's identity as
	// an option. If no pattern is provided then the default pattern "*" matches all
	// identities.
	fortune, err := s.Get(veyron2.RemoteID(*serverPattern))
	if err != nil {
		log.Fatal("error getting fortune: ", err)
	}
	fmt.Println(fortune)

	// If the user specified --new_fortune, Add() it to the server’s set of fortunes.
	// Again, the provided pattern on the server's identity is passed as an option.
	if *newFortune != "" {
		if err := s.Add(*newFortune, veyron2.RemoteID(*serverPattern)); err != nil {
			log.Fatal("error adding fortune: ", err)
		}
	}
}
