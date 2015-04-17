// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command rpsscorekeeper implements the ScoreKeeper interface.  It publishes
// itself as a score keeper for the rock-paper-scissors game and prints out all
// the score cards it receives to stdout.
package main

import (
	"flag"
	"fmt"
	"os"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
	"v.io/x/ref/examples/rps"
	"v.io/x/ref/examples/rps/internal"

	_ "v.io/x/ref/profiles/roaming"
)

var (
	aclFile = flag.String("acl-file", "", "file containing the JSON-encoded ACL")
)

type impl struct {
	ch chan rps.ScoreCard
}

func (i *impl) Record(ctx *context.T, call rpc.ServerCall, score rps.ScoreCard) error {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
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
	if err := server.Serve(fmt.Sprintf("rps/scorekeeper/%s", hostname), rps.ScoreKeeperServer(rpsService), internal.NewAuthorizer(*aclFile)); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	vlog.Infof("Listening on endpoint /%s", ep)

	for score := range ch {
		fmt.Print("======================\n", internal.FormatScoreCard(score))
	}
}
