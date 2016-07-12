// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/store"
)

// TODO(sadovsky): These methods should check that we're not in a batch. Better
// yet, hopefully we can just delete all the schema-related code, and add it
// back later.

////////////////////////////////////////
// SchemaManager RPC methods

func (d *database) GetSchemaMetadata(ctx *context.T, call rpc.ServerCall) (wire.SchemaMetadata, error) {
	allowGetSchemaMetadata := []access.Tag{access.Read}

	if !d.exists {
		return wire.SchemaMetadata{}, verror.New(verror.ErrNoExist, ctx, d.id)
	}
	// Check permissions on Database and retrieve schema metadata.
	var dbData DatabaseData
	if _, err := common.GetDataWithAuth(ctx, call, d, allowGetSchemaMetadata, d.st, &dbData); err != nil {
		return wire.SchemaMetadata{}, err
	}
	if dbData.SchemaMetadata == nil {
		return wire.SchemaMetadata{}, verror.New(verror.ErrNoExist, ctx, "Schema does not exist for the db")
	}
	return *dbData.SchemaMetadata, nil
}

func (d *database) SetSchemaMetadata(ctx *context.T, call rpc.ServerCall, metadata wire.SchemaMetadata) error {
	allowSetSchemaMetadata := []access.Tag{access.Admin}

	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.id)
	}
	// Check permissions on Database and store schema metadata.
	return store.RunInTransaction(d.st, func(tx store.Transaction) error {
		var dbData DatabaseData
		if _, err := common.GetDataWithAuth(ctx, call, d, allowSetSchemaMetadata, tx, &dbData); err != nil {
			return err
		}
		// NOTE: For now we expect the client to not issue multiple
		// concurrent SetSchemaMetadata calls.
		dbData.SchemaMetadata = &metadata
		return store.Put(ctx, tx, d.stKey(), &dbData)
	})
}

////////////////////////////////////////
// interfaces.Database methods

func (d *database) GetSchemaMetadataInternal(ctx *context.T) (*wire.SchemaMetadata, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.id)
	}
	var dbData DatabaseData
	if err := store.Get(ctx, d.st, d.stKey(), &dbData); err != nil {
		return nil, err
	}
	if dbData.SchemaMetadata == nil {
		return nil, verror.NewErrNoExist(ctx)
	}
	return dbData.SchemaMetadata, nil
}
