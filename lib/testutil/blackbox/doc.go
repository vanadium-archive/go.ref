// Package blackbox provides routines to help with blackbox testing.
//
// Some tests need to run a subprocess.  We reuse the same test binary
// to do so. A fake test 'TestHelperProcess' contains the code we need to
// run in the child and we simply run this same binary with a test.run= arg
// that runs just that test. This idea was taken from the tests for os/exec.
//
// To use this, you need to specify entry points to be called as subprocesses.
//
//     // File: mytestsubprocess.go
//     import (
//         "veyron/lib/testutil/blackbox"
//     )
//
//     func init() {
//         blackbox.CommandTable["myTestEntryPoint"] = myTestEntryPoint
//     }
//
//     func myTestEntryPoint(argv []string) {
//         // argv contains all non-flag arguments. Any flags should be handled
//         // using the flag package.
//         ...
//         blackbox.WaitForEOFOnStdin()
//     }
//
// You also need to specify a TestHelperProcess() entry point as part of your
// test suite.  This is boilerplate; the code reads as follows.
//
//     // File: driver_test.go
//     package foo
//
//     import (
//         "veyron/lib/testutil/blackbox"
//     )
//
//     func TestHelperProcess(t *testing.T) {
//		   blackbox.HelperProcess(t)
//     }
//
// Finally, to start a subprocess, use the HelperCommand(). defer execution
// of the Cleanup method to ensure that logs from the child process
// are collected, printed (if the vlog level is >= the number specified), and
// the log files are deleted.
//
//     // Starts myTestEntryPoint("myTestEntryPoint", "arg1", ..., "argN")
//     // in a subprocess.
//     child := blackbox.HelperCommand(t, "myTestEntryPoint", "arg1", ..., "argN")
//     child.Cmd.Start()
//     defer child.Cleanup(2)
//
//     // Use the ExpectXXX() functions to examine the output.
//     child.Expect("sometext\n")
//     child.ExpectEOFAndWait()
//
//     // Close stdin when you are done.
//     child.CloseStdin()
//
package blackbox
