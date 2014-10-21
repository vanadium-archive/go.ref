// This package provides a shell test during the security model transition.
package main

import (
	"flag"
	"fmt"
	"time"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

var runServer = flag.Bool("server", false, "If true, start a server. If false, start a client")

type service struct{}

func (service) Ping(call ipc.ServerCall) (string, error) {
	return fmt.Sprintf("ClientBlessings: %v\nClientPublicID: %v", call.RemoteBlessings(), call.RemoteID()), nil
}

type authorizer struct{}

func (authorizer) Authorize(security.Context) error { return nil }

func main() {
	r := rt.Init()
	defer r.Cleanup()

	if *runServer {
		startServer(r.NewServer())
	} else if len(flag.Args()) != 1 {
		vlog.Fatalf("Expected exactly 1 argument, got %d (%v)", len(flag.Args()), flag.Args())
	} else {
		ctx, _ := r.NewContext().WithDeadline(time.Now().Add(10 * time.Second))
		startClient(r.Client().StartCall(ctx, flag.Arg(0), "Ping", nil))
	}
}

func startServer(server ipc.Server, err error) {
	if err != nil {
		vlog.Fatal(err)
	}
	defer server.Stop()

	ep, err := server.ListenX(profiles.LocalListenSpec)
	if err != nil {
		vlog.Fatal(err)
	}
	fmt.Println("SERVER:", naming.JoinAddressName(ep.String(), ""))
	server.Serve("", ipc.LeafDispatcher(service{}, authorizer{}))
	<-signals.ShutdownOnSignals()
}

func startClient(call ipc.Call, err error) {
	if err != nil {
		vlog.Fatal(err)
	}
	var result string
	var apperr error
	if err = call.Finish(&result, &apperr); err != nil {
		vlog.Fatalf("ipc.Call.Finish error: %v", err)
	}
	if apperr != nil {
		vlog.Fatalf("Application error: %v", apperr)
	}
	fmt.Println(result)
}
