// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"

	"v.io/v23/context"
	"v.io/v23/services/device"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
)

var cmdDebug = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runDebug),
	Name:     "debug",
	Short:    "Debug the device.",
	Long:     "Debug the device.",
	ArgsName: "<app name>",
	ArgsLong: `
<app name> is the vanadium object name of an app installation or instance.`,
}

func runDebug(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("debug: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	appName := args[0]
	if description, err := device.DeviceClient(appName).Debug(ctx); err != nil {
		return fmt.Errorf("Debug failed: %v", err)
	} else {
		fmt.Fprintf(env.Stdout, "%v\n", description)
	}
	return nil
}
