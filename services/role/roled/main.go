// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/vlog"
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
	server, err := v23.NewServer(ctx)
	if err != nil {
		return fmt.Errorf("NewServer failed: %v", err)
	}

	listenSpec := v23.GetListenSpec(ctx)
	eps, err := server.Listen(listenSpec)
	if err != nil {
		return fmt.Errorf("Listen(%v) failed: %v", listenSpec, err)
	}
	vlog.Infof("Listening on: %q", eps)
	if err := server.ServeDispatcher(name, irole.NewDispatcher(configDir, name)); err != nil {
		return fmt.Errorf("ServeDispatcher(%q) failed: %v", name, err)
	}
	if len(name) > 0 {
		fmt.Printf("NAME=%s\n", name)
	} else if len(eps) > 0 {
		fmt.Printf("NAME=%s\n", eps[0].Name())
	}
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
