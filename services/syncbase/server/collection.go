// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/watchable"
)

// collectionReq is a per-request object that handles Collection RPCs.
type collectionReq struct {
	name string
	d    *database
}

var (
	_ wire.CollectionServerMethods = (*collectionReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (c *collectionReq) Create(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle, perms access.Permissions) error {
	impl := func(tx *watchable.Transaction) error {
		// Check DatabaseData perms.
		dData := &DatabaseData{}
		if err := util.GetWithAuth(ctx, call, tx, c.d.stKey(), dData); err != nil {
			return err
		}
		// Check for "collection already exists".
		if err := store.Get(ctx, tx, c.permsKey(), &CollectionPerms{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			return verror.New(verror.ErrExist, ctx, c.name)
		}
		if perms == nil {
			perms = dData.Perms
		}
		// Write new CollectionPerms.
		storedPerms := CollectionPerms(perms)
		return store.Put(ctx, tx, c.permsKey(), &storedPerms)
	}
	return c.d.runInExistingBatchOrNewTransaction(ctx, bh, impl)
}

// TODO(ivanpi): Decouple collection key prefix from collection name to allow
// collection data deletion to be deferred, making deletion faster (reference
// removal). Same for database deletion.
func (c *collectionReq) Destroy(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) error {
	impl := func(tx *watchable.Transaction) error {
		// Read-check-delete CollectionPerms.
		if err := util.GetWithAuth(ctx, call, tx, c.permsKey(), &CollectionPerms{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}

		// TODO(ivanpi): Check that no syncgroup includes the collection being
		// destroyed. Also check that all collections exist when creating a
		// syncgroup. Refactor with common part of DeleteRange.

		// Delete all data rows.
		it := tx.Scan(common.ScanPrefixArgs(common.JoinKeyParts(common.RowPrefix, c.name), ""))
		var key []byte
		for it.Advance() {
			key = it.Key(key)
			if err := tx.Delete(key); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		// Delete CollectionPerms.
		return store.Delete(ctx, tx, c.permsKey())
	}
	return c.d.runInExistingBatchOrNewTransaction(ctx, bh, impl)
}

func (c *collectionReq) Exists(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) (bool, error) {
	impl := func(sntx store.SnapshotOrTransaction) error {
		return util.GetWithAuth(ctx, call, c.d.st, c.permsKey(), &CollectionPerms{})
	}
	return util.ErrorToExists(c.d.runWithExistingBatchOrNewSnapshot(ctx, bh, impl))
}

func (c *collectionReq) GetPermissions(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) (perms access.Permissions, err error) {
	var res CollectionPerms
	impl := func(sntx store.SnapshotOrTransaction) error {
		return util.GetWithAuth(ctx, call, sntx, c.permsKey(), &res)
	}
	if err := c.d.runWithExistingBatchOrNewSnapshot(ctx, bh, impl); err != nil {
		return nil, err
	}
	return access.Permissions(res), nil
}

func (c *collectionReq) SetPermissions(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle, perms access.Permissions) error {
	impl := func(tx *watchable.Transaction) error {
		if err := c.checkAccess(ctx, call, tx); err != nil {
			return err
		}
		storedPerms := CollectionPerms(perms)
		return store.Put(ctx, tx, c.permsKey(), &storedPerms)
	}
	return c.d.runInExistingBatchOrNewTransaction(ctx, bh, impl)
}

func (c *collectionReq) DeleteRange(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle, start, limit []byte) error {
	impl := func(tx *watchable.Transaction) error {
		// Check for collection-level access before doing a scan.
		if err := c.checkAccess(ctx, call, tx); err != nil {
			return err
		}
		it := tx.Scan(common.ScanRangeArgs(common.JoinKeyParts(common.RowPrefix, c.name), string(start), string(limit)))
		key := []byte{}
		for it.Advance() {
			key = it.Key(key)
			// Delete the key-value pair.
			if err := tx.Delete(key); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	return c.d.runInExistingBatchOrNewTransaction(ctx, bh, impl)
}

func (c *collectionReq) Scan(ctx *context.T, call wire.CollectionScanServerCall, bh wire.BatchHandle, start, limit []byte) error {
	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check for collection-level access before doing a scan.
		if err := c.checkAccess(ctx, call, sntx); err != nil {
			return err
		}
		it := sntx.Scan(common.ScanRangeArgs(common.JoinKeyParts(common.RowPrefix, c.name), string(start), string(limit)))
		sender := call.SendStream()
		key, value := []byte{}, []byte{}
		for it.Advance() {
			key, value = it.Key(key), it.Value(value)
			// See comment in util/constants.go for why we use SplitNKeyParts.
			parts := common.SplitNKeyParts(string(key), 3)
			externalKey := parts[2]
			if err := sender.Send(wire.KeyValue{Key: externalKey, Value: value}); err != nil {
				it.Cancel()
				return err
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	return c.d.runWithExistingBatchOrNewSnapshot(ctx, bh, impl)
}

func (c *collectionReq) GlobChildren__(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element) error {
	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check perms.
		if err := c.checkAccess(ctx, call, sntx); err != nil {
			return err
		}
		return util.GlobChildren(ctx, call, matcher, sntx, common.JoinKeyParts(common.RowPrefix, c.name))
	}
	return store.RunWithSnapshot(c.d.st, impl)
}

////////////////////////////////////////
// interfaces.Database methods

func (c *collectionReq) Database() interfaces.Database {
	return c.d
}

func (c *collectionReq) Name() string {
	return c.name
}

////////////////////////////////////////
// Internal helpers

func (c *collectionReq) permsKey() string {
	// TODO(rdaoud,ivanpi): The third empty key component is added to ensure a
	// sync prefix matches only the exact collection name. Make sync handling of
	// collections more explicit and clean up this hack.
	return common.JoinKeyParts(common.CollectionPermsPrefix, c.stKeyPart(), "")
}

func (c *collectionReq) stKeyPart() string {
	return c.name
}

// checkAccess checks that this collection exists in the database, and performs
// an authorization check on the collection ACL. It should be called in the same
// transaction as any store modification to ensure that concurrent ACL changes
// invalidate the modification.
// TODO(rogulenko): Revisit this behavior. Eventually we'll want the
// collection-level access check to be a check for "Resolve", i.e. also check
// access to service and database.
func (c *collectionReq) checkAccess(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction) error {
	if err := util.GetWithAuth(ctx, call, sntx, c.permsKey(), &CollectionPerms{}); err != nil {
		return err
	}
	return nil
}
