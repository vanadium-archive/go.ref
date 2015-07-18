// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import (
	"v.io/syncbase/x/ref/services/syncbase/localblobstore"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
)

// Database is an internal interface to the database layer.
type Database interface {
	// St returns the storage engine instance for this database.
	St() store.Store

	// BlobSt returns the blob storage engine instance for this database.
	BlobSt() localblobstore.BlobStore

	// App returns the app handle for this database.
	App() App

	// CheckPermsInternal checks whether the given RPC (ctx, call) is allowed per
	// the database perms.
	// Designed for use from within App.DeleteNoSQLDatabase.
	CheckPermsInternal(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter) error

	// SetPermsInternal updates the database perms.
	// Designed for use from within App.SetDatabasePerms.
	SetPermsInternal(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error

	// Name returns the name of this database.
	Name() string
}
