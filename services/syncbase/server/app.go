// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"sync"

	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	nosqlwire "v.io/v23/services/syncbase/nosql"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/nosql"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// app is a per-app singleton (i.e. not per-request) that handles App RPCs.
type app struct {
	name string
	s    *service
	// The fields below are initialized iff this app exists.
	exists bool
	// Guards the fields below. Held during database Create, Delete, and
	// SetPermissions.
	mu  sync.Mutex
	dbs map[string]interfaces.Database
}

var (
	_ wire.AppServerMethods = (*app)(nil)
	_ interfaces.App        = (*app)(nil)
)

////////////////////////////////////////
// RPC methods

// TODO(sadovsky): Require the app name to match the client's blessing name.
// I.e. reserve names at the app level of the hierarchy.
func (a *app) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	if a.exists {
		return verror.New(verror.ErrExist, ctx, a.name)
	}
	// This app does not yet exist; a is just an ephemeral handle that holds
	// {name string, s *service}. a.s.createApp will create a new app handle and
	// store it in a.s.apps[a.name].
	return a.s.createApp(ctx, call, a.name, perms)
}

func (a *app) Destroy(ctx *context.T, call rpc.ServerCall) error {
	return a.s.destroyApp(ctx, call, a.name)
}

func (a *app) Exists(ctx *context.T, call rpc.ServerCall) (bool, error) {
	if !a.exists {
		return false, nil
	}
	return util.ErrorToExists(util.GetWithAuth(ctx, call, a.s.st, a.stKey(), &AppData{}))
}

func (a *app) SetPermissions(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	if !a.exists {
		return verror.New(verror.ErrNoExist, ctx, a.name)
	}
	return a.s.setAppPerms(ctx, call, a.name, perms, version)
}

func (a *app) GetPermissions(ctx *context.T, call rpc.ServerCall) (perms access.Permissions, version string, err error) {
	if !a.exists {
		return nil, "", verror.New(verror.ErrNoExist, ctx, a.name)
	}
	data := &AppData{}
	if err := util.GetWithAuth(ctx, call, a.s.st, a.stKey(), data); err != nil {
		return nil, "", err
	}
	return data.Perms, util.FormatVersion(data.Version), nil
}

func (a *app) GlobChildren__(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element) error {
	if !a.exists {
		return verror.New(verror.ErrNoExist, ctx, a.name)
	}
	// Check perms.
	sn := a.s.st.NewSnapshot()
	defer sn.Abort()
	if err := util.GetWithAuth(ctx, call, sn, a.stKey(), &AppData{}); err != nil {
		return err
	}
	return util.GlobChildren(ctx, call, matcher, sn, common.JoinKeyParts(common.DbInfoPrefix, a.name))
}

////////////////////////////////////////
// interfaces.App methods

func (a *app) Service() interfaces.Service {
	return a.s
}

func (a *app) NoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) (interfaces.Database, error) {
	if !a.exists {
		vlog.Fatalf("app %q does not exist", a.name)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.dbs[dbName]
	if !ok {
		return nil, verror.New(verror.ErrNoExist, ctx, dbName)
	}
	return d, nil
}

func (a *app) NoSQLDatabaseNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	if !a.exists {
		vlog.Fatalf("app %q does not exist", a.name)
	}
	// In the future this API will likely be replaced by one that streams the
	// database names.
	a.mu.Lock()
	defer a.mu.Unlock()
	dbNames := make([]string, 0, len(a.dbs))
	for n := range a.dbs {
		dbNames = append(dbNames, n)
	}
	return dbNames, nil
}

func (a *app) CreateNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, metadata *nosqlwire.SchemaMetadata) (reterr error) {
	if !a.exists {
		vlog.Fatalf("app %q does not exist", a.name)
	}
	// Steps:
	// 1. Check appData perms.
	// 2. Put dbInfo record into garbage collection log, to clean up database if
	//    remaining steps fail or syncbased crashes.
	// 3. Initialize database.
	// 4. Move dbInfo from GC log into active databases. <===== CHANGE BECOMES VISIBLE
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.dbs[dbName]; ok {
		// TODO(sadovsky): Should this be ErrExistOrNoAccess, for privacy?
		return verror.New(verror.ErrExist, ctx, dbName)
	}

	// 1. Check appData perms.
	aData := &AppData{}
	if err := util.GetWithAuth(ctx, call, a.s.st, a.stKey(), aData); err != nil {
		return err
	}

	// 2. Put dbInfo record into garbage collection log, to clean up database if
	//    remaining steps fail or syncbased crashes.
	rootDir, err := a.rootDirForDb(dbName)
	if err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	dbInfo := &DbInfo{
		Name:    dbName,
		RootDir: rootDir,
		Engine:  a.s.opts.Engine,
	}
	if err := putDbGCEntry(ctx, a.s.st, dbInfo); err != nil {
		return err
	}
	var stRef store.Store
	defer func() {
		if reterr != nil {
			// Best effort database destroy on error. If it fails, it will be retried
			// on syncbased restart. (It is safe to pass nil stRef if step 3 fails.)
			// TODO(ivanpi): Consider running asynchronously. However, see TODO in
			// finalizeDatabaseDestroy.
			if err := finalizeDatabaseDestroy(ctx, a.s.st, dbInfo, stRef); err != nil {
				vlog.Error(err)
			}
		}
	}()

	// 3. Initialize database.
	if perms == nil {
		perms = aData.Perms
	}
	// TODO(ivanpi): NewDatabase doesn't close the store on failure.
	d, err := nosql.NewDatabase(ctx, a, dbName, metadata, nosql.DatabaseOptions{
		Perms:   perms,
		RootDir: dbInfo.RootDir,
		Engine:  dbInfo.Engine,
	})
	if err != nil {
		return err
	}
	// Save reference to Store to allow finalizeDatabaseDestroy to close it in
	// case of error.
	stRef = d.St()

	// 4. Move dbInfo from GC log into active databases.
	if err := store.RunInTransaction(a.s.st, func(tx store.Transaction) error {
		// Even though perms and database existence are checked in step 1 to fail
		// early before initializing the database, there are possibly rare corner
		// cases which make it prudent to repeat the checks in a transaction.
		// Recheck appData perms. Verify perms version hasn't changed.
		aDataRepeat := &AppData{}
		if err := util.GetWithAuth(ctx, call, a.s.st, a.stKey(), aDataRepeat); err != nil {
			return err
		}
		if aData.Version != aDataRepeat.Version {
			return verror.NewErrBadVersion(ctx)
		}
		// Check for "database already exists".
		if _, err := a.getDbInfo(ctx, tx, dbName); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			// TODO(sadovsky): Should this be ErrExistOrNoAccess, for privacy?
			return verror.New(verror.ErrExist, ctx, dbName)
		}
		// Write dbInfo into active databases.
		if err := a.putDbInfo(ctx, tx, dbName, dbInfo); err != nil {
			return err
		}
		// Delete dbInfo from GC log.
		return delDbGCEntry(ctx, tx, dbInfo)
	}); err != nil {
		return err
	}

	a.dbs[dbName] = d
	return nil
}

func (a *app) DestroyNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) error {
	if !a.exists {
		vlog.Fatalf("app %q does not exist", a.name)
	}
	// Steps:
	// 1. Check databaseData perms.
	// 2. Move dbInfo from active databases into GC log. <===== CHANGE BECOMES VISIBLE
	// 3. Best effort database destroy. If it fails, it will be retried on
	//    syncbased restart.
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.dbs[dbName]
	if !ok {
		return nil // destroy is idempotent
	}

	// 1. Check databaseData perms.
	if err := d.CheckPermsInternal(ctx, call, d.St()); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			return nil // destroy is idempotent
		}
		return err
	}

	// 2. Move dbInfo from active databases into GC log.
	var dbInfo *DbInfo
	if err := store.RunInTransaction(a.s.st, func(tx store.Transaction) (err error) {
		dbInfo, err = deleteDatabaseEntry(ctx, tx, a, dbName)
		return
	}); err != nil {
		return err
	}
	delete(a.dbs, dbName)

	// 3. Best effort database destroy. If it fails, it will be retried on
	//    syncbased restart.
	// TODO(ivanpi): Consider returning an error on failure here, even though
	// database was made inaccessible. Note, if Close() failed, ongoing RPCs
	// might still be using the store.
	// TODO(ivanpi): Consider running asynchronously. However, see TODO in
	// finalizeDatabaseDestroy.
	if err := finalizeDatabaseDestroy(ctx, a.s.st, dbInfo, d.St()); err != nil {
		vlog.Error(err)
	}

	return nil
}

func (a *app) SetDatabasePerms(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, version string) error {
	if !a.exists {
		vlog.Fatalf("app %q does not exist", a.name)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.dbs[dbName]
	if !ok {
		return verror.New(verror.ErrNoExist, ctx, dbName)
	}
	return d.SetPermsInternal(ctx, call, perms, version)
}

func (a *app) Name() string {
	return a.name
}

////////////////////////////////////////
// Internal helpers

func (a *app) stKey() string {
	return common.JoinKeyParts(common.AppPrefix, a.stKeyPart())
}

func (a *app) stKeyPart() string {
	return a.name
}

func (a *app) rootDirForDb(dbName string) (string, error) {
	// Note: Common Linux filesystems such as ext4 allow almost any character to
	// appear in a filename, but other filesystems are more restrictive. For
	// example, by default the OS X filesystem uses case-insensitive filenames.
	// To play it safe, we hex-encode app and database names, yielding filenames
	// that match "^[0-9a-f]+$". To allow recreating databases independently of
	// garbage collecting old destroyed versions, a random suffix is appended to
	// the database name.
	appHex := hex.EncodeToString([]byte(a.name))
	dbHex := hex.EncodeToString([]byte(dbName))
	var suffix [32]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("failed to generate suffix: %v", err)
	}
	suffixHex := hex.EncodeToString(suffix[:])
	dbHexWithSuffix := dbHex + "-" + suffixHex
	// ValidAppName and ValidDatabaseName require len([]byte(name)) <= 64, so even
	// after appending the random suffix and with the 2x blowup from hex-encoding,
	// the lengths of these names should be well below the filesystem limit of 255
	// bytes.
	// TODO(sadovsky): Currently, our client-side app/db creation tests verify
	// that the server does not crash when too-long names are specified; we rely
	// on the server-side dispatcher logic to return errors for too-long names.
	// However, we ought to add server-side tests for this behavior, so that we
	// don't accidentally remove the server-side name validation after adding
	// client-side name validation.
	if len(appHex) > 255 || len(dbHexWithSuffix) > 255 {
		vlog.Fatalf("appHex %s or dbHexWithSuffix %s is too long", appHex, dbHexWithSuffix)
	}
	return path.Join(a.s.opts.RootDir, common.AppDir, appHex, common.DbDir, dbHexWithSuffix), nil
}
