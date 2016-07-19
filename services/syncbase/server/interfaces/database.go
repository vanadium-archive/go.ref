// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import (
	"v.io/v23/context"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/watchable"
)

// Database is an internal interface to the database layer.
type Database interface {
	// Id returns the id of this database.
	Id() wire.Id

	// St returns the storage engine instance for this database.
	St() *watchable.Store

	// Service returns the service handle for this database.
	Service() Service

	// GetCollectionPerms retrieves the permissions for the Collection with the
	// given id. No authorization check is performed.
	GetCollectionPerms(ctx *context.T, cxId wire.Id, st store.StoreReader) (access.Permissions, error)

	// GetSchemaMetadataInternal returns SchemaMetadata stored for this db
	// without checking any credentials.
	GetSchemaMetadataInternal(ctx *context.T) (*wire.SchemaMetadata, error)

	// CrConnectionStream returns the current conflict resolution stream
	// established between an app and this database.
	CrConnectionStream() wire.ConflictManagerStartConflictResolverServerStream

	// ResetCrConnectionStream resets the current conflict resolution stream.
	// This can be used to either close an active stream or to remove a dead
	// stream.
	// Note: Resetting a stream does not reconnect the stream. Its upto the
	// client to reconnect.
	ResetCrConnectionStream()

	common.Permser
}
