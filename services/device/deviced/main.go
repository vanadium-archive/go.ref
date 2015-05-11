// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"os"
	"runtime"

	"v.io/x/lib/cmdline2"
	"v.io/x/ref/lib/v23cmd"
)

func main() {
	// TODO(caprita): Remove this once we have a way to set the GOMAXPROCS
	// environment variable persistently for device manager.
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	rootCmd := &cmdline2.Command{
		Name:  "deviced",
		Short: "launch, configure and manage the deviced daemon",
		Long: `
Command deviced is used to launch, configure and manage the deviced daemon,
which implements the v.io/v23/services/device interfaces.
`,
		Children: []*cmdline2.Command{cmdInstall, cmdUninstall, cmdStart, cmdStop, cmdProfile},
		Runner:   v23cmd.RunnerFunc(runServer),
	}
	cmdline2.Main(rootCmd)
}
