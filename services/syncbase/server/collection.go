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
	pubutil "v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// collectionReq is a per-request object that handles Collection RPCs.
type collectionReq struct {
	id wire.Id
	d  *database
}

var (
	_ wire.CollectionServerMethods = (*collectionReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (c *collectionReq) Create(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle, perms access.Permissions) error {
	allowCreate := []access.Tag{access.Write}

	if err := common.ValidatePerms(ctx, perms, wire.AllCollectionTags); err != nil {
		return err
	}
	impl := func(ts *transactionState) error {
		tx := ts.tx
		// Check Database perms.
		if _, err := common.GetPermsWithAuth(ctx, call, c.d, allowCreate, tx); err != nil {
			return err
		}
		// Check implicit perms derived from blessing pattern in id.
		implicitPerms, err := common.CheckImplicitPerms(ctx, call, c.id, wire.AllCollectionTags)
		if err != nil {
			return err
		}
		// Check for "collection already exists".
		// Note, since the caller has been verified to have Write perm on database,
		// they are allowed to know that the collection exists.
		if err := store.Get(ctx, tx, c.permsKey(), &interfaces.CollectionPerms{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			return verror.New(verror.ErrExist, ctx, c.id)
		}
		// Collection Create is equivalent to changing permissions from implicit to
		// explicit. Note, the creator implicitly has all permissions (RWA), so it
		// is legal to write data and drop the write permission in the same batch.
		ts.MarkPermsChanged(c.id, implicitPerms, perms)
		// Write new CollectionPerms.
		storedPerms := interfaces.CollectionPerms(perms)
		return store.Put(ctx, tx, c.permsKey(), &storedPerms)
	}
	return c.d.runInExistingBatchOrNewTransaction(ctx, call, bh, impl)
}

// TODO(ivanpi): Decouple collection key prefix from collection id to allow
// collection data deletion to be deferred, making deletion faster (reference
// removal).
func (c *collectionReq) Destroy(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) error {
	allowDestroy := []access.Tag{access.Admin}

	impl := func(ts *transactionState) error {
		tx := ts.tx
		var authErr error
		// Check permissions on Database.
		if _, authErr = common.GetPermsWithAuth(ctx, call, c.d, allowDestroy, tx); authErr != nil {
			// Caller has no Admin access on Database. Check CollectionPerms.
			_, authErr = common.GetPermsWithAuth(ctx, call, c, allowDestroy, tx)
		} else {
			// Caller has Admin access on Database. Check if Collection exists.
			authErr = store.Get(ctx, tx, c.permsKey(), &interfaces.CollectionPerms{})
		}
		if authErr != nil {
			if verror.ErrorID(authErr) == verror.ErrNoExist.ID {
				// Destroy is idempotent. Note, this doesn't leak Collection existence
				// before the call.
				return nil
			}
			return authErr
		}

		// TODO(ivanpi): Check that no syncgroup includes the collection being
		// destroyed. Also check that all collections exist when creating a
		// syncgroup. Refactor with common part of DeleteRange.

		// Reset all tracked changes to the collection. See comment on the method
		// for more details.
		ts.ResetCollectionChanges(c.id)
		// Delete all data rows.
		it := tx.Scan(common.ScanPrefixArgs(common.JoinKeyParts(common.RowPrefix, c.stKeyPart()), ""))
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
	return c.d.runInExistingBatchOrNewTransaction(ctx, call, bh, impl)
}

func (c *collectionReq) Exists(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) (bool, error) {
	impl := func(sntx store.SnapshotOrTransaction) error {
		_, _, err := c.GetDataWithExistAuth(ctx, call, sntx, &interfaces.CollectionPerms{})
		return err
	}
	return common.ErrorToExists(c.d.runWithExistingBatchOrNewSnapshot(ctx, call, bh, impl))
}

func (c *collectionReq) GetPermissions(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle) (access.Permissions, error) {
	allowGetPermissions := []access.Tag{access.Admin}

	var perms access.Permissions
	impl := func(sntx store.SnapshotOrTransaction) (err error) {
		perms, err = common.GetPermsWithAuth(ctx, call, c, allowGetPermissions, sntx)
		return err
	}
	if err := c.d.runWithExistingBatchOrNewSnapshot(ctx, call, bh, impl); err != nil {
		return nil, err
	}
	return perms, nil
}

func (c *collectionReq) SetPermissions(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle, newPerms access.Permissions) error {
	allowSetPermissions := []access.Tag{access.Admin}

	if err := common.ValidatePerms(ctx, newPerms, wire.AllCollectionTags); err != nil {
		return err
	}
	impl := func(ts *transactionState) error {
		tx := ts.tx
		currentPerms, err := common.GetPermsWithAuth(ctx, call, c, allowSetPermissions, tx)
		if err != nil {
			return err
		}
		ts.MarkPermsChanged(c.id, currentPerms, newPerms)
		storedPerms := interfaces.CollectionPerms(newPerms)
		return store.Put(ctx, tx, c.permsKey(), &storedPerms)
	}
	return c.d.runInExistingBatchOrNewTransaction(ctx, call, bh, impl)
}

func (c *collectionReq) DeleteRange(ctx *context.T, call rpc.ServerCall, bh wire.BatchHandle, start, limit []byte) error {
	allowDeleteRange := []access.Tag{access.Write}

	impl := func(ts *transactionState) error {
		tx := ts.tx
		// Check for collection-level access before doing a scan.
		currentPerms, err := common.GetPermsWithAuth(ctx, call, c, allowDeleteRange, tx)
		if err != nil {
			return err
		}
		ts.MarkDataChanged(c.id, currentPerms)
		it := tx.Scan(common.ScanRangeArgs(common.JoinKeyParts(common.RowPrefix, c.stKeyPart()), string(start), string(limit)))
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
	return c.d.runInExistingBatchOrNewTransaction(ctx, call, bh, impl)
}

func (c *collectionReq) Scan(ctx *context.T, call wire.CollectionScanServerCall, bh wire.BatchHandle, start, limit []byte) error {
	allowScan := []access.Tag{access.Read}

	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check for collection-level access before doing a scan.
		if _, err := common.GetPermsWithAuth(ctx, call, c, allowScan, sntx); err != nil {
			return err
		}
		it := sntx.Scan(common.ScanRangeArgs(common.JoinKeyParts(common.RowPrefix, c.stKeyPart()), string(start), string(limit)))
		sender := call.SendStream()
		key, value := []byte{}, []byte{}
		for it.Advance() {
			key, value = it.Key(key), it.Value(value)
			_, row, err := common.ParseRowKey(string(key))
			if err != nil {
				it.Cancel()
				return verror.New(verror.ErrInternal, ctx, err)
			}
			var rawBytes *vom.RawBytes
			if err := vom.Decode(value, &rawBytes); err != nil {
				// TODO(m3b): Is this the correct behaviour on an encoding error here?
				it.Cancel()
				return err
			}
			if err := sender.Send(wire.KeyValue{Key: row, Value: rawBytes}); err != nil {
				it.Cancel()
				return err
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	return c.d.runWithExistingBatchOrNewSnapshot(ctx, call, bh, impl)
}

func (c *collectionReq) GlobChildren__(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element) error {
	allowGlob := []access.Tag{access.Read}

	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check perms.
		if _, err := common.GetPermsWithAuth(ctx, call, c, allowGlob, sntx); err != nil {
			return err
		}
		return util.GlobChildren(ctx, call, matcher, sntx, common.JoinKeyParts(common.RowPrefix, c.stKeyPart()))
	}
	return c.d.runWithNewSnapshot(ctx, call, impl)
}

////////////////////////////////////////
// Authorization hooks

var _ common.Permser = (*collectionReq)(nil)

func (c *collectionReq) GetDataWithExistAuth(ctx *context.T, call rpc.ServerCall, st store.StoreReader, v common.PermserData) (parentPerms, perms access.Permissions, _ error) {
	cp := v.(*interfaces.CollectionPerms)
	parentPerms, err := common.GetPermsWithExistAndParentResolveAuth(ctx, call, c.d, st)
	if err != nil {
		return nil, nil, err
	}
	err = common.GetDataWithExistAuthStep(ctx, call, c.id.String(), parentPerms, st, c.permsKey(), cp)
	return parentPerms, cp.GetPerms(), err
}

func (c *collectionReq) PermserData() common.PermserData {
	return &interfaces.CollectionPerms{}
}

////////////////////////////////////////
// Internal helpers

func (c *collectionReq) permsKey() string {
	// TODO(rdaoud,ivanpi): The third empty key component is added to ensure a
	// sync prefix matches only the exact collection id. Make sync handling of
	// collections more explicit and clean up this hack.
	return common.JoinKeyParts(common.CollectionPermsPrefix, c.stKeyPart(), "")
}

func (c *collectionReq) stKeyPart() string {
	return pubutil.EncodeId(c.id)
}
