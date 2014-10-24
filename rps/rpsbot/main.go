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

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	sflag "veyron.io/veyron/veyron/security/flag"

	"veyron.io/apps/rps"
	"veyron.io/apps/rps/common"
)

var (
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

	ep, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", roaming.ListenSpec, err)
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
