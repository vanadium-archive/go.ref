// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"
	"strings"

	"v.io/v23/context"
	"v.io/v23/services/device"
	"v.io/x/lib/cmdline"
)

var cmdDebug = &cmdline.Command{
	Runner:   globRunner(runDebug),
	Name:     "debug",
	Short:    "Debug the device.",
	Long:     "Get internal debug information about application installations and instances.",
	ArgsName: "<app name patterns...>",
	ArgsLong: `
<app name patterns...> are vanadium object names or glob name patterns corresponding to application installations and instances.`,
}

func runDebug(entry globResult, ctx *context.T, stdout, _ io.Writer) error {
	if description, err := device.DeviceClient(entry.name).Debug(ctx); err != nil {
		return fmt.Errorf("Debug failed: %v", err)
	} else {
		line := strings.Repeat("*", len(entry.name)+4)
		fmt.Fprintf(stdout, "%s\n* %s *\n%s\n%v\n", line, entry.name, line, description)
	}
	return nil
}
