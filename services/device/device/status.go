// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"

	"v.io/v23/context"
	"v.io/v23/services/device"
	"v.io/x/lib/cmdline"
)

var cmdStatus = &cmdline.Command{
	Runner:   globRunner(runStatus),
	Name:     "status",
	Short:    "Get application status.",
	Long:     "Get the status of application installations and instances.",
	ArgsName: "<app name patterns...>",
	ArgsLong: `
<app name patterns...> are vanadium object names or glob name patterns corresponding to application installations and instances.`,
}

func runStatus(entry globResult, _ *context.T, stdout, _ io.Writer) error {
	switch s := entry.status.(type) {
	case device.StatusInstance:
		fmt.Fprintf(stdout, "Instance %v [State:%v,Version:%v]\n", entry.name, s.Value.State, s.Value.Version)
	case device.StatusInstallation:
		fmt.Fprintf(stdout, "Installation %v [State:%v,Version:%v]\n", entry.name, s.Value.State, s.Value.Version)
	default:
		return fmt.Errorf("Status returned unknown type: %T", s)
	}
	return nil
}
