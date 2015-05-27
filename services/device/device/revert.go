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

var cmdRevert = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runRevert),
	Name:     "revert",
	Short:    "Revert the device manager or application",
	Long:     "Revert the device manager or application to its previous version",
	ArgsName: "<object>",
	ArgsLong: `
<object> is the vanadium object name of the device manager or application
installation to revert.`,
}

func runRevert(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("revert: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	deviceName := args[0]
	if err := device.ApplicationClient(deviceName).Revert(ctx); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Revert successful.")
	return nil
}
