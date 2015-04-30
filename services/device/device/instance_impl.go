// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// Commands to modify instance.

import (
	"fmt"
	"time"

	"v.io/v23/services/device"
	"v.io/x/lib/cmdline"
)

var cmdDelete = &cmdline.Command{
	Run:      runDelete,
	Name:     "delete",
	Short:    "Delete the given application instance.",
	Long:     "Delete the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to delete.`,
}

func runDelete(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("delete: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Delete(gctx); err != nil {
		return fmt.Errorf("Delete failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Delete succeeded\n")
	return nil
}

const killDeadline = 10 * time.Second

var cmdKill = &cmdline.Command{
	Run:      runKill,
	Name:     "kill",
	Short:    "Kill the given application instance.",
	Long:     "Kill the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to kill.`,
}

func runKill(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("kill: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Kill(gctx, killDeadline); err != nil {
		return fmt.Errorf("Kill failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Kill succeeded\n")
	return nil
}

var cmdRun = &cmdline.Command{
	Run:      runRun,
	Name:     "run",
	Short:    "Run the given application instance.",
	Long:     "Run the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to run.`,
}

func runRun(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("run: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Run(gctx); err != nil {
		return fmt.Errorf("Run failed: %v,\nView log with:\n debug logs read `debug glob %s/logs/STDERR-*`", err, appName)
	}
	fmt.Fprintf(cmd.Stdout(), "Run succeeded\n")
	return nil
}
