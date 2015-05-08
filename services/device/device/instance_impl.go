// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// Commands to modify instance.

import (
	"fmt"
	"time"

	"v.io/v23/context"
	"v.io/v23/services/device"
	"v.io/x/lib/cmdline2"
	"v.io/x/ref/lib/v23cmd"
)

var cmdDelete = &cmdline2.Command{
	Runner:   v23cmd.RunnerFunc(runDelete),
	Name:     "delete",
	Short:    "Delete the given application instance.",
	Long:     "Delete the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to delete.`,
}

func runDelete(ctx *context.T, env *cmdline2.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("delete: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Delete(ctx); err != nil {
		return fmt.Errorf("Delete failed: %v", err)
	}
	fmt.Fprintf(env.Stdout, "Delete succeeded\n")
	return nil
}

const killDeadline = 10 * time.Second

var cmdKill = &cmdline2.Command{
	Runner:   v23cmd.RunnerFunc(runKill),
	Name:     "kill",
	Short:    "Kill the given application instance.",
	Long:     "Kill the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to kill.`,
}

func runKill(ctx *context.T, env *cmdline2.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("kill: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Kill(ctx, killDeadline); err != nil {
		return fmt.Errorf("Kill failed: %v", err)
	}
	fmt.Fprintf(env.Stdout, "Kill succeeded\n")
	return nil
}

var cmdRun = &cmdline2.Command{
	Runner:   v23cmd.RunnerFunc(runRun),
	Name:     "run",
	Short:    "Run the given application instance.",
	Long:     "Run the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to run.`,
}

func runRun(ctx *context.T, env *cmdline2.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("run: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Run(ctx); err != nil {
		return fmt.Errorf("Run failed: %v,\nView log with:\n debug logs read `debug glob %s/logs/STDERR-*`", err, appName)
	}
	fmt.Fprintf(env.Stdout, "Run succeeded\n")
	return nil
}
