// Binary signallingserver is a simple implementation of the BoxSignalling
// rendezvous service.
package main

import (
	"log"

	"veyron/examples/boxes"
	"veyron/lib/signals"

	"veyron2/ipc"
	"veyron2/rt"
)

const (
	signallingServiceName = "signalling"
	signallingServicePort = ":8509"
)

type boxAppEndpoint string

func (b *boxAppEndpoint) Add(_ ipc.ServerContext, Endpoint string) (err error) {
	*b = boxAppEndpoint(Endpoint)
	log.Printf("Added endpoint %v to signalling service", *b)
	return nil
}

func (b *boxAppEndpoint) Get(_ ipc.ServerContext) (Endpoint string, err error) {
	log.Printf("Returning endpoints:%v from signalling service", *b)
	return string(*b), nil
}

func main() {
	r := rt.Init()

	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		log.Fatal("failed NewServer: ", err)
	}

	var boxApp boxAppEndpoint
	srv := boxes.NewServerBoxSignalling(&boxApp)

	// Create an endpoint and begin listening.
	if endPt, err := s.Listen("tcp", signallingServicePort); err == nil {
		log.Printf("SignallingService listening at: %v\n", endPt)
	} else {
		log.Fatal("failed Listen: ", err)
	}

	if err := s.Serve("/"+signallingServiceName, ipc.SoloDispatcher(srv, nil)); err != nil {
		log.Fatal("failed Serve:", err)
	}

	<-signals.ShutdownOnSignals()
}
