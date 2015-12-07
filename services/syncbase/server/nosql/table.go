// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase/nosql"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
)

// tableReq is a per-request object that handles Table RPCs.
type tableReq struct {
	name string
	d    *databaseReq
}

var (
	_ wire.TableServerMethods = (*tableReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (t *tableReq) Create(ctx *context.T, call rpc.ServerCall, schemaVersion int32, perms access.Permissions) error {
	if t.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}
	return store.RunInTransaction(t.d.st, func(tx store.Transaction) error {
		// Check DatabaseData perms.
		dData := &DatabaseData{}
		if err := util.GetWithAuth(ctx, call, tx, t.d.stKey(), dData); err != nil {
			return err
		}
		// Check for "table already exists".
		if err := util.Get(ctx, tx, t.stKey(), &TableData{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			return verror.New(verror.ErrExist, ctx, t.name)
		}
		if perms == nil {
			perms = dData.Perms
		}
		// Write new TableData.
		data := &TableData{
			Name:  t.name,
			Perms: perms,
		}
		if err := util.Put(ctx, tx, t.stKey(), data); err != nil {
			return err
		}
		// Write empty prefix permissions.
		if err := t.updatePermsIndexForKey(ctx, tx, "", ""); err != nil {
			return err
		}
		return util.Put(ctx, tx, t.prefixPermsKey(""), perms)
	})
}

// TODO(ivanpi): Decouple table key prefix from table name to allow table data
// deletion to be deferred, making deletion faster (reference removal). Same
// for database deletion.
func (t *tableReq) Destroy(ctx *context.T, call rpc.ServerCall, schemaVersion int32) error {
	if t.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}
	return store.RunInTransaction(t.d.st, func(tx store.Transaction) error {
		// Read-check-delete TableData.
		if err := util.GetWithAuth(ctx, call, tx, t.stKey(), &TableData{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}

		// TODO(ivanpi): Check that no syncgroup includes rows in the table.
		// Also check that all tables exist when creating/joining syncgroup.
		// In the current implementation Destroy() is protected only by the
		// Table ACL, so there is no prefix ACL protecting row and prefix ACL
		// deletion. This is OK as long as no syncgroup covers the table.

		// Delete all data rows without further ACL checks (note, this is different
		// from DeleteRange, which does check prefix ACLs).
		it := tx.Scan(util.ScanPrefixArgs(util.JoinKeyParts(util.RowPrefix, t.name), ""))
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
		it = tx.Scan(util.ScanPrefixArgs(util.JoinKeyParts(util.PermsPrefix, t.name), ""))
		for it.Advance() {
			key = it.Key(key)
			// See comment in util/constants.go for why we use SplitNKeyParts.
			parts := util.SplitNKeyParts(string(key), 3)
			externalKey := parts[2]
			// TODO(ivanpi): Optimize by deleting whole prefix perms index range
			// instead of one entry at a time.
			if err := t.UpdatePrefixPermsIndexForDelete(ctx, tx, externalKey); err != nil {
				return err
			}
			if err := tx.Delete(key); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}

		// Delete TableData.
		return util.Delete(ctx, tx, t.stKey())
	})
}

func (t *tableReq) Exists(ctx *context.T, call rpc.ServerCall, schemaVersion int32) (bool, error) {
	if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return false, err
	}
	return util.ErrorToExists(util.GetWithAuth(ctx, call, t.d.st, t.stKey(), &TableData{}))
}

func (t *tableReq) GetPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32) (perms access.Permissions, err error) {
	impl := func(sntx store.SnapshotOrTransaction) (perms access.Permissions, err error) {
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return nil, err
		}
		data := &TableData{}
		if err := util.GetWithAuth(ctx, call, sntx, t.stKey(), data); err != nil {
			return nil, err
		}
		return data.Perms, nil
	}
	if t.d.batchId != nil {
		return impl(t.d.batchReader())
	} else {
		sn := t.d.st.NewSnapshot()
		defer sn.Abort()
		return impl(sn)
	}
}

func (t *tableReq) SetPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, perms access.Permissions) error {
	impl := func(tx store.Transaction) error {
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		data := &TableData{}
		return util.UpdateWithAuth(ctx, call, tx, t.stKey(), data, func() error {
			data.Perms = perms
			return nil
		})
	}
	if t.d.batchId != nil {
		if tx, err := t.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) DeleteRange(ctx *context.T, call rpc.ServerCall, schemaVersion int32, start, limit []byte) error {
	impl := func(tx store.Transaction) error {
		// Check for table-level access before doing a scan.
		if _, err := t.checkAccess(ctx, call, tx, ""); err != nil {
			return err
		}
		// Check if the db schema version and the version provided by client
		// matches.
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		it := tx.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.name), string(start), string(limit)))
		key := []byte{}
		for it.Advance() {
			key = it.Key(key)
			// Check perms.
			// See comment in util/constants.go for why we use SplitNKeyParts.
			parts := util.SplitNKeyParts(string(key), 3)
			externalKey := parts[2]
			permsPrefix, err := t.checkAccess(ctx, call, tx, externalKey)
			if err != nil {
				// TODO(rogulenko): Revisit this behavior. Probably we should
				// delete all rows that we have access to.
				it.Cancel()
				return err
			}
			// Delete the key-value pair.
			if err := watchable.DeleteWithPerms(tx, key, t.prefixPermsKey(permsPrefix)); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	if t.d.batchId != nil {
		if tx, err := t.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) Scan(ctx *context.T, call wire.TableScanServerCall, schemaVersion int32, start, limit []byte) error {
	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check for table-level access before doing a scan.
		if _, err := t.checkAccess(ctx, call, sntx, ""); err != nil {
			return err
		}
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		it := sntx.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.name), string(start), string(limit)))
		sender := call.SendStream()
		key, value := []byte{}, []byte{}
		for it.Advance() {
			key, value = it.Key(key), it.Value(value)
			// Check perms.
			// See comment in util/constants.go for why we use SplitNKeyParts.
			parts := util.SplitNKeyParts(string(key), 3)
			externalKey := parts[2]
			if _, err := t.checkAccess(ctx, call, sntx, externalKey); err != nil {
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
	if t.d.batchId != nil {
		return impl(t.d.batchReader())
	} else {
		sntx := t.d.st.NewSnapshot()
		defer sntx.Abort()
		return impl(sntx)
	}
}

func (t *tableReq) GetPrefixPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, key string) ([]wire.PrefixPermissions, error) {
	impl := func(sntx store.SnapshotOrTransaction) ([]wire.PrefixPermissions, error) {
		// Check permissions only at table level.
		if _, err := t.checkAccess(ctx, call, sntx, ""); err != nil {
			return nil, err
		}
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return nil, err
		}
		// Get the most specific permissions object.
		prefix, parent, perms, err := t.prefixPermsForKey(ctx, sntx, key)
		if err != nil {
			return nil, err
		}
		result := []wire.PrefixPermissions{{Prefix: prefix, Perms: perms}}
		// Collect all parent permissions objects all the way up to the table level.
		for prefix != "" {
			prefix = parent
			if parent, err = t.parentForPrefix(ctx, sntx, prefix); err != nil {
				return nil, err
			}
			if perms, err = t.permsForPrefix(ctx, sntx, prefix); err != nil {
				return nil, err
			}
			result = append(result, wire.PrefixPermissions{Prefix: prefix, Perms: perms})
		}
		return result, nil
	}
	if t.d.batchId != nil {
		return impl(t.d.batchReader())
	} else {
		sntx := t.d.st.NewSnapshot()
		defer sntx.Abort()
		return impl(sntx)
	}
}

func (t *tableReq) SetPrefixPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, prefix string, perms access.Permissions) error {
	impl := func(tx store.Transaction) error {
		parent, err := t.checkAccess(ctx, call, tx, prefix)
		if err != nil {
			return err
		}
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		return t.setPrefixPerms(ctx, tx, prefix, parent, perms)
	}
	if t.d.batchId != nil {
		if tx, err := t.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) DeletePrefixPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, prefix string) error {
	if prefix == "" {
		// TODO(rogulenko): Write a better return msg in this case.
		return verror.New(verror.ErrBadArg, ctx, prefix)
	}
	impl := func(tx store.Transaction) error {
		parent, err := t.checkAccess(ctx, call, tx, prefix)
		if err != nil {
			return err
		}
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		if parent != prefix {
			// This can happen only if there is no permissions object for the
			// given prefix. Since DeletePermissions is idempotent, return nil.
			return nil
		}
		return t.deletePrefixPerms(ctx, tx, prefix)
	}
	if t.d.batchId != nil {
		if tx, err := t.d.batchTransaction(); err != nil {
			return err
		} else {
			return impl(tx)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) GlobChildren__(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element) error {
	impl := func(sntx store.SnapshotOrTransaction) error {
		// Check perms.
		if _, err := t.checkAccess(ctx, call, sntx, ""); err != nil {
			return err
		}
		return util.GlobChildren(ctx, call, matcher, sntx, util.JoinKeyParts(util.RowPrefix, t.name))
	}
	if t.d.batchId != nil {
		return impl(t.d.batchReader())
	} else {
		sn := t.d.st.NewSnapshot()
		defer sn.Abort()
		return impl(sn)
	}
}

////////////////////////////////////////
// interfaces.Database methods

func (t *tableReq) Database() interfaces.Database {
	return t.d.database
}

func (t *tableReq) Name() string {
	return t.name
}

func (t *tableReq) UpdatePrefixPermsIndexForSet(ctx *context.T, tx store.Transaction, key string) error {
	// Concurrent transactions that touch this table should fail with
	// ErrConcurrentTransaction when this transaction commits.
	if err := t.lock(ctx, tx); err != nil {
		return err
	}
	// Get the most specific permissions object.
	prefix, _, err := t.permsPrefixForKey(ctx, tx, key)
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
	if err := t.updateParentRefs(ctx, tx, key, key); err != nil {
		return err
	}
	return t.updatePermsIndexForKey(ctx, tx, key, prefix)
}

func (t *tableReq) UpdatePrefixPermsIndexForDelete(ctx *context.T, tx store.Transaction, key string) error {
	// Concurrent transactions that touch this table should fail with
	// ErrConcurrentTransaction when this transaction commits.
	if err := t.lock(ctx, tx); err != nil {
		return err
	}
	// Get the most specific permissions object.
	prefix, parent, err := t.permsPrefixForKey(ctx, tx, key)
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
	if err := t.updateParentRefs(ctx, tx, key, parent); err != nil {
		return err
	}
	if err := tx.Delete([]byte(t.permsIndexStart(key))); err != nil {
		return err
	}
	return tx.Delete([]byte(t.permsIndexLimit(key)))
}

////////////////////////////////////////
// Internal helpers

func (t *tableReq) setPrefixPerms(ctx *context.T, tx store.Transaction, key, parent string, perms access.Permissions) error {
	if err := t.UpdatePrefixPermsIndexForSet(ctx, tx, key); err != nil {
		return err
	}
	return watchable.PutVomWithPerms(ctx, tx, t.prefixPermsKey(key), perms, t.prefixPermsKey(parent))
}

func (t *tableReq) deletePrefixPerms(ctx *context.T, tx store.Transaction, key string) error {
	if err := t.UpdatePrefixPermsIndexForDelete(ctx, tx, key); err != nil {
		return err
	}
	return watchable.DeleteWithPerms(tx, []byte(t.prefixPermsKey(key)), t.prefixPermsKey(key))
}

func (t *tableReq) stKey() string {
	return util.JoinKeyParts(util.TablePrefix, t.stKeyPart())
}

func (t *tableReq) stKeyPart() string {
	return t.name
}

// updatePermsIndexForKey updates the parent prefix of the given key to
// newParent in the permissions index.
func (t *tableReq) updatePermsIndexForKey(ctx *context.T, tx store.Transaction, key, newParent string) error {
	if err := util.Put(ctx, tx, t.permsIndexStart(key), newParent); err != nil {
		return err
	}
	return util.Put(ctx, tx, t.permsIndexLimit(key), newParent)
}

// updateParentRefs updates the parent for all children of the given
// prefix to newParent.
func (t *tableReq) updateParentRefs(ctx *context.T, tx store.Transaction, prefix, newParent string) error {
	stPrefixStart := []byte(t.permsIndexStart(prefix) + "\x00")
	stPrefixLimit := []byte(t.permsIndexLimit(prefix))
	it := tx.Scan(stPrefixStart, stPrefixLimit)
	var key []byte
	for it.Advance() {
		key = it.Key(key)
		it.Cancel()
		if err := util.Put(ctx, tx, string(key), newParent); err != nil {
			return err
		}
		it = tx.Scan([]byte(string(key)+util.PrefixRangeLimitSuffix), stPrefixLimit)
	}
	if err := it.Err(); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// lock invalidates all in-flight transactions that have touched this table,
// such that any subsequent tx.Commit() will return ErrConcurrentTransaction.
//
// It is necessary to call lock() every time prefix permissions are updated so
// that snapshots inside all transactions reflect up-to-date permissions. Since
// every public function that touches this table has to read the table-level
// permissions object, it suffices to add the key of this object to the write
// set of the current transaction.
//
// TODO(rogulenko): Revisit this behavior to provide more granularity.
// One option is to add a prefix and its parent to the write set of the current
// transaction when the permissions object for that prefix is updated.
func (t *tableReq) lock(ctx *context.T, tx store.Transaction) error {
	var data TableData
	if err := util.Get(ctx, tx, t.stKey(), &data); err != nil {
		return err
	}
	return util.Put(ctx, tx, t.stKey(), data)
}

// checkAccess checks that this table exists in the database, and performs
// an authorization check. The access is checked at table level and at the
// level of the most specific prefix for the given key.
// checkAccess returns the longest prefix of the given key that has associated
// permissions if the access is granted.
// TODO(rogulenko): Revisit this behavior. Eventually we'll want the table-level
// access check to be a check for "Resolve", i.e. also check access to
// service, app and database.
func (t *tableReq) checkAccess(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction, key string) (string, error) {
	if err := util.GetWithAuth(ctx, call, sntx, t.stKey(), &TableData{}); err != nil {
		return "", err
	}
	prefix, _, perms, err := t.prefixPermsForKey(ctx, sntx, key)
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
// belongs to T. Each prefix P from T is represented as a pair of entries with
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
func (t *tableReq) permsPrefixForKey(ctx *context.T, sntx store.SnapshotOrTransaction, key string) (prefix, parent string, err error) {
	it := sntx.Scan([]byte(t.permsIndexStart(key)), []byte(t.permsIndexLimit("")))
	if !it.Advance() {
		return "", "", nil
	}
	defer it.Cancel()
	// See comment in util/constants.go for why we use SplitNKeyParts.
	parts := util.SplitNKeyParts(string(it.Key(nil)), 3)
	externalKey := parts[2]
	prefix = strings.TrimSuffix(externalKey, util.PrefixRangeLimitSuffix)
	value := it.Value(nil)
	if err = vom.Decode(value, &parent); err != nil {
		return "", "", verror.New(verror.ErrInternal, ctx, err)
	}
	if strings.HasPrefix(key, prefix) {
		return prefix, parent, nil
	}
	prefix = parent
	parent, err = t.parentForPrefix(ctx, sntx, prefix)
	return prefix, parent, err
}

// parentForPrefix returns the parent prefix of the provided permissions prefix.
func (t *tableReq) parentForPrefix(ctx *context.T, sntx store.SnapshotOrTransaction, prefix string) (string, error) {
	var parent string
	if err := util.Get(ctx, sntx, t.permsIndexStart(prefix), &parent); err != nil {
		return "", verror.New(verror.ErrInternal, ctx, err)
	}
	return parent, nil
}

// permsForPrefix returns the permissions object associated with the
// provided prefix.
func (t *tableReq) permsForPrefix(ctx *context.T, sntx store.SnapshotOrTransaction, prefix string) (access.Permissions, error) {
	var perms access.Permissions
	if err := util.Get(ctx, sntx, t.prefixPermsKey(prefix), &perms); err != nil {
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	return perms, nil
}

// prefixPermsForKey combines permsPrefixForKey and permsForPrefix.
func (t *tableReq) prefixPermsForKey(ctx *context.T, sntx store.SnapshotOrTransaction, key string) (prefix, parent string, perms access.Permissions, err error) {
	if prefix, parent, err = t.permsPrefixForKey(ctx, sntx, key); err != nil {
		return "", "", nil, err
	}
	perms, err = t.permsForPrefix(ctx, sntx, prefix)
	return prefix, parent, perms, err
}

// prefixPermsKey returns the key used for storing permissions for the given
// prefix in the table.
func (t *tableReq) prefixPermsKey(prefix string) string {
	return util.JoinKeyParts(util.PermsPrefix, t.name, prefix)
}

// permsIndexStart returns the key used for storing start of the prefix range
// in the prefix permissions index.
func (t *tableReq) permsIndexStart(prefix string) string {
	return util.JoinKeyParts(util.PermsIndexPrefix, t.name, prefix)
}

// permsIndexLimit returns the key used for storing limit of the prefix range
// in the prefix permissions index.
func (t *tableReq) permsIndexLimit(prefix string) string {
	return util.JoinKeyParts(util.PermsIndexPrefix, t.name, prefix) + util.PrefixRangeLimitSuffix
}
