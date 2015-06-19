// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"strings"

	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/v23/vom"
)

// tableReq is a per-request object that handles Table RPCs.
type tableReq struct {
	name string
	d    *databaseReq
}

var (
	_ wire.TableServerMethods = (*tableReq)(nil)
	_ util.Layer              = (*tableReq)(nil)
)

////////////////////////////////////////
// RPC methods

func (t *tableReq) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	if t.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return store.RunInTransaction(t.d.st, func(st store.StoreReadWriter) error {
		// Check databaseData perms.
		dData := &databaseData{}
		if err := util.Get(ctx, call, st, t.d, dData); err != nil {
			return err
		}
		// Check for "table already exists".
		if err := util.GetWithoutAuth(ctx, st, t, &tableData{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			// TODO(sadovsky): Should this be ErrExistOrNoAccess, for privacy?
			return verror.New(verror.ErrExist, ctx, t.name)
		}
		// Write new tableData.
		if perms == nil {
			perms = dData.Perms
		}
		data := &tableData{
			Name:  t.name,
			Perms: perms,
		}
		return util.Put(ctx, st, t, data)
	})
}

func (t *tableReq) Delete(ctx *context.T, call rpc.ServerCall) error {
	if t.d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return store.RunInTransaction(t.d.st, func(st store.StoreReadWriter) error {
		// Read-check-delete tableData.
		if err := util.Get(ctx, call, st, t, &tableData{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}
		// TODO(sadovsky): Delete all rows in this table.
		return util.Delete(ctx, st, t)
	})
}

func (t *tableReq) DeleteRowRange(ctx *context.T, call rpc.ServerCall, start, limit []byte) error {
	impl := func(st store.StoreReadWriter) error {
		// Check for table-level access before doing a scan.
		if err := t.checkAccess(ctx, call, st, ""); err != nil {
			return err
		}
		it := st.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.name), string(start), string(limit)))
		key := []byte{}
		for it.Advance() {
			key = it.Key(key)
			// Check perms.
			parts := util.SplitKeyParts(string(key))
			externalKey := parts[len(parts)-1]
			if err := t.checkAccess(ctx, call, st, externalKey); err != nil {
				// TODO(rogulenko): Revisit this behavior. Probably we should
				// delete all rows that we have access to.
				it.Cancel()
				return err
			}
			// Delete the key-value pair.
			if err := st.Delete(key); err != nil {
				return verror.New(verror.ErrInternal, ctx, err)
			}
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	if t.d.batchId != nil {
		if st, err := t.d.batchReadWriter(); err != nil {
			return err
		} else {
			return impl(st)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) Scan(ctx *context.T, call wire.TableScanServerCall, start, limit []byte) error {
	impl := func(st store.StoreReader) error {
		// Check for table-level access before doing a scan.
		if err := t.checkAccess(ctx, call, st, ""); err != nil {
			return err
		}
		it := st.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.name), string(start), string(limit)))
		sender := call.SendStream()
		key, value := []byte{}, []byte{}
		for it.Advance() {
			key, value = it.Key(key), it.Value(value)
			// Check perms.
			parts := util.SplitKeyParts(string(key))
			externalKey := parts[len(parts)-1]
			if err := t.checkAccess(ctx, call, st, externalKey); err != nil {
				it.Cancel()
				return err
			}
			sender.Send(wire.KeyValue{Key: externalKey, Value: value})
		}
		if err := it.Err(); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		return nil
	}
	var st store.StoreReader
	if t.d.batchId != nil {
		st = t.d.batchReader()
	} else {
		sn := t.d.st.NewSnapshot()
		st = sn
		defer sn.Close()
	}
	return impl(st)
}

func (t *tableReq) GetPermissions(ctx *context.T, call rpc.ServerCall, key string) ([]wire.PrefixPermissions, error) {
	impl := func(st store.StoreReader) ([]wire.PrefixPermissions, error) {
		// Check permissions only at table level.
		if err := t.checkAccess(ctx, call, st, ""); err != nil {
			return nil, err
		}
		// Get the most specific permissions object.
		prefix, prefixPerms, err := t.permsForKey(ctx, st, key)
		if err != nil {
			return nil, err
		}
		result := []wire.PrefixPermissions{{Prefix: prefix, Perms: prefixPerms.Perms}}
		// Collect all parent permissions objects all the way up to the table level.
		for prefix != "" {
			prefix = prefixPerms.Parent
			if prefixPerms, err = t.permsForPrefix(ctx, st, prefixPerms.Parent); err != nil {
				return nil, err
			}
			result = append(result, wire.PrefixPermissions{Prefix: prefix, Perms: prefixPerms.Perms})
		}
		return result, nil
	}
	var st store.StoreReader
	if t.d.batchId != nil {
		st = t.d.batchReader()
	} else {
		sn := t.d.st.NewSnapshot()
		st = sn
		defer sn.Close()
	}
	return impl(st)
}

func (t *tableReq) SetPermissions(ctx *context.T, call rpc.ServerCall, prefix string, perms access.Permissions) error {
	impl := func(st store.StoreReadWriter) error {
		if err := t.checkAccess(ctx, call, st, prefix); err != nil {
			return err
		}
		// Concurrent transactions that touch this table should fail with
		// ErrConcurrentTransaction when this transaction commits.
		if err := t.lock(ctx, st); err != nil {
			return err
		}
		if prefix == "" {
			data := &tableData{}
			return util.Update(ctx, call, st, t, data, func() error {
				data.Perms = perms
				return nil
			})
		}
		// Get the most specific permissions object.
		parent, prefixPerms, err := t.permsForKey(ctx, st, prefix)
		if err != nil {
			return err
		}
		// In case there is no permissions object for the given prefix, we need
		// to add a new node to the prefix permissions tree. We do it by updating
		// parents for all children of the prefix to the node corresponding to
		// the prefix.
		if parent != prefix {
			if err := t.updateParentRefs(ctx, st, prefix, prefix); err != nil {
				return err
			}
		} else {
			parent = prefixPerms.Parent
		}
		stPrefix := t.prefixPermsKey(prefix)
		stPrefixLimit := stPrefix + util.PrefixRangeLimitSuffix
		prefixPerms = stPrefixPerms{Parent: parent, Perms: perms}
		// Put the (prefix, perms) pair to the database.
		if err := util.PutObject(st, stPrefix, prefixPerms); err != nil {
			return err
		}
		return util.PutObject(st, stPrefixLimit, prefixPerms)
	}
	if t.d.batchId != nil {
		if st, err := t.d.batchReadWriter(); err != nil {
			return err
		} else {
			return impl(st)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) DeletePermissions(ctx *context.T, call rpc.ServerCall, prefix string) error {
	if prefix == "" {
		return verror.New(verror.ErrBadArg, ctx, prefix)
	}
	impl := func(st store.StoreReadWriter) error {
		if err := t.checkAccess(ctx, call, st, prefix); err != nil {
			return err
		}
		// Concurrent transactions that touch this table should fail with
		// ErrConcurrentTransaction when this transaction commits.
		if err := t.lock(ctx, st); err != nil {
			return err
		}
		// Get the most specific permissions object.
		parent, prefixPerms, err := t.permsForKey(ctx, st, prefix)
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
		if err := t.updateParentRefs(ctx, st, prefix, prefixPerms.Parent); err != nil {
			return err
		}
		stPrefix := []byte(t.prefixPermsKey(prefix))
		stPrefixLimit := append(stPrefix, util.PrefixRangeLimitSuffix...)
		if err := st.Delete(stPrefix); err != nil {
			return err
		}
		return st.Delete(stPrefixLimit)
	}
	if t.d.batchId != nil {
		if st, err := t.d.batchReadWriter(); err != nil {
			return err
		} else {
			return impl(st)
		}
	} else {
		return store.RunInTransaction(t.d.st, impl)
	}
}

func (t *tableReq) GlobChildren__(ctx *context.T, call rpc.ServerCall) (<-chan string, error) {
	impl := func(st store.StoreReader, closeStoreReader func() error) (<-chan string, error) {
		// Check perms.
		if err := t.checkAccess(ctx, call, st, ""); err != nil {
			closeStoreReader()
			return nil, err
		}
		// TODO(rogulenko): Check prefix permissions for children.
		return util.Glob(ctx, call, "*", st, closeStoreReader, util.JoinKeyParts(util.RowPrefix, t.name))
	}
	var st store.StoreReader
	var closeStoreReader func() error
	if t.d.batchId != nil {
		st = t.d.batchReader()
		closeStoreReader = func() error {
			return nil
		}
	} else {
		sn := t.d.st.NewSnapshot()
		st = sn
		closeStoreReader = func() error {
			return sn.Close()
		}
	}
	return impl(st, closeStoreReader)
}

////////////////////////////////////////
// util.Layer methods

func (t *tableReq) Name() string {
	return t.name
}

func (t *tableReq) StKey() string {
	return util.JoinKeyParts(util.TablePrefix, t.stKeyPart())
}

////////////////////////////////////////
// Internal helpers

func (t *tableReq) stKeyPart() string {
	return t.name
}

// updateParentRefs updates the parent for all children of the given
// prefix to newParent.
func (t *tableReq) updateParentRefs(ctx *context.T, st store.StoreReadWriter, prefix, newParent string) error {
	stPrefix := []byte(t.prefixPermsKey(prefix))
	stPrefixStart := append(stPrefix, 0)
	stPrefixLimit := append(stPrefix, util.PrefixRangeLimitSuffix...)
	it := st.Scan(stPrefixStart, stPrefixLimit)
	var key, value []byte
	for it.Advance() {
		key, value = it.Key(key), it.Value(value)
		var prefixPerms stPrefixPerms
		if err := vom.Decode(value, &prefixPerms); err != nil {
			it.Cancel()
			return verror.New(verror.ErrInternal, ctx, err)
		}
		prefixPerms.Parent = newParent
		if err := util.PutObject(st, string(key), prefixPerms); err != nil {
			it.Cancel()
			return err
		}
	}
	if err := it.Err(); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// lock invalidates all concurrent transactions with ErrConcurrentTransaction
// that have accessed this table.
// Returns a VDL-compatible error.
//
// It is necessary to call lock() every time prefix permissions are updated,
// so snapshots inside all transactions reflect up-to-date permissions. Since
// every public function that touches this table has to read the table-level
// permissions object, it is enough to add the key of table-level permissions
// to the write set of the current transaction.
//
// TODO(rogulenko): Revisit this behavior to provide more granularity.
// A possible option would be to add prefix and its parent to the write set
// of the current transaction when permissions object for a prefix is updated.
func (t *tableReq) lock(ctx *context.T, st store.StoreReadWriter) error {
	var data tableData
	if err := util.GetWithoutAuth(ctx, st, t, &data); err != nil {
		return err
	}
	return util.Put(ctx, st, t, data)
}

// checkAccess checks that this table exists in the database, and performs
// an authorization check. The access is checked at table level and at the
// level of the most specific prefix for the given key.
// Returns a VDL-compatible error.
// TODO(rogulenko): Revisit this behavior. Eventually we'll want the table-level
// access check to be a check for "Resolve", i.e. also check access to
// service, app and database.
func (t *tableReq) checkAccess(ctx *context.T, call rpc.ServerCall, st store.StoreReader, key string) error {
	prefix, prefixPerms, err := t.permsForKey(ctx, st, key)
	if err != nil {
		return err
	}
	if prefix != "" {
		if err := util.Get(ctx, call, st, t, &tableData{}); err != nil {
			return err
		}
	}
	auth, _ := access.PermissionsAuthorizer(prefixPerms.Perms, access.TypicalTagType())
	if err := auth.Authorize(ctx, call.Security()); err != nil {
		return verror.New(verror.ErrNoAccess, ctx, prefix)
	}
	return nil
}

// permsForKey returns the longest prefix of the given key that has
// associated permissions with its permissions object.
// permsForKey doesn't perform an authorization check.
// Returns a VDL-compatible error.
//
// Virtually we represent all prefixes as a forest T, where each vertex maps to
// a prefix. A parent for a string is the maximum proper prefix of it that
// belongs to T. Each prefix P from T is represented as a pair of entries with
// keys P and P~ with values of type stPrefixPerms (parent + perms).
// High level of how this function works:
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
func (t *tableReq) permsForKey(ctx *context.T, st store.StoreReader, key string) (string, stPrefixPerms, error) {
	it := st.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.PermsPrefix, t.name), key, ""))
	if !it.Advance() {
		prefixPerms, err := t.permsForPrefix(ctx, st, "")
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
	prefixPerms, err := t.permsForPrefix(ctx, st, prefixPerms.Parent)
	return prefixPerms.Parent, prefixPerms, err
}

// permsForPrefix returns the permissions object associated with the
// provided prefix.
// Returns a VDL-compatible error.
func (t *tableReq) permsForPrefix(ctx *context.T, st store.StoreReader, prefix string) (stPrefixPerms, error) {
	if prefix == "" {
		var data tableData
		if err := util.GetWithoutAuth(ctx, st, t, &data); err != nil {
			return stPrefixPerms{}, err
		}
		return stPrefixPerms{Perms: data.Perms}, nil
	}
	var prefixPerms stPrefixPerms
	if err := util.GetObject(st, t.prefixPermsKey(prefix), &prefixPerms); err != nil {
		return stPrefixPerms{}, verror.New(verror.ErrInternal, ctx, err)
	}
	return prefixPerms, nil
}

// prefixPermsKey returns the key used for storing permissions for the given
// prefix in the table.
func (t *tableReq) prefixPermsKey(prefix string) string {
	return util.JoinKeyParts(util.PermsPrefix, t.name, prefix)
}
