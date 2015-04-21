// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"

	"v.io/v23"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/profiles/internal/rpc/stress"
	_ "v.io/x/ref/profiles/static"
)

var cmdStopServers = &cmdline.Command{
	Run:      runStopServers,
	Name:     "stop",
	Short:    "Stop servers",
	Long:     "Stop servers",
	ArgsName: "<server> ...",
	ArgsLong: "<server> ... A list of servers to stop.",
}

func runStopServers(cmd *cmdline.Command, args []string) error {
	if len(args) == 0 {
		return cmd.UsageErrorf("no server specified")
	}

	ctx, shutdown := v23.Init()
	defer shutdown()

	for _, server := range args {
		if err := stress.StressClient(server).Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	cmdRoot := &cmdline.Command{
		Name:  "stress",
		Short: "Tool to stress/load test RPC",
		Long:  "Tool to stress/load test RPC by issuing randomly generated requests",
		Children: []*cmdline.Command{
			cmdStressTest,
			cmdStressStats,
			cmdLoadTest,
			cmdStopServers,
		},
	}
	os.Exit(cmdRoot.Main())
}
