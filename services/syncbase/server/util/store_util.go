// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"os"
	"strconv"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/store/leveldb"
	"v.io/syncbase/x/ref/services/syncbase/store/memstore"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/v23/vom"
)

func FormatVersion(version uint64) string {
	return strconv.FormatUint(version, 10)
}

func CheckVersion(ctx *context.T, presented string, actual uint64) error {
	if presented != "" && presented != FormatVersion(actual) {
		return verror.NewErrBadVersion(ctx)
	}
	return nil
}

////////////////////////////////////////////////////////////
// RPC-aware, higher-level get/put

type Layer interface {
	// Name returns the name of this instance, e.g. "fooapp" or "bardb".
	Name() string
	// StKey returns the storage engine key to use for metadata about this layer,
	// e.g. "$table:baztable".
	StKey() string
}

type Permser interface {
	// GetPerms returns the Permissions for this Layer.
	GetPerms() access.Permissions
}

// GetWithoutAuth does st.Get(l.StKey(), v), populating v.
// Returns a VDL-compatible error.
func GetWithoutAuth(ctx *context.T, st store.StoreReader, l Layer, v interface{}) error {
	if err := GetObject(st, l.StKey(), v); err != nil {
		if verror.ErrorID(err) == store.ErrUnknownKey.ID {
			return verror.New(verror.ErrNoExist, ctx, l.Name())
		}
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// Get does GetWithoutAuth followed by an auth check.
// Returns a VDL-compatible error.
func Get(ctx *context.T, call rpc.ServerCall, st store.StoreReader, l Layer, v Permser) error {
	if err := GetWithoutAuth(ctx, st, l, v); err != nil {
		return err
	}
	auth, _ := access.PermissionsAuthorizer(v.GetPerms(), access.TypicalTagType())
	if err := auth.Authorize(ctx, call.Security()); err != nil {
		return verror.New(verror.ErrNoAccess, ctx, l.Name())
	}
	return nil
}

// Put does st.Put(l.StKey(), v).
// Returns a VDL-compatible error.
// If you need to perform an authorization check, use Update().
func Put(ctx *context.T, st store.StoreWriter, l Layer, v interface{}) error {
	if err := PutObject(st, l.StKey(), v); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// Delete does st.Delete(l.StKey()).
// Returns a VDL-compatible error.
// If you need to perform an authorization check, call Get() first.
func Delete(ctx *context.T, st store.StoreWriter, l Layer) error {
	if err := st.Delete([]byte(l.StKey())); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// Update performs a read-modify-write.
// Input v is populated by the "read" step. fn should "modify" v, and should
// return a VDL-compatible error.
// Performs an auth check as part of the "read" step.
// Returns a VDL-compatible error.
func Update(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter, l Layer, v Permser, fn func() error) error {
	_ = st.(store.Transaction) // panics on failure, as desired
	if err := Get(ctx, call, st, l, v); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	return Put(ctx, st, l, v)
}

////////////////////////////////////////////////////////////
// RPC-oblivious, lower-level helpers

func GetObject(st store.StoreReader, k string, v interface{}) error {
	bytes, err := st.Get([]byte(k), nil)
	if err != nil {
		return err
	}
	return vom.Decode(bytes, v)
}

func PutObject(st store.StoreWriter, k string, v interface{}) error {
	bytes, err := vom.Encode(v)
	if err != nil {
		return err
	}
	return st.Put([]byte(k), bytes)
}

type OpenOptions struct {
	CreateIfMissing bool
	ErrorIfExists   bool
}

// OpenStore opens the given store.Store. OpenOptions are respected to the
// degree possible for the specified engine.
func OpenStore(engine, path string, opts OpenOptions) (store.Store, error) {
	switch engine {
	case "memstore":
		if !opts.CreateIfMissing {
			return nil, verror.New(verror.ErrInternal, nil, "cannot open memstore")
		}
		// By definition, the memstore does not already exist.
		return memstore.New(), nil
	case "leveldb":
		leveldbOpts := leveldb.OpenOptions{
			CreateIfMissing: opts.CreateIfMissing,
			ErrorIfExists:   opts.ErrorIfExists,
		}
		if opts.CreateIfMissing {
			// Note, os.MkdirAll is a noop if the path already exists. We rely on
			// leveldb to enforce ErrorIfExists.
			if err := os.MkdirAll(path, 0700); err != nil {
				return nil, verror.New(verror.ErrInternal, nil, err)
			}
		}
		return leveldb.Open(path, leveldbOpts)
	default:
		return nil, verror.New(verror.ErrBadArg, nil, engine)
	}
}

func DestroyStore(engine, path string) error {
	switch engine {
	case "memstore":
		// memstore doesn't persist any data on the disc, do nothing.
		return nil
	case "leveldb":
		if err := os.RemoveAll(path); err != nil {
			return verror.New(verror.ErrInternal, nil, err)
		}
		return nil
	default:
		return verror.New(verror.ErrBadArg, nil, engine)
	}
}
