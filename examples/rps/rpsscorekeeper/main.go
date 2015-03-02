// Command rpsscorekeeper is a simple implementation of the
// ScoreKeeper service. It publishes itself as a score keeper for the
// rock-paper-scissors game and prints out all the score cards it
// receives to stdout.
package main

import (
	"fmt"
	"os"

	"v.io/v23"
	"v.io/v23/ipc"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles/roaming"
	sflag "v.io/x/ref/security/flag"

	"v.io/x/ref/examples/rps"
	"v.io/x/ref/examples/rps/common"
)

type impl struct {
	ch chan rps.ScoreCard
}

func (i *impl) Record(call ipc.ServerCall, score rps.ScoreCard) error {
	b, _ := call.RemoteBlessings().ForCall(call)
	vlog.VI(1).Infof("Record (%+v) from %v", score, b)
	i.ch <- score
	return nil
}

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	ch := make(chan rps.ScoreCard)
	rpsService := &impl{ch}

	listenSpec := v23.GetListenSpec(ctx)
	ep, err := server.Listen(listenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", listenSpec, err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("os.Hostname failed: %v", err)
	}
	if err := server.Serve(fmt.Sprintf("rps/scorekeeper/%s", hostname), rps.ScoreKeeperServer(rpsService), sflag.NewAuthorizerOrDie()); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	vlog.Infof("Listening on endpoint /%s", ep)

	for score := range ch {
		fmt.Print("======================\n", common.FormatScoreCard(score))
	}
}
