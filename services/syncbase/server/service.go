// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

// TODO(sadovsky): Check Resolve access on parent where applicable. Relatedly,
// convert ErrNoExist and ErrNoAccess to ErrNoExistOrNoAccess where needed to
// preserve privacy.

import (
	"bytes"
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
	st      store.Store // keeps track of which apps and databases exist, etc.
	sync    interfaces.SyncServerMethods
	vclock  *vclock.VClock
	vclockD *vclock.VClockD
	opts    ServiceOptions
	// Guards the fields below. Held during app Create, Delete, and
	// SetPermissions.
	mu   sync.Mutex
	apps map[string]*app
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
			vlog.Errorf("top-level store is corrupt, moving all apps aside")
			appDir := filepath.Join(opts.RootDir, common.AppDir)
			newPath := appDir + ".corrupt." + time.Now().Format(time.RFC3339)
			if err := os.Rename(appDir, newPath); err != nil {
				return nil, verror.New(verror.ErrInternal, ctx, "could not move apps aside: "+err.Error())
			}
		}
		return nil, err
	}
	s := &service{
		st:     st,
		vclock: vclock.NewVClock(st),
		opts:   opts,
		apps:   map[string]*app{},
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
		// Initialize in-memory data structures.
		// Read all apps, populate apps map.
		aIt := st.Scan(common.ScanPrefixArgs(common.AppPrefix, ""))
		aBytes := []byte{}
		for aIt.Advance() {
			aBytes = aIt.Value(aBytes)
			aData := &AppData{}
			if err := vom.Decode(aBytes, aData); err != nil {
				return nil, verror.New(verror.ErrInternal, ctx, err)
			}
			a := &app{
				name:   aData.Name,
				s:      s,
				exists: true,
				dbs:    make(map[string]interfaces.Database),
			}
			if err := openDatabases(ctx, st, a); err != nil {
				return nil, verror.New(verror.ErrInternal, ctx, err)
			}
			s.apps[a.name] = a
		}
		if err := aIt.Err(); err != nil {
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

func openDatabases(ctx *context.T, st store.Store, a *app) error {
	// Read all dbs for this app, populate dbs map.
	dIt := st.Scan(common.ScanPrefixArgs(common.JoinKeyParts(common.DbInfoPrefix, a.name), ""))
	dBytes := []byte{}
	for dIt.Advance() {
		dBytes = dIt.Value(dBytes)
		info := &DbInfo{}
		if err := vom.Decode(dBytes, info); err != nil {
			return verror.New(verror.ErrInternal, ctx, err)
		}
		d, err := OpenDatabase(ctx, a, info.Name, DatabaseOptions{
			RootDir: info.RootDir,
			Engine:  info.Engine,
		}, storeutil.OpenOptions{
			CreateIfMissing: false,
			ErrorIfExists:   false,
		})
		if err != nil {
			// If the database is corrupt, OpenDatabase will have moved it aside. We
			// need to delete the app's reference to the database so that the client
			// application can recreate the database the next time it starts.
			if verror.ErrorID(err) == wire.ErrCorruptDatabase.ID {
				vlog.Errorf("app %s, database %s is corrupt, deleting the app's reference to it",
					a.name, info.Name)
				if err2 := a.delDbInfo(ctx, a.s.st, info.Name); err2 != nil {
					vlog.Errorf("failed to delete app %s reference to corrupt database %s: %v",
						a.name, info.Name, err2)
					// Return the ErrCorruptDatabase, not err2
				}
				return err
			}
			return verror.New(verror.ErrInternal, ctx, err)
		}
		a.dbs[info.Name] = d
	}
	if err := dIt.Err(); err != nil {
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
	return util.GlobChildren(ctx, call, matcher, sn, common.AppPrefix)
}

////////////////////////////////////////
// interfaces.Service methods

func (s *service) St() store.Store {
	return s.st
}

func (s *service) Sync() interfaces.SyncServerMethods {
	return s.sync
}

func (s *service) Clock() common.Clock {
	return s.vclock
}

func (s *service) App(ctx *context.T, call rpc.ServerCall, appName string) (interfaces.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Note, currently the service's 'apps' map as well as the per-app 'dbs' maps
	// are populated at startup.
	a, ok := s.apps[appName]
	if !ok {
		return nil, verror.New(verror.ErrNoExist, ctx, appName)
	}
	return a, nil
}

func (s *service) AppNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	// In the future this API will likely be replaced by one that streams the app
	// names.
	s.mu.Lock()
	defer s.mu.Unlock()
	appNames := make([]string, 0, len(s.apps))
	for n := range s.apps {
		appNames = append(appNames, n)
	}
	return appNames, nil
}

////////////////////////////////////////
// App management methods

func (s *service) createApp(ctx *context.T, call rpc.ServerCall, appName string, perms access.Permissions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.apps[appName]; ok {
		return verror.New(verror.ErrExist, ctx, appName)
	}

	a := &app{
		name:   appName,
		s:      s,
		exists: true,
		dbs:    make(map[string]interfaces.Database),
	}

	if err := store.RunInTransaction(s.st, func(tx store.Transaction) error {
		// Check serviceData perms.
		sData := &ServiceData{}
		if err := util.GetWithAuth(ctx, call, tx, s.stKey(), sData); err != nil {
			return err
		}
		// Check for "app already exists".
		if err := store.Get(ctx, tx, a.stKey(), &AppData{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			return verror.New(verror.ErrExist, ctx, appName)
		}
		// Write new appData.
		if perms == nil {
			perms = sData.Perms
		}
		data := &AppData{
			Name:  appName,
			Perms: perms,
		}
		return store.Put(ctx, tx, a.stKey(), data)
	}); err != nil {
		return err
	}

	s.apps[appName] = a
	return nil
}

func (s *service) destroyApp(ctx *context.T, call rpc.ServerCall, appName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// TODO(ivanpi): Destroying an app is in a possible race with creating a
	// database in that app. Consider locking app mutex here, possibly other
	// nested mutexes as well, and cancelling calls on nested objects. Same for
	// database and collection destroy.
	a, ok := s.apps[appName]
	if !ok {
		return nil // destroy is idempotent
	}

	type dbTombstone struct {
		store  store.Store
		dbInfo *DbInfo
	}
	var tombstones []dbTombstone
	if err := store.RunInTransaction(s.st, func(tx store.Transaction) error {
		// Check appData perms.
		if err := util.GetWithAuth(ctx, call, tx, a.stKey(), &AppData{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // destroy is idempotent
			}
			return err
		}
		// Mark all databases in this app for destruction.
		for _, db := range a.dbs {
			dbInfo, err := deleteDatabaseEntry(ctx, tx, a, db.Name())
			if err != nil {
				return err
			}
			tombstones = append(tombstones, dbTombstone{
				store:  db.St(),
				dbInfo: dbInfo,
			})
		}
		// Delete appData.
		return store.Delete(ctx, tx, a.stKey())
	}); err != nil {
		return err
	}
	delete(s.apps, appName)

	// Best effort destroy for all databases in this app. If any destroy fails,
	// it will be attempted again on syncbased restart.
	// TODO(ivanpi): Consider running asynchronously. However, see TODO in
	// finalizeDatabaseDestroy.
	for _, ts := range tombstones {
		if err := finalizeDatabaseDestroy(ctx, s.st, ts.dbInfo, ts.store); err != nil {
			vlog.Error(err)
		}
	}

	return nil
}

func (s *service) setAppPerms(ctx *context.T, call rpc.ServerCall, appName string, perms access.Permissions, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[appName]
	if !ok {
		return verror.New(verror.ErrNoExist, ctx, appName)
	}
	return store.RunInTransaction(s.st, func(tx store.Transaction) error {
		data := &AppData{}
		return util.UpdateWithAuth(ctx, call, tx, a.stKey(), data, func() error {
			if err := util.CheckVersion(ctx, version, data.Version); err != nil {
				return err
			}
			data.Perms = perms
			data.Version++
			return nil
		})
	})
}

////////////////////////////////////////
// Other internal helpers

func (s *service) stKey() string {
	return common.ServicePrefix
}
