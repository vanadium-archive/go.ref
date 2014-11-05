// Command rpsscorekeeper is a simple implementation of the
// ScoreKeeper service. It publishes itself as a score keeper for the
// rock-paper-scissors game and prints out all the score cards it
// receives to stdout.
package main

import (
	"fmt"
	"os"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/profiles/roaming"
	sflag "veyron.io/veyron/veyron/security/flag"

	"veyron.io/apps/rps"
	"veyron.io/apps/rps/common"
)

type impl struct {
	ch chan rps.ScoreCard
}

func (i *impl) Record(ctx ipc.ServerContext, score rps.ScoreCard) error {
	vlog.VI(1).Infof("Record (%+v) from %v", score, ctx.RemoteBlessings().ForContext(ctx))
	i.ch <- score
	return nil
}

func main() {
	r := rt.Init()
	defer r.Cleanup()
	server, err := r.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	ch := make(chan rps.ScoreCard)
	rpsService := &impl{ch}

	ep, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", roaming.ListenSpec, err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("os.Hostname failed: %v", err)
	}
	if err := server.Serve(fmt.Sprintf("rps/scorekeeper/%s", hostname), rps.NewServerScoreKeeper(rpsService), sflag.NewAuthorizerOrDie()); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	vlog.Infof("Listening on endpoint /%s", ep)

	for score := range ch {
		fmt.Print("======================\n", common.FormatScoreCard(score))
	}
}
