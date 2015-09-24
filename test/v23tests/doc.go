// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package v23tests implements support for writing writing end-to-end
// integration tests. In particular, support is provided for building binaries,
// running processes, making assertions about their output/state and ensuring
// that no processes or files are left behind on exit. Since such tests are
// often difficult to debug facilities are provided to help do so.
//
// The preferred usage of this integration test framework is via the v23
// tool which generates supporting code. The primary reason for doing so is
// to cleanly separate integration tests, which can be very expensive to run,
// from normal unit tests which are intended to be fast and used constantly.
// However, it still beneficial to be able to always compile the integration
// test code with the normal test code, just not to run it. Similarly, it
// is beneficial to share as much of the existing go test infrastructure as
// possible, so the generated code uses a flag and a naming convention to
// separate the tests. Integration tests may be run in addition to unit tests
// by supplying the --v23.tests flag; the -run flag can be used
// to avoid running unit tests by specifying a prefix of TestV23 since
// the generated test functions names always start with TestV23. Thus:
//
// v23 go test -v <pkgs> --v23.tests  // runs both unit and integration tests
// v23 go test -v -run=TestV23 <pkgs> --v23.tests // runs just integration tests
//
// The go generate mechanism is used to generate the test code, thus the
// comment:
//
// //go:generate jiri test generate
//
// will generate the files v23_test.go and internal_v23_test.go for the
// package in which it occurs. Run v23 test generate --help for full
// details and options. In short, any function in an external
// (i.e. <pgk>_test) test package of the following form:
//
// V23Test<x>(t *v23tests.T)
//
// will be invoked as integration test if the --v23.tests flag is used.
//
// The generated code makes use of the RunTest function.
//
// The test environment is implemented by an instance of T.
// It is constructed with an instance of another interface TB, a mirror
// of testing.TB. Thus, the integration test environment can be used
// directly as follows:
//
//   func TestFoo(t *testing.T) {
//     env := v23tests.New(t)
//     defer env.Cleanup()
//
//     ...
//   }
//
// The methods in this API typically do not return error in the case of
// failure. Instead, the current test will fail with an appropriate error
// message. This avoids the need to handle errors inline in the test.
//
// The test environment manages all built packages, subprocesses and a
// set of environment variables that are passed to subprocesses.
//
// Debugging is supported as follows:
// 1. The DebugShell method creates an interative shell at that point in
//    the tests execution that has access to all of the running processes
//    and environment of those processes. The developer can interact with
//    those processes to determine the state of the test.
// 2. Calls to methods on Test (e.g. FailNow, Fatalf) that fail the test
//    cause the Cleanup method to print out the status of all invocations.
// 3. Similarly, if the --v23.tests.shell-on-error flag is set then the
//    cleanup method will invoke a DebugShell on a test failure allowing
//    the developer to inspect the state of the test.
// 4. The implementation of this package uses filenames that start with v23test
//    to allow for easy tracing with --vmodule=v23test*=2 for example.
//
package v23tests
