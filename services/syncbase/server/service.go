// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

// TODO(sadovsky): Check Resolve access on parent where applicable. Relatedly,
// convert ErrNoExist and ErrNoAccess to ErrNoExistOrNoAccess where needed to
// preserve privacy.

import (
	"path"
	"sync"

	wire "v.io/syncbase/v23/services/syncbase"
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/vsync"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

// service is a singleton (i.e. not per-request) that handles Service RPCs.
type service struct {
	st   store.Store // keeps track of which apps and databases exist, etc.
	sync interfaces.SyncServerMethods
	opts ServiceOptions
	// Guards the fields below. Held during app Create, Delete, and
	// SetPermissions.
	mu   sync.Mutex
	apps map[string]*app
}

var (
	_ wire.ServiceServerMethods = (*service)(nil)
	_ interfaces.Service        = (*service)(nil)
	_ util.Layer                = (*service)(nil)
)

// ServiceOptions configures a service.
type ServiceOptions struct {
	// Service-level permissions.
	Perms access.Permissions
	// Root dir for data storage.
	RootDir string
	// Storage engine to use (for service and per-database engines).
	Engine string
}

// NewService creates a new service instance and returns it.
// Returns a VDL-compatible error.
func NewService(ctx *context.T, call rpc.ServerCall, opts ServiceOptions) (*service, error) {
	if opts.Perms == nil {
		return nil, verror.New(verror.ErrInternal, ctx, "perms must be specified")
	}
	st, err := util.OpenStore(opts.Engine, path.Join(opts.RootDir, opts.Engine))
	if err != nil {
		return nil, err
	}
	s := &service{
		st:   st,
		opts: opts,
		apps: map[string]*app{},
	}
	data := &serviceData{
		Perms: opts.Perms,
	}
	if err := util.Put(ctx, s.st, s, data); err != nil {
		return nil, err
	}
	if s.sync, err = vsync.New(ctx, call, s); err != nil {
		return nil, err
	}
	return s, nil
}

////////////////////////////////////////
// RPC methods

func (s *service) SetPermissions(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	return store.RunInTransaction(s.st, func(st store.StoreReadWriter) error {
		data := &serviceData{}
		return util.Update(ctx, call, st, s, data, func() error {
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
	data := &serviceData{}
	if err := util.Get(ctx, call, s.st, s, data); err != nil {
		return nil, "", err
	}
	return data.Perms, util.FormatVersion(data.Version), nil
}

func (s *service) GlobChildren__(ctx *context.T, call rpc.ServerCall) (<-chan string, error) {
	// Check perms.
	sn := s.st.NewSnapshot()
	closeSnapshot := func() error {
		return sn.Close()
	}
	if err := util.Get(ctx, call, sn, s, &serviceData{}); err != nil {
		closeSnapshot()
		return nil, err
	}
	return util.Glob(ctx, call, "*", sn, closeSnapshot, util.AppPrefix)
}

////////////////////////////////////////
// interfaces.Service methods

func (s *service) St() store.Store {
	return s.st
}

func (s *service) Sync() interfaces.SyncServerMethods {
	return s.sync
}

// TODO(sadovsky): Currently, we return an error (here and elsewhere) if the
// specified app does not exist in s.apps. But note, this will always be the
// case after a Syncbase service restart. The implementation should be updated
// so that s.apps acts as an in-memory cache of app handles. If an app exists
// but is not present in s.apps, s.App() should open the app and add its handle
// to s.apps.
func (s *service) App(ctx *context.T, call rpc.ServerCall, appName string) (interfaces.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
		dbs:    map[string]interfaces.Database{},
	}

	if err := store.RunInTransaction(s.st, func(st store.StoreReadWriter) error {
		// Check serviceData perms.
		sData := &serviceData{}
		if err := util.Get(ctx, call, st, s, sData); err != nil {
			return err
		}
		// Check for "app already exists".
		if err := util.GetWithoutAuth(ctx, st, a, &appData{}); verror.ErrorID(err) != verror.ErrNoExist.ID {
			if err != nil {
				return err
			}
			return verror.New(verror.ErrExist, ctx, appName)
		}
		// Write new appData.
		if perms == nil {
			perms = sData.Perms
		}
		data := &appData{
			Name:  appName,
			Perms: perms,
		}
		return util.Put(ctx, st, a, data)
	}); err != nil {
		return err
	}

	s.apps[appName] = a
	return nil
}

func (s *service) deleteApp(ctx *context.T, call rpc.ServerCall, appName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[appName]
	if !ok {
		return nil // delete is idempotent
	}

	if err := store.RunInTransaction(s.st, func(st store.StoreReadWriter) error {
		// Read-check-delete appData.
		if err := util.Get(ctx, call, st, a, &appData{}); err != nil {
			if verror.ErrorID(err) == verror.ErrNoExist.ID {
				return nil // delete is idempotent
			}
			return err
		}
		// TODO(sadovsky): Delete all databases in this app.
		return util.Delete(ctx, st, a)
	}); err != nil {
		return err
	}

	delete(s.apps, appName)
	return nil
}

func (s *service) setAppPerms(ctx *context.T, call rpc.ServerCall, appName string, perms access.Permissions, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[appName]
	if !ok {
		return verror.New(verror.ErrNoExist, ctx, appName)
	}
	return store.RunInTransaction(s.st, func(st store.StoreReadWriter) error {
		data := &appData{}
		return util.Update(ctx, call, st, a, data, func() error {
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
// util.Layer methods

func (s *service) Name() string {
	return "service"
}

func (s *service) StKey() string {
	return util.ServicePrefix
}
