// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !mojo

package test

import (
	"v.io/v23"
	"v.io/v23/context"
)

// V23Init initializes the runtime and sets up the principal with a self-signed
// TestBlessing. The blessing setup step is skipped if this function is invoked
// from a v23test.Shell child process, since v23test.Shell passes credentials to
// its children.
// NOTE: For tests involving Vanadium RPCs, developers are encouraged to use
// V23InitWithMounttable, and have their services access each other via the
// mount table (rather than using endpoint strings).
func V23Init() (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init()
	ctx = internalInit(ctx, false)
	return ctx, shutdown
}

// V23InitWithMounttable initializes the runtime and:
// - Sets up the principal with a self-signed TestBlessing
// - Starts a mounttable and sets the namespace roots appropriately
// Both these steps are skipped if this function is invoked from a v23test.Shell
// child process.
func V23InitWithMounttable() (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init()
	ctx = internalInit(ctx, true)
	return ctx, shutdown
}
