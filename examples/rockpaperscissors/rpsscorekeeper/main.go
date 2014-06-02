// rpsscorekeeper is a simple implementation of the ScoreKeeper service. It
// publishes itself as a score keeper for the rock-paper-scissors game and
// prints out all the score cards it receives to stdout.
package main

import (
	"flag"
	"fmt"
	"os"

	rps "veyron/examples/rockpaperscissors"
	"veyron/examples/rockpaperscissors/common"
	sflag "veyron/security/flag"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/vlog"
)

var (
	// TODO(rthellend): Remove the address and protocol flags when the config manager is working.
	protocol = flag.String("protocol", "tcp", "network to listen on. For example, set to 'veyron' and set --address to the endpoint/name of a proxy to have this service proxied.")
	address  = flag.String("address", ":0", "address to listen on")
)

type impl struct {
	ch chan rps.ScoreCard
}

func (i *impl) Record(ctx ipc.ServerContext, score rps.ScoreCard) error {
	vlog.VI(1).Infof("Record (%+v) from %s", score, ctx.RemoteID())
	i.ch <- score
	return nil
}

func main() {
	r := rt.Init()
	defer r.Shutdown()
	server, err := r.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	defer server.Stop()

	ch := make(chan rps.ScoreCard)
	rpsService := &impl{ch}

	if err := server.Register("", ipc.SoloDispatcher(rps.NewServerScoreKeeper(rpsService), sflag.NewAuthorizerOrDie())); err != nil {
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
	if err := server.Publish(fmt.Sprintf("rps/scorekeeper/%s", hostname)); err != nil {
		vlog.Fatalf("Publish failed: %v", err)
	}
	vlog.Infof("Listening on endpoint /%s", ep)

	for score := range ch {
		fmt.Print("======================\n", common.FormatScoreCard(score))
	}
}
