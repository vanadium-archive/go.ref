// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"v.io/x/lib/cmdline"

	"v.io/v23"
	"v.io/v23/context"

	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/runtime/internal/rpc/benchmark/internal"
)

func main() {
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

var cmdRoot = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runBenchmarkD),
	Name:   "benchmarkd",
	Short:  "Run the benchmark server",
	Long:   "Command benchmarkd runs the benchmark server.",
}

func runBenchmarkD(ctx *context.T, env *cmdline.Env, args []string) error {
	ep, stop := internal.StartServer(ctx, v23.GetListenSpec(ctx))
	ctx.Infof("Listening on %s", ep.Name())
	defer stop()
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
