// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// Commands to modify instance.

import (
	"fmt"

	"v.io/v23/services/mgmt/device"
	"v.io/x/lib/cmdline"
)

var cmdStop = &cmdline.Command{
	Run:      runStop,
	Name:     "stop",
	Short:    "Stop the given application instance.",
	Long:     "Stop the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to stop.`,
}

func runStop(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("stop: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Stop(gctx, 5); err != nil {
		return fmt.Errorf("Stop failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Stop succeeded\n")
	return nil
}

var cmdSuspend = &cmdline.Command{
	Run:      runSuspend,
	Name:     "suspend",
	Short:    "Suspend the given application instance.",
	Long:     "Suspend the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to suspend.`,
}

func runSuspend(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("suspend: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Suspend(gctx); err != nil {
		return fmt.Errorf("Suspend failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Suspend succeeded\n")
	return nil
}

var cmdResume = &cmdline.Command{
	Run:      runResume,
	Name:     "resume",
	Short:    "Resume the given application instance.",
	Long:     "Resume the given application instance.",
	ArgsName: "<app instance>",
	ArgsLong: `
<app instance> is the vanadium object name of the application instance to resume.`,
}

func runResume(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("resume: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]

	if err := device.ApplicationClient(appName).Resume(gctx); err != nil {
		return fmt.Errorf("Resume failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Resume succeeded\n")
	return nil
}
