package runtime

import (
	"fmt"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vlog"

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
)

// dispatcher is a simple no-op dispatcher we use for setting up example
// servers.
type dispatcher struct{}

func (dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return nil, nil, nil
}

// makeServer sets up a simple dummy server.
func makeServer() ipc.Server {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("r.NewServer error: %s", err)
	}
	if err := server.Register("", new(dispatcher)); err != nil {
		vlog.Fatalf("server.Register error: %s", err)
	}
	if _, err := server.Listen("tcp", "127.0.0.1:0"); err != nil {
		vlog.Fatalf("server.Listen error: %s", err)
	}
	return server
}

// remoteCmdLoop listens on stdin and interprets commands sent over stdin (from
// the parent process).
func remoteCmdLoop() func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			switch blackbox.ReadLineFromStdin() {
			case "stop":
				rt.R().Stop()
			case "forcestop":
				fmt.Println("straight exit")
				rt.R().ForceStop()
			case "close":
				return
			}
		}
	}()
	return func() { <-done }
}
