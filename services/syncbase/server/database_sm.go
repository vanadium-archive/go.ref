// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

////////////////////////////////////////
// SchemaManager RPC methods

func (d *databaseReq) GetSchemaMetadata(ctx *context.T, call rpc.ServerCall) (wire.SchemaMetadata, error) {
	metadata := wire.SchemaMetadata{}

	if !d.exists {
		return metadata, verror.New(verror.ErrNoExist, ctx, d.Name())
	}

	// Check permissions on Database and retrieve schema metadata.
	dbData := DatabaseData{}
	if err := util.GetWithAuth(ctx, call, d.st, d.stKey(), &dbData); err != nil {
		return metadata, err
	}
	if dbData.SchemaMetadata == nil {
		return metadata, verror.New(verror.ErrNoExist, ctx, "Schema does not exist for the db")
	}
	return *dbData.SchemaMetadata, nil
}

func (d *databaseReq) SetSchemaMetadata(ctx *context.T, call rpc.ServerCall, metadata wire.SchemaMetadata) error {
	// Check if database exists.
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.Name())
	}
	// Check permissions on Database and store schema metadata.
	return store.RunInTransaction(d.st, func(tx store.Transaction) error {
		dbData := DatabaseData{}
		return util.UpdateWithAuth(ctx, call, tx, d.stKey(), &dbData, func() error {
			// NOTE: For now we expect the client to not issue multiple
			// concurrent SetSchemaMetadata calls.
			dbData.SchemaMetadata = &metadata
			return nil
		})
	})
}

func (d *database) GetSchemaMetadataInternal(ctx *context.T) (*wire.SchemaMetadata, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.Name())
	}
	dbData := DatabaseData{}
	if err := store.Get(ctx, d.st, d.stKey(), &dbData); err != nil {
		return nil, err
	}
	if dbData.SchemaMetadata == nil {
		return nil, verror.NewErrNoExist(ctx)
	}
	return dbData.SchemaMetadata, nil
}
