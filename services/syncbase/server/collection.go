// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/v23/vom"
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

func (c *collectionReq) Create(ctx *context.T, call rpc.ServerCall, schemaVersion int32, perms access.Permissions) error {
	if c.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}
	return store.RunInTransaction(c.d.st, func(tx store.Transaction) error {
		// Check DatabaseData perms.
		dData := &DatabaseData{}
		if err := util.GetWithAuth(ctx, call, tx, c.d.stKey(), dData); err != nil {
			return err
		}
		// Check for "collection already exists".
		if err := store.Get(ctx, tx, c.stKey(), &CollectionData{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			return verror.New(verror.ErrExist, ctx, c.name)
		}
		if perms == nil {
			perms = dData.Perms
		}
		// Write new CollectionData.
		data := &CollectionData{
			Name:  c.name,
			Perms: perms,
		}
		if err := store.Put(ctx, tx, c.stKey(), data); err != nil {
			return err
		}
		// Write empty prefix permissions.
		if err := c.updatePermsIndexForKey(ctx, tx, "", ""); err != nil {
			return err
		}
		return store.Put(ctx, tx, c.prefixPermsKey(""), perms)
	})
}

// TODO(ivanpi): Decouple collection key prefix from collection name to allow
// collection data deletion to be deferred, making deletion faster (reference
// removal). Same for database deletion.
func (c *collectionReq) Destroy(ctx *context.T, call rpc.ServerCall, schemaVersion int32) error {
	if c.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}
	return store.RunInTransaction(c.d.st, func(tx store.Transaction) error {
		// Read-check-delete CollectionData.
		if err := util.GetWithAuth(ctx, call, tx, c.stKey(), &CollectionData{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}

		// TODO(ivanpi): Check that no syncgroup includes rows in the collection.
		// Also check that all collections exist when creating/joining syncgroup.
		// In the current implementation Destroy() is protected only by the
		// Collection ACL, so there is no prefix ACL protecting row and prefix ACL
		// deletion. This is OK as long as no syncgroup covers the collection.

		// Delete all data rows without further ACL checks (note, this is different
		// from DeleteRange, which does check prefix ACLs).
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

		// Delete all prefix permissions without further ACL checks.
		it = tx.Scan(common.ScanPrefixArgs(common.JoinKeyParts(common.PermsPrefix, c.name), ""))
		for it.Advance() {
			key = it.Key(key)
			// See comment in util/constants.go for why we use SplitNKeyParts.
			parts := common.SplitNKeyParts(string(key), 3)
			externalKey := parts[2]
			// TODO(ivanpi): Optimize by deleting whole prefix perms index range
			// instead of one entry at a time.
			if err := c.UpdatePrefixPermsIndexForDelete(ctx, tx, externalKey); err != nil {
				return err
			}
			if err := tx.Delete(key); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}

		// Delete CollectionData.
		return store.Delete(ctx, tx, c.stKey())
	})
}

func (c *collectionReq) Exists(ctx *context.T, call rpc.ServerCall, schemaVersion int32) (bool, error) {
	if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return false, err
	}
	return util.ErrorToExists(util.GetWithAuth(ctx, call, c.d.st, c.stKey(), &CollectionData{}))
}

func (c *collectionReq) GetPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32) (perms access.Permissions, err error) {
	impl := func(sntx store.SnapshotOrTransaction) (perms access.Permissions, err error) {
		if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return nil, err
		}
		data := &CollectionData{}
		if err := util.GetWithAuth(ctx, call, sntx, c.stKey(), data); err != nil {
			return nil, err
		}
		return data.Perms, nil
	}
	if c.d.batchId != nil {
		return impl(c.d.batchReader())
	} else {
		sn := c.d.st.NewSnapshot()
		defer sn.Abort()
		return impl(sn)
	}
}

func (c *collectionReq) SetPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, perms access.Permissions) error {
	impl := func(tx store.Transaction) error {
		if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		data := &CollectionData{}
		return util.UpdateWithAuth(ctx, call, tx, c.stKey(), data, func() error {
			data.Perms = perms
			return nil
		})
	}
	if c.d.batchId != nil {
		if tx, err := c.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return store.RunInTransaction(c.d.st, impl)
	}
}

func (c *collectionReq) DeleteRange(ctx *context.T, call rpc.ServerCall, schemaVersion int32, start, limit []byte) error {
	impl := func(tx *watchable.Transaction) error {
		// Check for collection-level access before doing a scan.
		if _, err := c.checkAccess(ctx, call, tx, ""); err != nil {
			return err
		}
		// Check if the db schema version and the version provided by client
		// matches.
		if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		it := tx.Scan(common.ScanRangeArgs(common.JoinKeyParts(common.RowPrefix, c.name), string(start), string(limit)))
		key := []byte{}
		for it.Advance() {
			key = it.Key(key)
			// Check perms.
			// See comment in util/constants.go for why we use SplitNKeyParts.
			parts := common.SplitNKeyParts(string(key), 3)
			externalKey := parts[2]
			permsPrefix, err := c.checkAccess(ctx, call, tx, externalKey)
			if err != nil {
				// TODO(rogulenko): Revisit this behavior. Probably we should
				// delete all rows that we have access to.
				it.Cancel()
				return err
			}
			// Delete the key-value pair.
			if err := watchable.DeleteWithPerms(tx, key, c.prefixPermsKey(permsPrefix)); err != nil {
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

func (c *collectionReq) Scan(ctx *context.T, call wire.CollectionScanServerCall, schemaVersion int32, start, limit []byte) error {
	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check for collection-level access before doing a scan.
		if _, err := c.checkAccess(ctx, call, sntx, ""); err != nil {
			return err
		}
		if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		it := sntx.Scan(common.ScanRangeArgs(common.JoinKeyParts(common.RowPrefix, c.name), string(start), string(limit)))
		sender := call.SendStream()
		key, value := []byte{}, []byte{}
		for it.Advance() {
			key, value = it.Key(key), it.Value(value)
			// Check perms.
			// See comment in util/constants.go for why we use SplitNKeyParts.
			parts := common.SplitNKeyParts(string(key), 3)
			externalKey := parts[2]
			if _, err := c.checkAccess(ctx, call, sntx, externalKey); err != nil {
				it.Cancel()
				return err
			}
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

func (c *collectionReq) GetPrefixPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, key string) ([]wire.PrefixPermissions, error) {
	impl := func(sntx store.SnapshotOrTransaction) ([]wire.PrefixPermissions, error) {
		// Check permissions only at collection level.
		if _, err := c.checkAccess(ctx, call, sntx, ""); err != nil {
			return nil, err
		}
		if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return nil, err
		}
		// Get the most specific permissions object.
		prefix, parent, perms, err := c.prefixPermsForKey(ctx, sntx, key)
		if err != nil {
			return nil, err
		}
		result := []wire.PrefixPermissions{{Prefix: prefix, Perms: perms}}
		// Collect all parent permissions objects all the way up to the collection
		// level.
		for prefix != "" {
			prefix = parent
			if parent, err = c.parentForPrefix(ctx, sntx, prefix); err != nil {
				return nil, err
			}
			if perms, err = c.permsForPrefix(ctx, sntx, prefix); err != nil {
				return nil, err
			}
			result = append(result, wire.PrefixPermissions{Prefix: prefix, Perms: perms})
		}
		return result, nil
	}
	if c.d.batchId != nil {
		return impl(c.d.batchReader())
	} else {
		sntx := c.d.st.NewSnapshot()
		defer sntx.Abort()
		return impl(sntx)
	}
}

func (c *collectionReq) SetPrefixPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, prefix string, perms access.Permissions) error {
	impl := func(tx *watchable.Transaction) error {
		parent, err := c.checkAccess(ctx, call, tx, prefix)
		if err != nil {
			return err
		}
		if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		return c.setPrefixPerms(ctx, tx, prefix, parent, perms)
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

func (c *collectionReq) DeletePrefixPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, prefix string) error {
	if prefix == "" {
		// TODO(rogulenko): Write a better return msg in this case.
		return verror.New(verror.ErrBadArg, ctx, prefix)
	}
	impl := func(tx *watchable.Transaction) error {
		parent, err := c.checkAccess(ctx, call, tx, prefix)
		if err != nil {
			return err
		}
		if err := c.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		if parent != prefix {
			// This can happen only if there is no permissions object for the given
			// prefix. Since DeletePermissions is idempotent, return nil.
			return nil
		}
		return c.deletePrefixPerms(ctx, tx, prefix)
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

func (c *collectionReq) GlobChildren__(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element) error {
	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check perms.
		if _, err := c.checkAccess(ctx, call, sntx, ""); err != nil {
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

func (c *collectionReq) UpdatePrefixPermsIndexForSet(ctx *context.T, tx store.Transaction, key string) error {
	// Concurrent transactions that touch this collection should fail with
	// ErrConcurrentTransaction when this transaction commits.
	if err := c.lock(ctx, tx); err != nil {
		return err
	}
	// Get the most specific permissions object.
	prefix, _, err := c.permsPrefixForKey(ctx, tx, key)
	if err != nil {
		return err
	}
	// In case there is no permissions object for the given prefix, we need
	// to add a new node to the prefix permissions index. We do it by updating
	// parents for all children of the prefix to the node corresponding to
	// the prefix.
	if prefix == key {
		return nil
	}
	if err := c.updateParentRefs(ctx, tx, key, key); err != nil {
		return err
	}
	return c.updatePermsIndexForKey(ctx, tx, key, prefix)
}

func (c *collectionReq) UpdatePrefixPermsIndexForDelete(ctx *context.T, tx store.Transaction, key string) error {
	// Concurrent transactions that touch this collection should fail with
	// ErrConcurrentTransaction when this transaction commits.
	if err := c.lock(ctx, tx); err != nil {
		return err
	}
	// Get the most specific permissions object.
	prefix, parent, err := c.permsPrefixForKey(ctx, tx, key)
	if err != nil {
		return err
	}
	if prefix != key {
		// This can happen only if there is no permissions object for the
		// given prefix. Since DeletePermissions is idempotent, return nil.
		return nil
	}
	// We need to delete the node corresponding to the prefix from the prefix
	// permissions index. We do it by updating parents for all children of the
	// prefix to the parent of the node corresponding to the prefix.
	if err := c.updateParentRefs(ctx, tx, key, parent); err != nil {
		return err
	}
	if err := tx.Delete([]byte(c.permsIndexStart(key))); err != nil {
		return err
	}
	return tx.Delete([]byte(c.permsIndexLimit(key)))
}

////////////////////////////////////////
// Internal helpers

func (c *collectionReq) setPrefixPerms(ctx *context.T, tx *watchable.Transaction, key, parent string, perms access.Permissions) error {
	if err := c.UpdatePrefixPermsIndexForSet(ctx, tx, key); err != nil {
		return err
	}
	return watchable.PutVomWithPerms(ctx, tx, c.prefixPermsKey(key), perms, c.prefixPermsKey(parent))
}

func (c *collectionReq) deletePrefixPerms(ctx *context.T, tx *watchable.Transaction, key string) error {
	if err := c.UpdatePrefixPermsIndexForDelete(ctx, tx, key); err != nil {
		return err
	}
	return watchable.DeleteWithPerms(tx, []byte(c.prefixPermsKey(key)), c.prefixPermsKey(key))
}

func (c *collectionReq) stKey() string {
	return common.JoinKeyParts(common.CollectionPrefix, c.stKeyPart())
}

func (c *collectionReq) stKeyPart() string {
	return c.name
}

// updatePermsIndexForKey updates the parent prefix of the given key to
// newParent in the permissions index.
func (c *collectionReq) updatePermsIndexForKey(ctx *context.T, tx store.Transaction, key, newParent string) error {
	if err := store.Put(ctx, tx, c.permsIndexStart(key), newParent); err != nil {
		return err
	}
	return store.Put(ctx, tx, c.permsIndexLimit(key), newParent)
}

// updateParentRefs updates the parent for all children of the given
// prefix to newParent.
func (c *collectionReq) updateParentRefs(ctx *context.T, tx store.Transaction, prefix, newParent string) error {
	stPrefixStart := []byte(c.permsIndexStart(prefix) + "\x00")
	stPrefixLimit := []byte(c.permsIndexLimit(prefix))
	it := tx.Scan(stPrefixStart, stPrefixLimit)
	var key []byte
	for it.Advance() {
		key = it.Key(key)
		it.Cancel()
		if err := store.Put(ctx, tx, string(key), newParent); err != nil {
			return err
		}
		it = tx.Scan([]byte(string(key)+common.PrefixRangeLimitSuffix), stPrefixLimit)
	}
	if err := it.Err(); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// lock invalidates all in-flight transactions that have touched this
// collection, such that any subsequent tx.Commit() will return
// ErrConcurrentTransaction.
//
// It is necessary to call lock() every time prefix permissions are updated so
// that snapshots inside all transactions reflect up-to-date permissions. Since
// every public function that touches this collection has to read the
// collection-level permissions object, it suffices to add the key of this
// object to the write set of the current transaction.
//
// TODO(rogulenko): Revisit this behavior to provide more granularity.
// One option is to add a prefix and its parent to the write set of the current
// transaction when the permissions object for that prefix is updated.
func (c *collectionReq) lock(ctx *context.T, tx store.Transaction) error {
	var data CollectionData
	if err := store.Get(ctx, tx, c.stKey(), &data); err != nil {
		return err
	}
	return store.Put(ctx, tx, c.stKey(), data)
}

// checkAccess checks that this collection exists in the database, and performs
// an authorization check. The access is checked at collection level and at the
// level of the most specific prefix for the given key.
// checkAccess returns the longest prefix of the given key that has associated
// permissions if the access is granted.
// TODO(rogulenko): Revisit this behavior. Eventually we'll want the
// collection-level access check to be a check for "Resolve", i.e. also check
// access to service, app and database.
func (c *collectionReq) checkAccess(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction, key string) (string, error) {
	if err := util.GetWithAuth(ctx, call, sntx, c.stKey(), &CollectionData{}); err != nil {
		return "", err
	}
	prefix, _, perms, err := c.prefixPermsForKey(ctx, sntx, key)
	if err != nil {
		return "", err
	}
	auth, _ := access.PermissionsAuthorizer(perms, access.TypicalTagType())
	if err := auth.Authorize(ctx, call.Security()); err != nil {
		return "", verror.New(verror.ErrNoAccess, ctx, prefix)
	}
	return prefix, nil
}

// permsPrefixForKey returns the longest prefix of the given key that has
// associated permissions, along with its permissions object.
// permsPrefixForKey doesn't perform an authorization check.
//
// Effectively, we represent all prefixes as a forest T, where each vertex maps
// to a prefix. A parent for a string is the maximum proper prefix of it that
// belongs to c. Each prefix P from T is represented as a pair of entries with
// keys P and P~ with parent prefix as the value. High level
// explanation of how this function works:
// 1	iter = db.Scan(K, "")
// 		Here last character of iter.Key() is removed automatically if it is '~'
// 2	if hasPrefix(K, iter.Key()) return iter.Value()
// 3	return parent(iter.Key())
// Short proof:
// iter returned on line 1 points to one of the following:
// - a string t that is equal to K;
// - a string t~: if t is not a prefix of K, then K < t < t~ which
//   contradicts with property of returned iterator on line 1 => t is prefix of
//   K; also t is the largest prefix of K, as all larger prefixes of K are
//   less than t~; in this case line 2 returns correct result;
// - a string t that doesn't end with '~': it can't be a prefix of K, as all
//   proper prefixes of K are less than K; parent(t) is a prefix of K, otherwise
//   K < parent(t) < t; parent(t) is the largest prefix of K, otherwise t is a
//   prefix of K; in this case line 3 returns correct result.
func (c *collectionReq) permsPrefixForKey(ctx *context.T, sntx store.SnapshotOrTransaction, key string) (prefix, parent string, err error) {
	it := sntx.Scan([]byte(c.permsIndexStart(key)), []byte(c.permsIndexLimit("")))
	if !it.Advance() {
		return "", "", nil
	}
	defer it.Cancel()
	// See comment in util/constants.go for why we use SplitNKeyParts.
	parts := common.SplitNKeyParts(string(it.Key(nil)), 3)
	externalKey := parts[2]
	prefix = strings.TrimSuffix(externalKey, common.PrefixRangeLimitSuffix)
	value := it.Value(nil)
	if err = vom.Decode(value, &parent); err != nil {
		return "", "", verror.New(verror.ErrInternal, ctx, err)
	}
	if strings.HasPrefix(key, prefix) {
		return prefix, parent, nil
	}
	prefix = parent
	parent, err = c.parentForPrefix(ctx, sntx, prefix)
	return prefix, parent, err
}

// parentForPrefix returns the parent prefix of the provided permissions prefix.
func (c *collectionReq) parentForPrefix(ctx *context.T, sntx store.SnapshotOrTransaction, prefix string) (string, error) {
	var parent string
	if err := store.Get(ctx, sntx, c.permsIndexStart(prefix), &parent); err != nil {
		return "", verror.New(verror.ErrInternal, ctx, err)
	}
	return parent, nil
}

// permsForPrefix returns the permissions object associated with the
// provided prefix.
func (c *collectionReq) permsForPrefix(ctx *context.T, sntx store.SnapshotOrTransaction, prefix string) (access.Permissions, error) {
	var perms access.Permissions
	if err := store.Get(ctx, sntx, c.prefixPermsKey(prefix), &perms); err != nil {
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	return perms, nil
}

// prefixPermsForKey combines permsPrefixForKey and permsForPrefix.
func (c *collectionReq) prefixPermsForKey(ctx *context.T, sntx store.SnapshotOrTransaction, key string) (prefix, parent string, perms access.Permissions, err error) {
	if prefix, parent, err = c.permsPrefixForKey(ctx, sntx, key); err != nil {
		return "", "", nil, err
	}
	perms, err = c.permsForPrefix(ctx, sntx, prefix)
	return prefix, parent, perms, err
}

// prefixPermsKey returns the key used for storing permissions for the given
// prefix in the collection.
func (c *collectionReq) prefixPermsKey(prefix string) string {
	return common.JoinKeyParts(common.PermsPrefix, c.name, prefix)
}

// permsIndexStart returns the key used for storing start of the prefix range
// in the prefix permissions index.
func (c *collectionReq) permsIndexStart(prefix string) string {
	return common.JoinKeyParts(common.PermsIndexPrefix, c.name, prefix)
}

// permsIndexLimit returns the key used for storing limit of the prefix range
// in the prefix permissions index.
func (c *collectionReq) permsIndexLimit(prefix string) string {
	return common.JoinKeyParts(common.PermsIndexPrefix, c.name, prefix) + common.PrefixRangeLimitSuffix
}
