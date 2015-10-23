// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package test implements initalization for unit and integration tests.
//
// Init configures logging, random number generators and other global state.
// Typical usage in _test.go files:
//
// import "v.io/x/ref/test"
//
// func TestMain(m *testing.M) {
//     test.Init()
//     os.Exit(m.Run())
// }
//
// V23Init can be used within test functions as a safe alternative to v23.Init.
//
// func TestFoo(t *testing.T) {
//    ctx, shutdown := test.V23Init()
//    defer shutdown()
//    ...
// }
//
// This package also defines flags for enabling and controlling
// the v23 integration tests in package v23tests:
// --v23.tests - run the integration tests
// --v23.tests.shell-on-fail - drop into a debug shell if the test fails.
//
// Typical usage is:
// $ jiri go test . --v23.tests
//
// Note that, like all flags not recognised by the go testing package, the
// v23.tests flags must follow the package spec.
//
// The sub-directories of this package provide either functionality that
// can be used within traditional go tests, or support for the v23 integration
// test framework. The jiri command is able to generate boilerplate code
// to support these tests. In summary, 'jiri test generate' will generate
// go files to be checked in that include appropriate TestMain functions,
// registration calls for modules commands and wrapper functions for v23test
// tests. More detailed documentation is available via:
//
// $ jiri test generate --help
//
// Vanadium tests often need to run subprocesses to provide either common
// services that they depend (e.g. a mount table) and/or services that are
// specific to the tests. The modules and v23tests subdirectories contain
// packages that simplify this process.
//
// The subdirectories are:
// benchmark  - support for writing benchmarks.
// testutil   - utility functions used in tests.
// security   - security related utility functions used in tests.
// timekeeper - an implementation of the timekeeper interface for use within
//              tests.
// modules    - support for running subprocesses using a single binary
// v23tests   - support for integration tests, including compiling and running
//              arbirtrary go code and pre-existing system commands.
// expect     - support for testing the contents of input streams. The methods
//              provided are embedded in the types used by modules and v23tests
//              so this package is generally not used directly.
package test
