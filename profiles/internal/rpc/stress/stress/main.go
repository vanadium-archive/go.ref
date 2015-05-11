// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"v.io/v23/context"
	"v.io/x/lib/cmdline2"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/profiles/internal/rpc/stress"
	_ "v.io/x/ref/profiles/static"
)

var cmdStopServers = &cmdline2.Command{
	Runner:   v23cmd.RunnerFunc(runStopServers),
	Name:     "stop",
	Short:    "Stop servers",
	Long:     "Stop servers",
	ArgsName: "<server> ...",
	ArgsLong: "<server> ... A list of servers to stop.",
}

func runStopServers(ctx *context.T, env *cmdline2.Env, args []string) error {
	if len(args) == 0 {
		return env.UsageErrorf("no server specified")
	}
	for _, server := range args {
		if err := stress.StressClient(server).Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	cmdRoot := &cmdline2.Command{
		Name:  "stress",
		Short: "Tool to stress/load test RPC",
		Long:  "Tool to stress/load test RPC by issuing randomly generated requests",
		Children: []*cmdline2.Command{
			cmdStressTest,
			cmdStressStats,
			cmdLoadTest,
			cmdStopServers,
		},
	}
	cmdline2.Main(cmdRoot)
}
