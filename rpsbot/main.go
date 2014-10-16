// Command rpsbot is a binary that runs the fully automated
// implementation of the RockPaperScissors service, which includes all
// three roles involved in the game of rock-paper-scissors. It
// publishes itself as player, judge, and scorekeeper. Then, it
// initiates games with other players, in a loop. As soon as one game
// is over, it starts a new one.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"time"

	"veyron.io/examples/rps"
	"veyron.io/examples/rps/common"
	"veyron.io/veyron/veyron/lib/signals"
	sflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on. For example, set to 'veyron' and set --address to the endpoint/name of a proxy to have this tunnel service proxied.")
	address  = flag.String("address", ":0", "address to listen on")
	name     = flag.String("name", "", "identifier to publish itself as (defaults to user@hostname)")
	numGames = flag.Int("num-games", -1, "number of games to play (-1 means unlimited)")
)

func main() {
	r := rt.Init()
	defer r.Cleanup()
	auth := sflag.NewAuthorizerOrDie()
	server, err := r.NewServer(options.DebugAuthorizer{auth})
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	rand.Seed(time.Now().UnixNano())
	rpsService := NewRPS()

	dispatcher := ipc.LeafDispatcher(rps.NewServerRockPaperScissors(rpsService), auth)

	ep, err := server.Listen(*protocol, *address)
	if err != nil {
		vlog.Fatalf("Listen(%q, %q) failed: %v", "tcp", *address, err)
	}
	if *name == "" {
		*name = common.CreateName()
	}
	names := []string{
		fmt.Sprintf("rps/judge/%s", *name),
		fmt.Sprintf("rps/player/%s", *name),
		fmt.Sprintf("rps/scorekeeper/%s", *name),
	}
	for _, n := range names {
		if err := server.Serve(n, dispatcher); err != nil {
			vlog.Fatalf("Serve(%v) failed: %v", n, err)
		}
	}
	vlog.Infof("Listening on endpoint /%s (published as %v)", ep, names)

	ctx := r.NewContext()
	go initiateGames(ctx, rpsService)
	<-signals.ShutdownOnSignals()
}

func initiateGames(ctx context.T, rpsService *RPS) {
	for i := 0; i < *numGames || *numGames == -1; i++ {
		if err := rpsService.Player().InitiateGame(ctx); err != nil {
			vlog.Infof("Failed to initiate game: %v", err)
		}
	}
}
