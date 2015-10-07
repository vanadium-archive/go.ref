// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// sb - syncbase general-purpose client and management utility.
// Currently supports syncQL select queries.

package main

import (
	"flag"

	"v.io/x/lib/cmdline"
	_ "v.io/x/ref/runtime/factories/generic"
)

func main() {
	cmdline.Main(cmdSb)
}

var cmdSb = &cmdline.Command{
	Name:  "sb",
	Short: "sb - Vanadium syncbase client and management utility",
	Long: `
Syncbase general-purpose client and management utility.
Currently supports starting a syncQL shell.
`,
	Children: []*cmdline.Command{cmdSbShell},
}

var (
	// TODO(ivanpi): Decide on convention for local syncbase service name.
	flagSbService = flag.String("service", "/:8101/syncbase", "Location of the Syncbase service to connect to. Can be absolute or relative to the namespace root.")
)
