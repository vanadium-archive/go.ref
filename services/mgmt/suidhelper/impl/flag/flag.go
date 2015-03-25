// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package flag provides flag definitions for the suidhelper package.
//
// It does NOT depend on any packages outside the Go standard library.
// This allows v.io/x/ref/lib/test to depend on this
// package, thereby ensuring that the suidhelper flags are defined
// before the flag.Parse call in testutil.init is made.
//
// This is a hack! This file should go away once testutil.init
// is changed to not parse flags in init().
// TODO(cnicolaou,ashankar): See above!
package flag

import "flag"

var (
	Username, Workspace, LogDir, Run, ProgName *string
	MinimumUid                                 *int64
	Remove, Dryrun                             *bool
)

func init() {
	SetupFlags(flag.CommandLine)
}

func SetupFlags(fs *flag.FlagSet) {
	Username = fs.String("username", "", "The UNIX user name used for the other functions of this tool.")
	Workspace = fs.String("workspace", "", "Path to the application's workspace directory.")
	LogDir = fs.String("logdir", "", "Path to the log directory.")
	Run = fs.String("run", "", "Path to the application to exec.")
	ProgName = fs.String("progname", "unnamed_app", "Visible name of the application, used in argv[0]")
	MinimumUid = fs.Int64("minuid", uidThreshold, "UIDs cannot be less than this number.")
	Remove = fs.Bool("rm", false, "Remove the file trees given as command-line arguments.")
	Dryrun = fs.Bool("dryrun", false, "Elides root-requiring systemcalls.")
}

const uidThreshold = 501
