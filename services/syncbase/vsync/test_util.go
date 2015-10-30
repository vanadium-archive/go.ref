// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Utilities for testing sync.

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase/nosql"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/clock"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/test"
)

var (
	mockCRStream *conflictResolverStream
)

// mockService emulates a Syncbase service that includes store and sync.
// It is used to access a mock application.
type mockService struct {
	engine   string
	dir      string
	st       store.Store
	sync     *syncService
	shutdown func()
}

func (s *mockService) St() store.Store {
	return s.st
}

func (s *mockService) Sync() interfaces.SyncServerMethods {
	return s.sync
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

func (a *mockApp) CreateNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, metadata *wire.SchemaMetadata) error {
	return verror.NewErrNotImplemented(ctx)
}

func (a *mockApp) DestroyNoSQLDatabase(ctx *context.T, call rpc.ServerCall, dbName string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (a *mockApp) SetDatabasePerms(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, version string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (a *mockApp) Service() interfaces.Service {
	return nil
}

func (a *mockApp) Name() string {
	return "mockapp"
}

// mockDatabase emulates a Syncbase Database.  It is used to test sync functionality.
type mockDatabase struct {
	st store.Store
}

func (d *mockDatabase) St() store.Store {
	return d.st
}

func (d *mockDatabase) CheckPermsInternal(ctx *context.T, call rpc.ServerCall, st store.StoreReader) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *mockDatabase) SetPermsInternal(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *mockDatabase) Name() string {
	return "mockdb"
}

func (d *mockDatabase) App() interfaces.App {
	return nil
}

func (d *mockDatabase) Table(ctx *context.T, tableName string) interfaces.Table {
	return nil
}

func (d *mockDatabase) GetSchemaMetadataInternal(ctx *context.T) (*wire.SchemaMetadata, error) {
	return nil, verror.NewErrNoExist(ctx)
}

func (d *mockDatabase) CrConnectionStream() wire.ConflictManagerStartConflictResolverServerStream {
	return mockCRStream
}

func (d *mockDatabase) ResetCrConnectionStream() {
	mockCRStream = nil
}

// createService creates a mock Syncbase service used for testing sync functionality.
func createService(t *testing.T) *mockService {
	ctx, shutdown := test.V23Init()
	engine := store.EngineForTest
	opts := util.OpenOptions{CreateIfMissing: true, ErrorIfExists: false}
	dir := fmt.Sprintf("%s/vsync_test_%d_%d", os.TempDir(), os.Getpid(), time.Now().UnixNano())

	st, err := util.OpenStore(engine, path.Join(dir, engine), opts)
	if err != nil {
		t.Fatalf("cannot create store %s (%s): %v", engine, dir, err)
	}
	vclock := clock.NewVClock(st)
	st, err = watchable.Wrap(st, vclock, &watchable.Options{
		ManagedPrefixes: []string{util.RowPrefix, util.PermsPrefix},
	})

	s := &mockService{
		st:       st,
		engine:   engine,
		dir:      dir,
		shutdown: shutdown,
	}
	if s.sync, err = New(ctx, s, engine, dir, vclock, true); err != nil {
		util.DestroyStore(engine, dir)
		t.Fatalf("cannot create sync service: %v", err)
	}
	return s
}

func setMockCRStream(crs *conflictResolverStream) {
	mockCRStream = crs
}

// destroyService cleans up the mock Syncbase service.
func destroyService(t *testing.T, s *mockService) {
	defer s.shutdown()
	defer Close(s.sync)
	if err := util.DestroyStore(s.engine, s.dir); err != nil {
		t.Fatalf("cannot destroy store %s (%s): %v", s.engine, s.dir, err)
	}
}

// makeRowKey returns the database row key for a given application key.
func makeRowKey(key string) string {
	return util.JoinKeyParts(util.RowPrefix, key)
}

func makeRowKeyFromParts(table, row string) string {
	return util.JoinKeyParts(util.RowPrefix, table, row)
}

func makePermsKeyFromParts(table, row string) string {
	return util.JoinKeyParts(util.PermsPrefix, table, row)
}

// conflictResolverStream mock for testing.
type conflictResolverStream struct {
	sendQ   []wire.ConflictInfo
	recvQ   []wire.ResolutionInfo
	sendErr error
	recvErr error
}

func (crs *conflictResolverStream) RecvStream() interface {
	Advance() bool
	Value() wire.ResolutionInfo
	Err() error
} {
	return &recvStream{index: -1, cr: crs}
}

func (crs *conflictResolverStream) SendStream() interface {
	Send(item wire.ConflictInfo) error
} {
	return &sendStream{cr: crs}
}

type sendStream struct {
	cr *conflictResolverStream
}

func (ss *sendStream) Send(item wire.ConflictInfo) error {
	if ss.cr.sendErr != nil {
		return ss.cr.sendErr
	}
	ss.cr.sendQ = append(ss.cr.sendQ, item)
	return nil
}

type recvStream struct {
	index int
	cr    *conflictResolverStream
}

func (rs *recvStream) Advance() bool {
	rs.index++
	return rs.index < len(rs.cr.recvQ)
}

func (rs *recvStream) Value() wire.ResolutionInfo {
	return rs.cr.recvQ[rs.index]
}

func (rs *recvStream) Err() error {
	return rs.cr.recvErr
}
