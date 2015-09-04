// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"fmt"
	"os"

	"v.io/x/lib/cmdline"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"

	"v.io/v23"
	"v.io/x/ref/examples/rps"
	"v.io/x/ref/examples/rps/internal"
	"v.io/x/ref/lib/v23cmd"

	_ "v.io/x/ref/runtime/factories/roaming"
)

var aclFile string

func main() {
	cmdRoot.Flags.StringVar(&aclFile, "acl-file", "", "File containing JSON-encoded Permissions.")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

var cmdRoot = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runScoreKeeper),
	Name:   "rpsscorekeeper",
	Short:  "Implements the ScoreKeeper interface",
	Long: `
Command rpsscorekeeper implements the ScoreKeeper interface.  It publishes
itself as a score keeper for the rock-paper-scissors game and prints out all the
score cards it receives to stdout.
`,
}

type impl struct {
	ch chan rps.ScoreCard
}

func (i *impl) Record(ctx *context.T, call rpc.ServerCall, score rps.ScoreCard) error {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.VI(1).Infof("Record (%+v) from %v", score, b)
	i.ch <- score
	return nil
}

func runScoreKeeper(ctx *context.T, env *cmdline.Env, args []string) error {
	ch := make(chan rps.ScoreCard)
	rpsService := &impl{ch}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("os.Hostname failed: %v", err)
	}
	name := fmt.Sprintf("rps/scorekeeper/%s", hostname)
	service := rps.ScoreKeeperServer(rpsService)
	authorizer := internal.NewAuthorizer(aclFile)
	ctx, server, err := v23.WithNewServer(ctx, name, service, authorizer)
	if err != nil {
		return fmt.Errorf("NewServer failed: %v", err)
	}

	ctx.Infof("Listening on endpoint /%s", server.Status().Endpoints[0])

	for score := range ch {
		fmt.Print("======================\n", internal.FormatScoreCard(score))
	}
	return nil
}
