// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import (
	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
)

// App is an internal interface to the app layer.
type App interface {
	// Service returns the service handle for this app.
	Service() Service

	// NoSQLDatabase returns the Database for the specified NoSQL database.
	NoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) (Database, error)

	// NoSQLDatabaseNames returns the names of the NoSQL databases within the App.
	NoSQLDatabaseNames(ctx *context.T, call rpc.ServerCall) ([]string, error)

	// CreateNoSQLDatabase creates the specified NoSQL database.
	CreateNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, metadata *wire.SchemaMetadata) error

	// DeleteNoSQLDatabase deletes the specified NoSQL database.
	DeleteNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) error

	// SetDatabasePerms sets the perms for the specified database.
	SetDatabasePerms(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, version string) error

	// Name returns the name of this app.
	Name() string
}
