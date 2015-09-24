// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/static"
	irole "v.io/x/ref/services/role/roled/internal"
)

var configDir, name string

func main() {
	cmdRoleD.Flags.StringVar(&configDir, "config-dir", "", "The directory where the role configuration files are stored.")
	cmdRoleD.Flags.StringVar(&name, "name", "", "The name to publish for this service.")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoleD)
}

var cmdRoleD = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runRoleD),
	Name:   "roled",
	Short:  "Runs the Role interface daemon.",
	Long:   "Command roled runs the Role interface daemon.",
}

func runRoleD(ctx *context.T, env *cmdline.Env, args []string) error {
	if len(configDir) == 0 {
		return env.UsageErrorf("-config-dir must be specified")
	}
	if len(name) == 0 {
		return env.UsageErrorf("-name must be specified")
	}
	ctx, _, err := v23.WithNewDispatchingServer(ctx, name, irole.NewDispatcher(configDir, name))
	if err != nil {
		return fmt.Errorf("NewServer failed: %v", err)
	}
	fmt.Printf("NAME=%s\n", name)
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
