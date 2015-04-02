// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package consts defines constants used by the exec library.
package consts

// The exec package uses this environment variable to communicate
// the version of the protocol being used between the parent and child.
// It takes care to clear this variable from the child process'
// environment as soon as it can, however, there may still be some
// situations where an application may need to test for its presence
// or ensure that it doesn't appear in a set of environment variables;
// exposing the name of this variable is intended to support such
// situations.
const ExecVersionVariable = "V23_EXEC_VERSION"
