// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

////////////////////////////////////////
// SchemaManager RPC methods

func (d *databaseReq) GetSchemaMetadata(ctx *context.T, call rpc.ServerCall) (wire.SchemaMetadata, error) {
	metadata := wire.SchemaMetadata{}

	if !d.exists {
		return metadata, verror.New(verror.ErrNoExist, ctx, d.Name())
	}

	// Check permissions on Database and retreve schema metadata.
	dbData := databaseData{}
	if err := util.GetWithAuth(ctx, call, d.st, d.stKey(), &dbData); err != nil {
		return metadata, err
	}
	if dbData.SchemaMetadata == nil {
		return metadata, verror.New(verror.ErrNoExist, ctx, "Schema does not exist for the db")
	}
	return *dbData.SchemaMetadata, nil
}

func (d *databaseReq) SetSchemaMetadata(ctx *context.T, call rpc.ServerCall, metadata wire.SchemaMetadata) error {
	// Check if database exists
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.Name())
	}

	// Check permissions on Database and store schema metadata.
	return store.RunInTransaction(d.st, func(tx store.Transaction) error {
		dbData := databaseData{}
		return util.UpdateWithAuth(ctx, call, tx, d.stKey(), &dbData, func() error {
			// NOTE: For now we expect the client to not issue multiple
			// concurrent SetSchemaMetadata calls.
			dbData.SchemaMetadata = &metadata
			return nil
		})
	})
}

func (d *databaseReq) getSchemaMetadataWithoutAuth(ctx *context.T) (*wire.SchemaMetadata, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrInternal, ctx, "field store in database cannot be nil")
	}
	dbData := databaseData{}
	if err := util.Get(ctx, d.st, d.stKey(), &dbData); err != nil {
		return nil, err
	}
	return dbData.SchemaMetadata, nil
}
