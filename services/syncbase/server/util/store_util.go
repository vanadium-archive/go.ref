// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"os"
	"strconv"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/leveldb"
	"v.io/x/ref/services/syncbase/store/memstore"
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

// TODO(sadovsky): Perhaps these functions should strip key prefixes such as
// "$table:" from the error messages they return.

type Permser interface {
	// GetPerms returns the Permissions for this Layer.
	GetPerms() access.Permissions
}

// Get does st.Get(k, v) and wraps the returned error.
func Get(ctx *context.T, st store.StoreReader, k string, v interface{}) error {
	bytes, err := st.Get([]byte(k), nil)
	if err != nil {
		if verror.ErrorID(err) == store.ErrUnknownKey.ID {
			return verror.New(verror.ErrNoExist, ctx, k)
		}
		return verror.New(verror.ErrInternal, ctx, err)
	}
	if err = vom.Decode(bytes, v); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// Exists returns true if the key exists in the store.
// TODO(rdaoud): for now it only bypasses the Get's VOM decode step.  It should
// be optimized further by adding a st.Exists(k) API and let each implementation
// do its best to reduce data fetching in its key lookup.
func Exists(ctx *context.T, st store.StoreReader, k string) (bool, error) {
	_, err := st.Get([]byte(k), nil)
	if err != nil {
		if verror.ErrorID(err) == store.ErrUnknownKey.ID {
			return false, nil
		}
		return false, verror.New(verror.ErrInternal, ctx, err)
	}
	return true, nil
}

// GetWithAuth does Get followed by an auth check.
func GetWithAuth(ctx *context.T, call rpc.ServerCall, st store.StoreReader, k string, v Permser) error {
	if err := Get(ctx, st, k, v); err != nil {
		return err
	}
	auth, _ := access.PermissionsAuthorizer(v.GetPerms(), access.TypicalTagType())
	if err := auth.Authorize(ctx, call.Security()); err != nil {
		return verror.New(verror.ErrNoAccess, ctx, err)
	}
	return nil
}

// Put does stw.Put(k, v) and wraps the returned error.
func Put(ctx *context.T, stw store.StoreWriter, k string, v interface{}) error {
	bytes, err := vom.Encode(v)
	if err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	if err = stw.Put([]byte(k), bytes); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// Delete does stw.Delete(k, v) and wraps the returned error.
func Delete(ctx *context.T, stw store.StoreWriter, k string) error {
	if err := stw.Delete([]byte(k)); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// UpdateWithAuth performs a read-modify-write.
// Input v is populated by the "read" step. fn should "modify" v.
// Performs an auth check as part of the "read" step.
func UpdateWithAuth(ctx *context.T, call rpc.ServerCall, tx store.Transaction, k string, v Permser, fn func() error) error {
	if err := GetWithAuth(ctx, call, tx, k, v); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	return Put(ctx, tx, k, v)
}

// Wraps a call to Get and returns true if Get found the object, false
// otherwise, suppressing ErrNoExist. Access errors are suppressed as well
// because they imply existence in some Get implementations.
// TODO(ivanpi): Revisit once ACL specification is finalized.
func ErrorToExists(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	switch verror.ErrorID(err) {
	case verror.ErrNoExist.ID:
		return false, nil
	case verror.ErrNoAccess.ID, verror.ErrNoExistOrNoAccess.ID:
		return false, nil
	default:
		return false, err
	}
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
		st, err := leveldb.Open(path, leveldbOpts)
		if err != nil {
			if strings.Contains(err.Error(), "Corruption") {
				vlog.Errorf("leveldb %s is corrupt.  Moving aside. %v", path, err)
				return nil, handleCorruptLevelDB(path)
			}
		}
		return st, err
	default:
		return nil, verror.New(verror.ErrBadArg, nil, engine)
	}
}

// DestroyStore destroys the specified store. Idempotent.
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

// Moves a corrupt store aside.  The app will be responsible for creating a new
// one. Returns an error containing the path of the old store in case the user
// wants to try to debug it.
func handleCorruptLevelDB(path string) error {
	newPath := path + ".corrupt." + time.Now().Format(time.RFC3339)
	if err := os.Rename(path, newPath); err != nil {
		return verror.New(verror.ErrInternal, nil, "leveldb corrupt but could not move aside: "+err.Error())
	}
	return wire.NewErrCorruptDatabase(nil, newPath)
}
