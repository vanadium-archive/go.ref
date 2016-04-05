// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

// TODO(sadovsky): Check Resolve access on parent where applicable. Relatedly,
// convert ErrNoExist and ErrNoAccess to ErrNoExistOrNoAccess where needed to
// preserve privacy.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
	storeutil "v.io/x/ref/services/syncbase/store/util"
	"v.io/x/ref/services/syncbase/vclock"
	"v.io/x/ref/services/syncbase/vsync"
)

// service is a singleton (i.e. not per-request) that handles Service RPCs.
type service struct {
	st      store.Store // keeps track of which databases exist, etc.
	sync    interfaces.SyncServerMethods
	vclock  *vclock.VClock
	vclockD *vclock.VClockD
	opts    ServiceOptions
	// Guards the fields below. Held during database Create, Delete, and
	// SetPermissions.
	mu  sync.Mutex
	dbs map[wire.Id]*database
}

var (
	_ wire.ServiceServerMethods = (*service)(nil)
	_ interfaces.Service        = (*service)(nil)
)

// ServiceOptions configures a service.
type ServiceOptions struct {
	// Service-level permissions. Used only when creating a brand-new storage
	// instance.
	Perms access.Permissions
	// Root dir for data storage. If empty, we write to a fresh directory created
	// using ioutil.TempDir.
	RootDir string
	// Storage engine to use: memstore or leveldb. If empty, we use the default
	// storage engine, currently leveldb.
	Engine string
	// Whether to skip publishing in the neighborhood.
	SkipPublishInNh bool
	// Whether to run in development mode; required for RPCs such as
	// Service.DevModeUpdateVClock.
	DevMode bool
}

// defaultPerms returns a permissions object that grants all permissions to the
// provided blessing patterns.
func defaultPerms(blessingPatterns []security.BlessingPattern) access.Permissions {
	perms := access.Permissions{}
	for _, tag := range access.AllTypicalTags() {
		for _, bp := range blessingPatterns {
			perms.Add(bp, string(tag))
		}
	}
	return perms
}

// PermsString returns a JSON-based string representation of the permissions.
func PermsString(perms access.Permissions) string {
	var buf bytes.Buffer
	if err := access.WritePermissions(&buf, perms); err != nil {
		vlog.Errorf("Failed to serialize permissions %+v: %v", perms, err)
		return fmt.Sprintf("[unserializable] %+v", perms)
	}
	return buf.String()
}

// NewService creates a new service instance and returns it.
// TODO(sadovsky): If possible, close all stores when the server is stopped.
func NewService(ctx *context.T, opts ServiceOptions) (*service, error) {
	// Fill in default values for missing options.
	if opts.Engine == "" {
		opts.Engine = "leveldb"
	}
	if opts.RootDir == "" {
		var err error
		if opts.RootDir, err = ioutil.TempDir("", "syncbased-"); err != nil {
			return nil, err
		}
		vlog.Infof("Created new root dir: %s", opts.RootDir)
	}

	st, err := storeutil.OpenStore(opts.Engine, filepath.Join(opts.RootDir, opts.Engine), storeutil.OpenOptions{CreateIfMissing: true, ErrorIfExists: false})
	if err != nil {
		// If the top-level store is corrupt, we lose the meaning of all of the
		// app-level databases. util.OpenStore moved the top-level store aside, but
		// it didn't do anything about the app-level stores.
		if verror.ErrorID(err) == wire.ErrCorruptDatabase.ID {
			vlog.Errorf("top-level store is corrupt, moving all databases aside")
			appDir := filepath.Join(opts.RootDir, common.AppDir)
			newPath := appDir + ".corrupt." + time.Now().Format(time.RFC3339)
			if err := os.Rename(appDir, newPath); err != nil {
				return nil, verror.New(verror.ErrInternal, ctx, "could not move databases aside: "+err.Error())
			}
		}
		return nil, err
	}
	s := &service{
		st:     st,
		vclock: vclock.NewVClock(st),
		opts:   opts,
		dbs:    map[wire.Id]*database{},
	}

	// Make sure the vclock is initialized before opening any databases (which
	// pass vclock to watchable.Wrap) and before starting sync or vclock daemon
	// goroutines.
	if err := s.vclock.InitVClockData(); err != nil {
		return nil, err
	}

	var sd ServiceData
	if err := store.Get(ctx, st, s.stKey(), &sd); verror.ErrorID(err) != verror.ErrNoExist.ID {
		if err != nil {
			return nil, err
		}
		readPerms := sd.Perms.Normalize()
		if opts.Perms != nil {
			if givenPerms := opts.Perms.Copy().Normalize(); !reflect.DeepEqual(givenPerms, readPerms) {
				vlog.Infof("Warning: configured permissions will be ignored: %v", PermsString(givenPerms))
			}
		}
		vlog.Infof("Using persisted permissions: %v", PermsString(readPerms))
		// Service exists.
		// Run garbage collection of inactive databases.
		// TODO(ivanpi): This is currently unsafe to call concurrently with
		// database creation/deletion. Add mutex and run asynchronously.
		if err := runGCInactiveDatabases(ctx, st); err != nil {
			return nil, err
		}
		// Initialize in-memory data structures, namely the dbs map.
		if err := s.openDatabases(ctx); err != nil {
			return nil, verror.New(verror.ErrInternal, ctx, err)
		}
	} else {
		perms := opts.Perms
		// Service does not exist.
		if perms == nil {
			vlog.Info("Permissions flag not set. Giving local principal all permissions.")
			perms = defaultPerms(security.DefaultBlessingPatterns(v23.GetPrincipal(ctx)))
		}
		vlog.Infof("Using permissions: %v", PermsString(perms))
		data := &ServiceData{
			Perms: perms,
		}
		if err := store.Put(ctx, st, s.stKey(), data); err != nil {
			return nil, err
		}
	}

	// Note, vsync.New internally handles both first-time and subsequent
	// invocations.
	if s.sync, err = vsync.New(ctx, s, opts.Engine, opts.RootDir, s.vclock, !opts.SkipPublishInNh); err != nil {
		return nil, err
	}

	// Start the vclock daemon. For now, we disable NTP when running in dev mode.
	// If we decide to change this behavior, we'll need to update the tests in
	// v.io/v23/syncbase/featuretests/vclock_v23_test.go to disable NTP some other
	// way.
	ntpHost := ""
	if !s.opts.DevMode {
		ntpHost = common.NtpDefaultHost
	}
	s.vclockD = vclock.NewVClockD(s.vclock, ntpHost)
	s.vclockD.Start()
	return s, nil
}

func (s *service) openDatabases(ctx *context.T) error {
	dbIt := s.st.Scan(common.ScanPrefixArgs(common.DbInfoPrefix, ""))
	v := []byte{}
	for dbIt.Advance() {
		v = dbIt.Value(v)
		info := &DbInfo{}
		if err := vom.Decode(v, info); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		d, err := openDatabase(ctx, s, info.Id, DatabaseOptions{
			RootDir: info.RootDir,
			Engine:  info.Engine,
		}, storeutil.OpenOptions{
			CreateIfMissing: false,
			ErrorIfExists:   false,
		})
		if err != nil {
			// If the database is corrupt, openDatabase will have moved it aside. We
			// need to delete the app's reference to the database so that the client
			// application can recreate the database the next time it starts.
			if verror.ErrorID(err) == wire.ErrCorruptDatabase.ID {
				vlog.Errorf("database %v is corrupt, deleting the reference to it", info.Id)
				if err2 := delDbInfo(ctx, s.st, info.Id); err2 != nil {
					vlog.Errorf("failed to delete reference to corrupt database %v: %v", info.Id, err2)
					// Return the ErrCorruptDatabase, not err2.
				}
				return err
			}
			return verror.New(verror.ErrInternal, ctx, err)
		}
		s.dbs[info.Id] = d
	}
	if err := dbIt.Err(); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// AddNames adds all the names for this Syncbase instance, gathered from all the
// syncgroups it is currently participating in. This method is exported so that
// when syncbased is launched, it can publish these names.
//
// Note: This method is exported here and syncbased in main.go calls it since
// publishing needs the server handle which is available during init in
// syncbased. Alternately, one can step through server creation in main.go and
// create the server handle first and pass it to NewService so that sync can use
// that handle to publish the names it needs. Server creation can then proceed
// with hooking up the dispatcher, etc. In the current approach, we favor not
// breaking up the server init into pieces but using the available wrapper, and
// adding the names when ready. Finally, we could have also exported a SetServer
// method instead of the AddNames at the service layer. However, that approach
// will also need further synchronization between when the service starts
// accepting incoming RPC requests and when the restart is complete. We can
// control when the server starts accepting incoming requests by using a fake
// dispatcher until we are ready and then switching to the real one after
// restart. However, we will still need synchronization between Add/RemoveName.
// So, we decided to add synchronization from the get-go and avoid the fake
// dispatcher.
func (s *service) AddNames(ctx *context.T, svr rpc.Server) error {
	return vsync.AddNames(ctx, s.sync, svr)
}

// Close shuts down this Syncbase instance.
//
// TODO(hpucha): Close or cleanup Syncbase app/db data structures.
func (s *service) Close() {
	s.vclockD.Close()
	vsync.Close(s.sync)
}

////////////////////////////////////////
// RPC methods

// TODO(sadovsky): Add test to demonstrate that these don't work unless Syncbase
// was started in dev mode.
func (s *service) DevModeUpdateVClock(ctx *context.T, call rpc.ServerCall, opts wire.DevModeUpdateVClockOpts) error {
	if !s.opts.DevMode {
		return wire.NewErrNotInDevMode(ctx)
	}
	// Check perms.
	if err := util.GetWithAuth(ctx, call, s.st, s.stKey(), &ServiceData{}); err != nil {
		return err
	}
	if opts.NtpHost != "" {
		s.vclockD.InjectNtpHost(opts.NtpHost)
	}
	if !opts.Now.Equal(time.Time{}) {
		s.vclock.InjectFakeSysClock(opts.Now, opts.ElapsedTime)
	}
	if opts.DoNtpUpdate {
		if err := s.vclockD.DoNtpUpdate(); err != nil {
			return err
		}
	}
	if opts.DoLocalUpdate {
		if err := s.vclockD.DoLocalUpdate(); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) DevModeGetTime(ctx *context.T, call rpc.ServerCall) (time.Time, error) {
	if !s.opts.DevMode {
		return time.Time{}, wire.NewErrNotInDevMode(ctx)
	}
	// Check perms.
	if err := util.GetWithAuth(ctx, call, s.st, s.stKey(), &ServiceData{}); err != nil {
		return time.Time{}, err
	}
	return s.vclock.Now()
}

func (s *service) SetPermissions(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	return store.RunInTransaction(s.st, func(tx store.Transaction) error {
		data := &ServiceData{}
		return util.UpdateWithAuth(ctx, call, tx, s.stKey(), data, func() error {
			if err := util.CheckVersion(ctx, version, data.Version); err != nil {
				return err
			}
			data.Perms = perms
			data.Version++
			return nil
		})
	})
}

func (s *service) GetPermissions(ctx *context.T, call rpc.ServerCall) (perms access.Permissions, version string, err error) {
	data := &ServiceData{}
	if err := util.GetWithAuth(ctx, call, s.st, s.stKey(), data); err != nil {
		return nil, "", err
	}
	return data.Perms, util.FormatVersion(data.Version), nil
}

func (s *service) GlobChildren__(ctx *context.T, call rpc.GlobChildrenServerCall, matcher *glob.Element) error {
	// Check perms.
	sn := s.st.NewSnapshot()
	defer sn.Abort()
	if err := util.GetWithAuth(ctx, call, sn, s.stKey(), &ServiceData{}); err != nil {
		return err
	}
	return util.GlobChildren(ctx, call, matcher, sn, common.DbInfoPrefix)
}

////////////////////////////////////////
// interfaces.Service methods

func (s *service) St() store.Store {
	return s.st
}

func (s *service) Sync() interfaces.SyncServerMethods {
	return s.sync
}

func (s *service) Database(ctx *context.T, call rpc.ServerCall, dbId wire.Id) (interfaces.Database, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.dbs[dbId]
	if !ok {
		return nil, verror.New(verror.ErrNoExist, ctx, dbId)
	}
	return d, nil
}

func (s *service) DatabaseIds(ctx *context.T, call rpc.ServerCall) ([]wire.Id, error) {
	// Note: In the future this API will likely be replaced by one that streams
	// the database ids.
	s.mu.Lock()
	defer s.mu.Unlock()
	dbIds := make([]wire.Id, 0, len(s.dbs))
	for id := range s.dbs {
		dbIds = append(dbIds, id)
	}
	return dbIds, nil
}

////////////////////////////////////////
// Database management methods

func (s *service) createDatabase(ctx *context.T, call rpc.ServerCall, dbId wire.Id, perms access.Permissions, metadata *wire.SchemaMetadata) (reterr error) {
	// Steps:
	// 1. Check serviceData perms.
	// 2. Put dbInfo record into garbage collection log, to clean up database if
	//    remaining steps fail or syncbased crashes.
	// 3. Initialize database.
	// 4. Move dbInfo from GC log into active dbs. <===== CHANGE BECOMES VISIBLE
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dbs[dbId]; ok {
		// TODO(sadovsky): Should this be ErrExistOrNoAccess, for privacy?
		return verror.New(verror.ErrExist, ctx, dbId)
	}

	// 1. Check serviceData perms.
	sData := &ServiceData{}
	if err := util.GetWithAuth(ctx, call, s.st, s.stKey(), sData); err != nil {
		return err
	}

	// 2. Put dbInfo record into garbage collection log, to clean up database if
	//    remaining steps fail or syncbased crashes.
	rootDir, err := s.rootDirForDb(dbId)
	if err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	dbInfo := &DbInfo{
		Id:      dbId,
		RootDir: rootDir,
		Engine:  s.opts.Engine,
	}
	if err := putDbGCEntry(ctx, s.st, dbInfo); err != nil {
		return err
	}
	var stRef store.Store
	defer func() {
		if reterr != nil {
			// Best effort database destroy on error. If it fails, it will be retried
			// on syncbased restart. (It is safe to pass nil stRef if step 3 fails.)
			// TODO(ivanpi): Consider running asynchronously. However, see TODO in
			// finalizeDatabaseDestroy.
			if err := finalizeDatabaseDestroy(ctx, s.st, dbInfo, stRef); err != nil {
				vlog.Error(err)
			}
		}
	}()

	// 3. Initialize database.
	// TODO(sadovsky): Revisit default perms.
	if perms == nil {
		perms = sData.Perms
	}
	// TODO(ivanpi): newDatabase doesn't close the store on failure.
	d, err := newDatabase(ctx, s, dbId, metadata, DatabaseOptions{
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

	// 4. Move dbInfo from GC log into active dbs.
	if err := store.RunInTransaction(s.st, func(tx store.Transaction) error {
		// Note: To avoid a race, we must re-check service perms, and make sure the
		// perms version hasn't changed, inside the transaction that makes the new
		// database visible.
		sDataRepeat := &ServiceData{}
		if err := util.GetWithAuth(ctx, call, s.st, s.stKey(), sDataRepeat); err != nil {
			return err
		}
		if sData.Version != sDataRepeat.Version {
			return verror.NewErrBadVersion(ctx)
		}
		// Check for "database already exists".
		if _, err := getDbInfo(ctx, tx, dbId); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			// TODO(sadovsky): Should this be ErrExistOrNoAccess, for privacy?
			return verror.New(verror.ErrExist, ctx, dbId)
		}
		// Write dbInfo into active databases.
		if err := putDbInfo(ctx, tx, dbInfo); err != nil {
			return err
		}
		// Delete dbInfo from GC log.
		return delDbGCEntry(ctx, tx, dbInfo)
	}); err != nil {
		return err
	}

	s.dbs[dbId] = d
	return nil
}

func (s *service) destroyDatabase(ctx *context.T, call rpc.ServerCall, dbId wire.Id) error {
	// Steps:
	// 1. Check databaseData perms.
	// 2. Move dbInfo from active dbs into GC log. <===== CHANGE BECOMES VISIBLE
	// 3. Best effort database destroy. If it fails, it will be retried on
	//    syncbased restart.
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.dbs[dbId]
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

	// 2. Move dbInfo from active dbs into GC log.
	var dbInfo *DbInfo
	if err := store.RunInTransaction(s.st, func(tx store.Transaction) (err error) {
		dbInfo, err = deleteDatabaseEntry(ctx, tx, dbId)
		return
	}); err != nil {
		return err
	}
	delete(s.dbs, dbId)

	// 3. Best effort database destroy. If it fails, it will be retried on
	//    syncbased restart.
	// TODO(ivanpi): Consider returning an error on failure here, even though
	// database was made inaccessible. Note, if Close() failed, ongoing RPCs
	// might still be using the store.
	// TODO(ivanpi): Consider running asynchronously. However, see TODO in
	// finalizeDatabaseDestroy.
	if err := finalizeDatabaseDestroy(ctx, s.st, dbInfo, d.St()); err != nil {
		vlog.Error(err)
	}

	return nil
}

func (s *service) setDatabasePerms(ctx *context.T, call rpc.ServerCall, dbId wire.Id, perms access.Permissions, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.dbs[dbId]
	if !ok {
		return verror.New(verror.ErrNoExist, ctx, dbId)
	}
	return d.setPermsInternal(ctx, call, perms, version)
}

////////////////////////////////////////
// Other internal helpers

func (s *service) stKey() string {
	return common.ServicePrefix
}

func (s *service) rootDirForDb(dbId wire.Id) (string, error) {
	// Note: Common Linux filesystems such as ext4 allow almost any character to
	// appear in a filename, but other filesystems are more restrictive. For
	// example, by default the OS X filesystem uses case-insensitive filenames.
	// To play it safe, we hex-encode app and database names, yielding filenames
	// that match "^[0-9a-f]+$". To allow recreating databases independently of
	// garbage collecting old destroyed versions, a random suffix is appended to
	// the database name.
	blessingHex := hex.EncodeToString([]byte(dbId.Blessing))
	nameHex := hex.EncodeToString([]byte(dbId.Name))
	var suffix [32]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("failed to generate suffix: %v", err)
	}
	suffixHex := hex.EncodeToString(suffix[:])
	nameHexWithSuffix := nameHex + "-" + suffixHex
	// ValidDatabaseId requires len([]byte(id.Blessing)) <= 64 and
	// len([]byte(id.Name)) <= 64, so even after appending the random suffix and
	// with the 2x blowup from hex-encoding, the lengths of these names should be
	// well below the filesystem limit of 255 bytes.
	// TODO(sadovsky): Currently, our client-side database creation tests verify
	// that the server does not crash when too-long names are specified; we rely
	// on the server-side dispatcher logic to return errors for too-long names.
	// However, we ought to add server-side tests for this behavior, so that we
	// don't accidentally remove the server-side name validation after adding
	// client-side name validation.
	if len(blessingHex) > 255 || len(nameHexWithSuffix) > 255 {
		vlog.Fatalf("blessingHex %s or nameHexWithSuffix %s is too long", blessingHex, nameHexWithSuffix)
	}
	return filepath.Join(s.opts.RootDir, common.AppDir, blessingHex, common.DbDir, nameHexWithSuffix), nil
}
