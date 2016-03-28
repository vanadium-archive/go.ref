// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
)

// App is an internal interface to the app layer.
type App interface {
	// Service returns the service handle for this app.
	Service() Service

	// Database returns the Database for the specified database.
	Database(ctx *context.T, call rpc.ServerCall, dbName string) (Database, error)

	// DatabaseNames returns the names of the databases within the App.
	DatabaseNames(ctx *context.T, call rpc.ServerCall) ([]string, error)

	// CreateDatabase creates the specified database.
	CreateDatabase(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, metadata *wire.SchemaMetadata) error

	// DestroyDatabase deletes the specified database.
	DestroyDatabase(ctx *context.T, call rpc.ServerCall, dbName string) error

	// SetDatabasePerms sets the perms for the specified database.
	SetDatabasePerms(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, version string) error

	// Name returns the name of this app.
	Name() string
}
