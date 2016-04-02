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
	d    *databaseReq
}

var (
	_ wire.CollectionServerMethods = (*collectionReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (c *collectionReq) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	if c.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return store.RunInTransaction(c.d.st, func(tx store.Transaction) error {
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
	})
}

// TODO(ivanpi): Decouple collection key prefix from collection name to allow
// collection data deletion to be deferred, making deletion faster (reference
// removal). Same for database deletion.
func (c *collectionReq) Destroy(ctx *context.T, call rpc.ServerCall) error {
	if c.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return store.RunInTransaction(c.d.st, func(tx store.Transaction) error {
		// Read-check-delete CollectionPerms.
		if err := util.GetWithAuth(ctx, call, tx, c.permsKey(), &CollectionPerms{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}

		// TODO(ivanpi): Check that no syncgroup includes the collection being
		// destroyed. Also check that all collections exist when creating a
		// syncgroup.

		// Delete all data rows without logging the collection ACL version allowing
		// the operation (note, this is different from DeleteRange, which does log
		// the ACL version).
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
		// Delete CollectionPerms without logging the ACL version allowing the
		// operation.
		return store.Delete(ctx, tx, c.permsKey())
	})
}

func (c *collectionReq) Exists(ctx *context.T, call rpc.ServerCall) (bool, error) {
	return util.ErrorToExists(util.GetWithAuth(ctx, call, c.d.st, c.permsKey(), &CollectionPerms{}))
}

func (c *collectionReq) GetPermissions(ctx *context.T, call rpc.ServerCall) (perms access.Permissions, err error) {
	impl := func(sntx store.SnapshotOrTransaction) (perms access.Permissions, err error) {
		var storedPerms CollectionPerms
		if err := util.GetWithAuth(ctx, call, sntx, c.permsKey(), &storedPerms); err != nil {
			return nil, err
		}
		return access.Permissions(storedPerms), nil
	}
	if c.d.batchId != nil {
		return impl(c.d.batchReader())
	} else {
		sn := c.d.st.NewSnapshot()
		defer sn.Abort()
		return impl(sn)
	}
}

func (c *collectionReq) SetPermissions(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	impl := func(tx *watchable.Transaction) error {
		if err := c.checkAccess(ctx, call, tx); err != nil {
			return err
		}
		storedPerms := CollectionPerms(perms)
		return watchable.PutVomWithPerms(ctx, tx, c.permsKey(), &storedPerms, c.permsKey())
	}
	if c.d.batchId != nil {
		if tx, err := c.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return watchable.RunInTransaction(c.d.st, impl)
	}
}

func (c *collectionReq) DeleteRange(ctx *context.T, call rpc.ServerCall, start, limit []byte) error {
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
			if err := watchable.DeleteWithPerms(tx, key, c.permsKey()); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	if c.d.batchId != nil {
		if tx, err := c.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return watchable.RunInTransaction(c.d.st, impl)
	}
}

func (c *collectionReq) Scan(ctx *context.T, call wire.CollectionScanServerCall, start, limit []byte) error {
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
	if c.d.batchId != nil {
		return impl(c.d.batchReader())
	} else {
		sntx := c.d.st.NewSnapshot()
		defer sntx.Abort()
		return impl(sntx)
	}
}

func (c *collectionReq) GlobChildren__(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element) error {
	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check perms.
		if err := c.checkAccess(ctx, call, sntx); err != nil {
			return err
		}
		return util.GlobChildren(ctx, call, matcher, sntx, common.JoinKeyParts(common.RowPrefix, c.name))
	}
	if c.d.batchId != nil {
		return impl(c.d.batchReader())
	} else {
		sn := c.d.st.NewSnapshot()
		defer sn.Abort()
		return impl(sn)
	}
}

////////////////////////////////////////
// interfaces.Database methods

func (c *collectionReq) Database() interfaces.Database {
	return c.d.database
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
// access to service, app and database.
func (c *collectionReq) checkAccess(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction) error {
	if err := util.GetWithAuth(ctx, call, sntx, c.permsKey(), &CollectionPerms{}); err != nil {
		return err
	}
	return nil
}
