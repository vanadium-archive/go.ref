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
	"v.io/x/ref/services/syncbase/server/util"
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
		// Check databaseData perms.
		dData := &databaseData{}
		if err := util.GetWithAuth(ctx, call, tx, t.d.stKey(), dData); err != nil {
			return err
		}
		// Check for "table already exists".
		if err := util.Get(ctx, tx, t.stKey(), &tableData{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			return verror.New(verror.ErrExist, ctx, t.name)
		}
		if perms == nil {
			perms = dData.Perms
		}
		// Write empty prefix permissions.
		if err := util.Put(ctx, tx, t.prefixPermsKey(""), stPrefixPerms{Perms: perms}); err != nil {
			return err
		}
		if err := util.Put(ctx, tx, t.prefixPermsKey("")+util.PrefixRangeLimitSuffix, stPrefixPerms{Perms: perms}); err != nil {
			return err
		}
		// Write new tableData.
		data := &tableData{
			Name:  t.name,
			Perms: perms,
		}
		return util.Put(ctx, tx, t.stKey(), data)
	})
}

func (t *tableReq) Destroy(ctx *context.T, call rpc.ServerCall, schemaVersion int32) error {
	if t.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}
	return store.RunInTransaction(t.d.st, func(tx store.Transaction) error {
		// Read-check-delete tableData.
		if err := util.GetWithAuth(ctx, call, tx, t.stKey(), &tableData{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}
		// TODO(sadovsky): Delete all rows in this table.
		if err := util.Delete(ctx, tx, t.prefixPermsKey("")); err != nil {
			return err
		}
		if err := util.Delete(ctx, tx, t.prefixPermsKey("")+util.PrefixRangeLimitSuffix); err != nil {
			return err
		}
		return util.Delete(ctx, tx, t.stKey())
	})
}

func (t *tableReq) Exists(ctx *context.T, call rpc.ServerCall, schemaVersion int32) (bool, error) {
	if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
		return false, err
	}
	return util.ErrorToExists(util.GetWithAuth(ctx, call, t.d.st, t.stKey(), &tableData{}))
}

func (t *tableReq) GetPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32) (perms access.Permissions, err error) {
	impl := func(sntx store.SnapshotOrTransaction) (perms access.Permissions, err error) {
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return nil, err
		}
		data := &tableData{}
		if err := util.GetWithAuth(ctx, call, sntx, t.stKey(), data); err != nil {
			return nil, err
		}
		return data.Perms, nil
	}
	if t.d.batchId != nil {
		return impl(t.d.batchReader())
	} else {
		sntx := t.d.st.NewSnapshot()
		defer sntx.Abort()
		return impl(sntx)
	}
}

func (t *tableReq) SetPermissions(ctx *context.T, call rpc.ServerCall, schemaVersion int32, perms access.Permissions) error {
	impl := func(tx store.Transaction) error {
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		data := &tableData{}
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
		if err := t.checkAccess(ctx, call, tx, ""); err != nil {
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
			parts := util.SplitKeyParts(string(key))
			externalKey := parts[len(parts)-1]
			if err := t.checkAccess(ctx, call, tx, externalKey); err != nil {
				// TODO(rogulenko): Revisit this behavior. Probably we should
				// delete all rows that we have access to.
				it.Cancel()
				return err
			}
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
		if err := t.checkAccess(ctx, call, sntx, ""); err != nil {
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
			parts := util.SplitKeyParts(string(key))
			externalKey := parts[len(parts)-1]
			if err := t.checkAccess(ctx, call, sntx, externalKey); err != nil {
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
		if err := t.checkAccess(ctx, call, sntx, ""); err != nil {
			return nil, err
		}
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return nil, err
		}
		// Get the most specific permissions object.
		prefix, prefixPerms, err := t.permsForKey(ctx, sntx, key)
		if err != nil {
			return nil, err
		}
		result := []wire.PrefixPermissions{{Prefix: prefix, Perms: prefixPerms.Perms}}
		// Collect all parent permissions objects all the way up to the table level.
		for prefix != "" {
			prefix = prefixPerms.Parent
			if prefixPerms, err = t.permsForPrefix(ctx, sntx, prefixPerms.Parent); err != nil {
				return nil, err
			}
			result = append(result, wire.PrefixPermissions{Prefix: prefix, Perms: prefixPerms.Perms})
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
		if err := t.checkAccess(ctx, call, tx, prefix); err != nil {
			return err
		}
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		// Concurrent transactions that touch this table should fail with
		// ErrConcurrentTransaction when this transaction commits.
		if err := t.lock(ctx, tx); err != nil {
			return err
		}
		// Get the most specific permissions object.
		parent, prefixPerms, err := t.permsForKey(ctx, tx, prefix)
		if err != nil {
			return err
		}
		// In case there is no permissions object for the given prefix, we need
		// to add a new node to the prefix permissions tree. We do it by updating
		// parents for all children of the prefix to the node corresponding to
		// the prefix.
		if parent != prefix {
			if err := t.updateParentRefs(ctx, tx, prefix, prefix); err != nil {
				return err
			}
		} else {
			parent = prefixPerms.Parent
		}
		stPrefixStart := t.prefixPermsKey(prefix)
		stPrefixLimit := t.prefixPermsKey(prefix) + util.PrefixRangeLimitSuffix
		prefixPerms = stPrefixPerms{Parent: parent, Perms: perms}
		// Put the (prefix, perms) pair to the database.
		if err := util.Put(ctx, tx, stPrefixStart, prefixPerms); err != nil {
			return err
		}
		return util.Put(ctx, tx, stPrefixLimit, prefixPerms)
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
		if err := t.checkAccess(ctx, call, tx, prefix); err != nil {
			return err
		}
		if err := t.d.checkSchemaVersion(ctx, schemaVersion); err != nil {
			return err
		}
		// Concurrent transactions that touch this table should fail with
		// ErrConcurrentTransaction when this transaction commits.
		if err := t.lock(ctx, tx); err != nil {
			return err
		}
		// Get the most specific permissions object.
		parent, prefixPerms, err := t.permsForKey(ctx, tx, prefix)
		if err != nil {
			return err
		}
		if parent != prefix {
			// This can happen only if there is no permissions object for the
			// given prefix. Since DeletePermissions is idempotent, return nil.
			return nil
		}
		// We need to delete the node corresponding to the prefix from the prefix
		// permissions tree. We do it by updating parents for all children of the
		// prefix to the parent of the node corresponding to the prefix.
		if err := t.updateParentRefs(ctx, tx, prefix, prefixPerms.Parent); err != nil {
			return err
		}
		stPrefixStart := []byte(t.prefixPermsKey(prefix))
		stPrefixLimit := []byte(t.prefixPermsKey(prefix) + util.PrefixRangeLimitSuffix)
		if err := tx.Delete(stPrefixStart); err != nil {
			return err
		}
		return tx.Delete(stPrefixLimit)
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
	impl := func(sntx store.SnapshotOrTransaction, closeSntx func() error) error {
		// Check perms.
		if err := t.checkAccess(ctx, call, sntx, ""); err != nil {
			closeSntx()
			return err
		}
		return util.GlobChildren(ctx, call, matcher, sntx, closeSntx, util.JoinKeyParts(util.RowPrefix, t.name))
	}
	if t.d.batchId != nil {
		return impl(t.d.batchReader(), func() error {
			return nil
		})
	} else {
		sn := t.d.st.NewSnapshot()
		return impl(sn, sn.Abort)
	}
}

////////////////////////////////////////
// Internal helpers

func (t *tableReq) stKey() string {
	return util.JoinKeyParts(util.TablePrefix, t.stKeyPart())
}

func (t *tableReq) stKeyPart() string {
	return t.name
}

// updateParentRefs updates the parent for all children of the given
// prefix to newParent.
func (t *tableReq) updateParentRefs(ctx *context.T, tx store.Transaction, prefix, newParent string) error {
	stPrefixStart := []byte(t.prefixPermsKey(prefix) + "\x00")
	stPrefixLimit := []byte(t.prefixPermsKey(prefix) + util.PrefixRangeLimitSuffix)
	it := tx.Scan(stPrefixStart, stPrefixLimit)
	var key, value []byte
	for it.Advance() {
		key, value = it.Key(key), it.Value(value)
		it.Cancel()
		var prefixPerms stPrefixPerms
		if err := vom.Decode(value, &prefixPerms); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		prefixPerms.Parent = newParent
		if err := util.Put(ctx, tx, string(key), prefixPerms); err != nil {
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
	var data tableData
	if err := util.Get(ctx, tx, t.stKey(), &data); err != nil {
		return err
	}
	return util.Put(ctx, tx, t.stKey(), data)
}

// checkAccess checks that this table exists in the database, and performs
// an authorization check. The access is checked at table level and at the
// level of the most specific prefix for the given key.
// TODO(rogulenko): Revisit this behavior. Eventually we'll want the table-level
// access check to be a check for "Resolve", i.e. also check access to
// service, app and database.
func (t *tableReq) checkAccess(ctx *context.T, call rpc.ServerCall, sntx store.SnapshotOrTransaction, key string) error {
	if err := util.GetWithAuth(ctx, call, sntx, t.stKey(), &tableData{}); err != nil {
		return err
	}
	prefix, prefixPerms, err := t.permsForKey(ctx, sntx, key)
	if err != nil {
		return err
	}
	auth, _ := access.PermissionsAuthorizer(prefixPerms.Perms, access.TypicalTagType())
	if err := auth.Authorize(ctx, call.Security()); err != nil {
		return verror.New(verror.ErrNoAccess, ctx, prefix)
	}
	return nil
}

// permsForKey returns the longest prefix of the given key that has
// associated permissions, along with its permissions object.
// permsForKey doesn't perform an authorization check.
//
// Effectively, we represent all prefixes as a forest T, where each vertex maps
// to a prefix. A parent for a string is the maximum proper prefix of it that
// belongs to T. Each prefix P from T is represented as a pair of entries with
// keys P and P~ with values of type stPrefixPerms (parent + perms). High level
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
func (t *tableReq) permsForKey(ctx *context.T, sntx store.SnapshotOrTransaction, key string) (string, stPrefixPerms, error) {
	it := sntx.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.PermsPrefix, t.name), key, ""))
	if !it.Advance() {
		prefixPerms, err := t.permsForPrefix(ctx, sntx, "")
		return "", prefixPerms, err
	}
	defer it.Cancel()
	parts := util.SplitKeyParts(string(it.Key(nil)))
	prefix := strings.TrimSuffix(parts[len(parts)-1], util.PrefixRangeLimitSuffix)
	value := it.Value(nil)
	var prefixPerms stPrefixPerms
	if err := vom.Decode(value, &prefixPerms); err != nil {
		return "", stPrefixPerms{}, verror.New(verror.ErrInternal, ctx, err)
	}
	if strings.HasPrefix(key, prefix) {
		return prefix, prefixPerms, nil
	}
	parent := prefixPerms.Parent
	prefixPerms, err := t.permsForPrefix(ctx, sntx, prefixPerms.Parent)
	return parent, prefixPerms, err
}

// permsForPrefix returns the permissions object associated with the
// provided prefix.
func (t *tableReq) permsForPrefix(ctx *context.T, sntx store.SnapshotOrTransaction, prefix string) (stPrefixPerms, error) {
	var prefixPerms stPrefixPerms
	if err := util.Get(ctx, sntx, t.prefixPermsKey(prefix), &prefixPerms); err != nil {
		return stPrefixPerms{}, verror.New(verror.ErrInternal, ctx, err)
	}
	return prefixPerms, nil
}

// prefixPermsKey returns the key used for storing permissions for the given
// prefix in the table.
func (t *tableReq) prefixPermsKey(prefix string) string {
	return util.JoinKeyParts(util.PermsPrefix, t.name, prefix)
}
