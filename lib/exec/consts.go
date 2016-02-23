// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package exec

// The V23_EXEC_CONFIG environment variable is used to share a base64 encoded JSON
// dictionary containing an instance of Config between a parent and child process.
const V23_EXEC_CONFIG = "V23_EXEC_CONFIG"

// ExecVersionVariable is the name of the environment variable used by the exec
// package to communicate the protocol version between the parent and child.  It
// takes care to clear this variable from the child process' environment as soon
// as it can, however, there may still be some situations where an application
// may need to test for its presence or ensure that it doesn't appear in a set
// of environment variables; exposing the name of this variable is intended to
// support such situations.
const ExecVersionVariable = "V23_EXEC_VERSION"

const (
	version1     = "1.0.0"
	readyStatus  = "ready::"
	failedStatus = "failed::"
	initStatus   = "init"

	// eofChar is written onto the status pipe to signal end-of-file.  It
	// should be the last byte written onto the pipe, before closing it.
	// This signals to the reader that no more input is coming.  This is
	// needed since we cannot use the closing of the write end of the pipe
	// to send io.EOF to the reader: since there are two write ends (one in
	// the parent and one in the child), closing any one of the two is not
	// going to send io.EOF to the reader.
	// Since the data coming from the child should be valid utf-8, we pick
	// one of the invalid utf-8 bytes for this.
	eofChar = 0xFF
)
