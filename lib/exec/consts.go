// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package exec

// ExecVersionVariable is the name of the environment variable used by the exec
// package to communicate the protocol version between the parent and child.  It
// takes care to clear this variable from the child process' environment as soon
// as it can, however, there may still be some situations where an application
// may need to test for its presence or ensure that it doesn't appear in a set
// of environment variables; exposing the name of this variable is intended to
// support such situations.
const ExecVersionVariable = "V23_EXEC_VERSION"
