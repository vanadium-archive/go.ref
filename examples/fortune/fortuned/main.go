// Binary fortuned is a simple implementation of the fortune service.  See
// http://go/veyron:code-lab for a thorough explanation.
package main

import (
	"fmt"
	"log"
	"math/rand"

	"veyron/examples/fortune"
	"veyron/lib/signals"
	vflag "veyron/security/flag"

	"veyron2/ipc"
	"veyron2/rt"
)

type fortuned struct {
	// The set of all fortunes.
	fortunes []string
	// Used to pick a random index in 'fortunes'.
	random *rand.Rand
}

// newFortuned creates a new fortuned and seeds it with a few fortunes.
func newFortuned() *fortuned {
	return &fortuned{
		fortunes: []string{
			"You will reach the height of success in whatever you do.",
			"You have remarkable power which you are not using.",
			"Everything will now come your way.",
		},
		random: rand.New(rand.NewSource(99)),
	}
}

func (f *fortuned) Get(_ ipc.ServerContext) (Fortune string, err error) {
	return f.fortunes[f.random.Intn(len(f.fortunes))], nil
}

func (f *fortuned) Add(_ ipc.ServerContext, Fortune string) error {
	f.fortunes = append(f.fortunes, Fortune)
	return nil
}

func main() {
	// Create the runtime
	r := rt.Init()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("failure creating server: ", err)
	}

	// Create the fortune server stub.
	serverFortune := fortune.NewServerFortune(newFortuned())

	// Register the "fortune" prefix with a fortune dispatcher.
	if err := s.Register("fortune", ipc.SoloDispatcher(serverFortune, vflag.NewAuthorizerOrDie())); err != nil {
		log.Fatal("error registering service: ", err)
	}

	// Create an endpoint and begin listening.
	if endpoint, err := s.Listen("tcp", "127.0.0.1:0"); err == nil {
		fmt.Printf("Listening at: %v\n", endpoint)
	} else {
		log.Fatal("error listening to service: ", err)
	}

	// Wait forever.
	<-signals.ShutdownOnSignals()
}
