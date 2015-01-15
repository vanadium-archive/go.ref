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

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	sflag "v.io/core/veyron/security/flag"

	"v.io/apps/rps"
	"v.io/apps/rps/common"
)

var (
	name     = flag.String("name", "", "identifier to publish itself as (defaults to user@hostname)")
	numGames = flag.Int("num-games", -1, "number of games to play (-1 means unlimited)")
)

func main() {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	auth := sflag.NewAuthorizerOrDie()
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	rand.Seed(time.Now().UnixNano())
	rpsService := NewRPS(ctx)

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
	if err := server.Serve(names[0], rps.RockPaperScissorsServer(rpsService), auth); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", names[0], err)
	}
	for _, n := range names[1:] {
		if err := server.AddName(n); err != nil {
			vlog.Fatalf("(%v) failed: %v", n, err)
		}
	}
	vlog.Infof("Listening on endpoint /%s (published as %v)", ep, names)

	go initiateGames(ctx, rpsService)
	<-signals.ShutdownOnSignals(ctx)
}

func initiateGames(ctx *context.T, rpsService *RPS) {
	for i := 0; i < *numGames || *numGames == -1; i++ {
		if err := rpsService.Player().InitiateGame(ctx); err != nil {
			vlog.Infof("Failed to initiate game: %v", err)
		}
	}
}
