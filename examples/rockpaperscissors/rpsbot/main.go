// rpsbot is a binary that runs the fully automated implementation of the
// RockPaperScissors service, which includes all three roles involved in the
// game of rock-paper-scissors. It publishes itself as player, judge, and
// scorekeeper. Then, it initiates games with other players, in a loop. As soon
// as one game is over, it starts a new one.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	rps "veyron/examples/rockpaperscissors"
	"veyron/examples/rockpaperscissors/impl"
	"veyron/lib/signals"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vlog"
)

var (
	protocol = flag.String("protocol", "tcp", "network to listen on. For example, set to 'veyron' and set --address to the endpoint/name of a proxy to have this service proxied.")
	address  = flag.String("address", ":0", "address to listen on")

	users = flag.String("users", "", "A comma-separated list of principal patterns allowed to use this service.")
)

func authorizer() security.Authorizer {
	ACL := make(security.ACL)
	principals := strings.Split(*users, ",")
	for _, p := range principals {
		ACL[security.PrincipalPattern(p)] = security.LabelSet(security.AdminLabel)
	}
	return security.NewACLAuthorizer(ACL)
}

func main() {
	r := rt.Init()
	defer r.Shutdown()
	server, err := r.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	rand.Seed(time.Now().UTC().UnixNano())
	rpsService := impl.NewRPS(r.MountTable())

	if err := server.Register("", ipc.SoloDispatcher(rps.NewServerRockPaperScissors(rpsService), authorizer())); err != nil {
		vlog.Fatalf("Register failed: %v", err)
	}
	ep, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatalf("Listen(%q, %q) failed: %v", "tcp", *address, err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("os.Hostname failed: %v", err)
	}
	names := []string{
		fmt.Sprintf("rps/judge/%s", hostname),
		fmt.Sprintf("rps/player/%s", hostname),
		fmt.Sprintf("rps/scorekeeper/%s", hostname),
	}
	for _, n := range names {
		if err := server.Publish(n); err != nil {
			vlog.Fatalf("Publish(%v) failed: %v", n, err)
		}
	}
	vlog.Infof("Listening on endpoint /%s (published as %v)", ep, names)

	go initiateGames(rpsService)
	<-signals.ShutdownOnSignals()
}

func initiateGames(rpsService *impl.RPS) {
	for {
		if err := rpsService.Player().InitiateGame(); err != nil {
			vlog.Infof("Failed to initiate game: %v", err)
		}
	}
}
