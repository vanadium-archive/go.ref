package main

import (
	"flag"
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/profiles"
)

type pongd struct{}

func (f *pongd) Ping(call rpc.ServerCall, message string) (result string, err error) {
	client, _ := security.BlessingNames(call.Context(), security.CallSideRemote)
	server, _ := security.BlessingNames(call.Context(), security.CallSideLocal)
	return fmt.Sprintf("pong (client:%v server:%v)", client, server), nil
}

func clientMain(ctx *context.T, server string) {
	fmt.Println("Pinging...")
	pong, err := PingPongClient(server).Ping(ctx, "ping")
	if err != nil {
		vlog.Fatal("error pinging: ", err)
	}
	fmt.Println(pong)
}

func serverMain(ctx *context.T) {
	s, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatal("failure creating server: ", err)
	}
	vlog.Info("Waiting for ping")
	spec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	if endpoints, err := s.Listen(spec); err == nil {
		fmt.Printf("NAME=%v\n", endpoints[0].Name())
	} else {
		vlog.Fatal("error listening to service:", err)
	}
	// Provide an empty name, no need to mount on any mounttable.
	//
	// Use the default authorization policy (nil authorizer), which will
	// only authorize clients if the blessings of the client is a prefix of
	// that of the server or vice-versa.
	if err := s.Serve("", PingPongServer(&pongd{}), nil); err != nil {
		vlog.Fatal("error serving service: ", err)
	}

	// Wait forever.
	<-signals.ShutdownOnSignals(ctx)
}

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	if len(flag.Args()) == 0 {
		serverMain(ctx)
	} else if len(flag.Args()) == 1 {
		clientMain(ctx, flag.Args()[0])
	} else {
		vlog.Fatalf("Expected at most one argument, the object name of the server. Got %v", flag.Args())
	}
}
