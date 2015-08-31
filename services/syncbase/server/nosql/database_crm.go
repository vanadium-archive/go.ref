// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"v.io/v23/context"
	wire "v.io/v23/services/syncbase/nosql"
)

////////////////////////////////////////
// ConflictManager RPC methods

func (d *databaseReq) StartConflictResolver(ctx *context.T, call wire.ConflictManagerStartConflictResolverServerCall) error {
	// Store the conflict resolver connection in the per-app, per-database
	// singleton so that sync can access it.
	d.database.resolver = call
	return nil
}
