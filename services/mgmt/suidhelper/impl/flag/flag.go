// Package flag provides flag definitions for the suidhelper package.
//
// It does NOT depend on any packages outside the Go standard library.
// This allows veyron.io/veyron/veyron/lib/testutil to depend on this
// package, thereby ensuring that the suidhelper flags are defined
// before the flag.Parse call in testutil.init is made.
//
// This is a hack! This file should go away once testutil.init
// is changed to not parse flags in init().
// TODO(cnicolaou,ashankar): See above!
package flag

import "flag"

var (
	Username, Workspace, StdoutLog, StderrLog, Run *string
	MinimumUid                                     *int64
)

func init() {
	SetupFlags(flag.CommandLine)
}

func SetupFlags(fs *flag.FlagSet) {
	Username = fs.String("username", "", "The UNIX user name used for the other functions of this tool.")
	Workspace = fs.String("workspace", "", "Path to the application's workspace directory.")
	StdoutLog = fs.String("stdoutlog", "", "Path to the stdout log file.")
	StderrLog = fs.String("stderrlog", "", "Path to the stdin log file.")
	Run = fs.String("run", "", "Path to the application to exec.")
	MinimumUid = fs.Int64("minuid", uidThreshold, "UIDs cannot be less than this number.")
}

const uidThreshold = 501
