// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"regexp"

	"v.io/x/lib/cmdline"
	_ "v.io/x/ref/runtime/factories/static"
)

var CmdRoot = &cmdline.Command{
	Name:  "device",
	Short: "facilitates interaction with the Vanadium device manager",
	Long: `
Command device facilitates interaction with the Vanadium device manager.
`,
	Children: []*cmdline.Command{cmdInstall, cmdInstallLocal, cmdUninstall, cmdAssociate, cmdDescribe, cmdClaim, cmdInstantiate, cmdDelete, cmdRun, cmdKill, cmdRevert, cmdUpdate, cmdUpdateAll, cmdStatus, cmdDebug, cmdACL, cmdPublish},
}

func main() {
	cmdline.HideGlobalFlagsExcept(regexp.MustCompile(`^((v23\.namespace\.root)|(v23\.proxy))$`))
	cmdline.Main(CmdRoot)
}
