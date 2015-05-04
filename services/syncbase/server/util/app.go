// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
)

// App is an internal interface that enables nosql.database to invoke methods on
// server.app while avoiding an import cycle.
// All methods return VDL-compatible errors.
type App interface {
	// NoSQLDatabase returns the Database for the specified NoSQL database.
	NoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) (Database, error)

	// CreateNoSQLDatabase creates the specified NoSQL database.
	CreateNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions) error

	// DeleteNoSQLDatabase deletes the specified NoSQL database.
	DeleteNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) error

	// SetDatabasePerms sets the perms for the specified database.
	SetDatabasePerms(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, version string) error
}
