// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Utilities for testing sync.

import (
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/syncbase/x/ref/services/syncbase/store/memstore"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

// mockService emulates a Syncbase service that includes store and sync.
// It is used to access a mock application.
type mockService struct {
	st   store.Store
	sync *syncService
}

func (s *mockService) St() store.Store {
	return s.st
}

func (s *mockService) App(ctx *context.T, call rpc.ServerCall, appName string) (interfaces.App, error) {
	return &mockApp{st: s.st}, nil
}

func (s *mockService) AppNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	return []string{"mockapp"}, nil
}

// mockApp emulates a Syncbase App.  It is used to access a mock database.
type mockApp struct {
	st store.Store
}

func (a *mockApp) NoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) (interfaces.Database, error) {
	return &mockDatabase{st: a.st}, nil
}

func (a *mockApp) NoSQLDatabaseNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	return []string{"mockdb"}, nil
}

func (a *mockApp) CreateNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions) error {
	return verror.NewErrNotImplemented(ctx)
}

func (a *mockApp) DeleteNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (a *mockApp) SetDatabasePerms(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, version string) error {
	return verror.NewErrNotImplemented(ctx)
}

// mockDatabase emulates a Syncbase Database.  It is used to test sync functionality.
type mockDatabase struct {
	st store.Store
}

func (d *mockDatabase) St() store.Store {
	return d.st
}

func (d *mockDatabase) CheckPermsInternal(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *mockDatabase) SetPermsInternal(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	return verror.NewErrNotImplemented(ctx)
}

// createService creates a mock Syncbase service used for testing sync functionality.
// At present it relies on the underlying Memstore engine.
func createService(t *testing.T) *mockService {
	var err error
	s := &mockService{
		st: memstore.New(),
	}
	if s.sync, err = New(nil, nil, s); err != nil {
		t.Fatalf("cannot create sync service: %v", err)
	}
	return s
}
