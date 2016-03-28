// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	wire "v.io/v23/services/syncbase"
	"v.io/x/lib/vlog"
)

////////////////////////////////////////
// ConflictManager RPC methods

func (d *databaseReq) StartConflictResolver(ctx *context.T, call wire.ConflictManagerStartConflictResolverServerCall) error {
	// Store the conflict resolver connection in the per-app, per-database
	// singleton so that sync can access it.
	vlog.VI(2).Infof("cr: StartConflictResolver: resolution stream established")

	d.database.crMu.Lock()
	d.database.crStream = call
	d.database.crMu.Unlock()

	// To keep the CrStream alive we cant finish this rpc. Wait till the
	// context for this rpc is canceled/closed.
	<-ctx.Done()

	// When the above blocking call returns it means that the channel is no more
	// live. Remove the crStream instance from cache.
	// NOTE: any code that accesses crStream must make a copy of the pointer
	// before using it to make sure that the value does not suddenly become
	// nil midway through their processing.
	d.database.crMu.Lock()
	d.database.crStream = nil
	d.database.crMu.Unlock()
	return nil
}
