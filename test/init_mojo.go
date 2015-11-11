// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

package test

import (
	"mojo/public/go/application"

	"v.io/v23"
	"v.io/v23/context"
)

// V23Init initializes the runtime and sets up the principal with the
// self-signed TestBlessing. The blessing setup step is skipped if this function
// is invoked from a subprocess run using the modules package; in that case, the
// blessings of the principal are created by the parent process.
// NOTE: For tests involving Vanadium RPCs, developers are encouraged to use
// V23InitWithMounttable, and have their services access each other via the
// mount table (rather than using endpoint strings).
func V23Init(mctx application.Context) (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init(mctx)
	ctx = internalInit(ctx, false)
	return ctx, shutdown
}

// V23InitWithMounttable initializes the runtime and:
// - Sets up the principal with the self-signed TestBlessing
// - Starts a mounttable and sets the namespace roots appropriately
// Both these steps are skipped if this function is invoked from a subprocess
// run using the modules package; in that case, the mounttable to use and the
// blessings of the principal are created by the parent process.
func V23InitWithMounttable(mctx application.Context) (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init(mctx)
	ctx = internalInit(ctx, true)
	return ctx, shutdown
}
