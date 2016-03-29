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
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/store"
	storeutil "v.io/x/ref/services/syncbase/store/util"
	"v.io/x/ref/services/syncbase/store/watchable"
	"v.io/x/ref/services/syncbase/vclock"
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
	vclock   *vclock.VClock
	sync     *syncService
	shutdown func()
}

func (s *mockService) St() store.Store {
	return s.st
}

func (s *mockService) Sync() interfaces.SyncServerMethods {
	return s.sync
}

func (s *mockService) Clock() common.Clock {
	return s.vclock
}

func (s *mockService) App(ctx *context.T, call rpc.ServerCall, appName string) (interfaces.App, error) {
	return &mockApp{s: s}, nil
}

func (s *mockService) AppNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	return []string{"mockapp"}, nil
}

// mockApp emulates a Syncbase App.  It is used to access a mock database.
type mockApp struct {
	s *mockService
}

func (a *mockApp) Database(ctx *context.T, call rpc.ServerCall, dbName string) (interfaces.Database, error) {
	wst, err := watchable.Wrap(a.s.st, a.s.vclock, &watchable.Options{
		ManagedPrefixes: []string{common.RowPrefix, common.PermsPrefix},
	})
	return &mockDatabase{st: wst}, err
}

func (a *mockApp) DatabaseNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	return []string{"mockdb"}, nil
}

func (a *mockApp) CreateDatabase(ctx *context.T, call rpc.ServerCall, dbName string, perms access.Permissions, metadata *wire.SchemaMetadata) error {
	return verror.NewErrNotImplemented(ctx)
}

func (a *mockApp) DestroyDatabase(ctx *context.T, call rpc.ServerCall, dbName string) error {
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

// mockDatabase emulates a Syncbase Database. It is used to test sync
// functionality.
type mockDatabase struct {
	st *watchable.Store
}

func (d *mockDatabase) St() *watchable.Store {
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

func (d *mockDatabase) Collection(ctx *context.T, collectionName string) interfaces.Collection {
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

// createService creates a mock Syncbase service used for testing sync
// functionality.
func createService(t *testing.T) *mockService {
	ctx, shutdown := test.V23Init()
	engine := store.EngineForTest
	opts := storeutil.OpenOptions{CreateIfMissing: true, ErrorIfExists: false}
	dir := fmt.Sprintf("%s/vsync_test_%d_%d", os.TempDir(), os.Getpid(), time.Now().UnixNano())

	st, err := storeutil.OpenStore(engine, path.Join(dir, engine), opts)
	if err != nil {
		t.Fatalf("cannot create store %s (%s): %v", engine, dir, err)
	}

	cl := vclock.NewVClock(st)
	if err := cl.InitVClockData(); err != nil {
		t.Fatalf("InitVClockData failed: %v", err)
	}

	s := &mockService{
		engine:   engine,
		dir:      dir,
		st:       st,
		vclock:   cl,
		shutdown: shutdown,
	}
	if s.sync, err = New(ctx, s, engine, dir, cl, true); err != nil {
		storeutil.DestroyStore(engine, dir)
		t.Fatalf("cannot create sync service: %v", err)
	}
	return s
}

// createDatabase creates a mock database.
func createDatabase(t *testing.T, s *mockService) *mockDatabase {
	app, err := s.App(nil, nil, "mockapp")
	if err != nil {
		t.Errorf("Error while creating App: %v", err)
	}
	db, err := app.Database(nil, nil, "mockdb")
	if err != nil {
		t.Errorf("Error while creating Database: %v", err)
	}
	// TODO(razvanm): Get rid of the type assertion from below.
	return db.(*mockDatabase)
}

func setMockCRStream(crs *conflictResolverStream) {
	mockCRStream = crs
}

// destroyService cleans up the mock Syncbase service.
func destroyService(t *testing.T, s *mockService) {
	defer s.shutdown()
	defer Close(s.sync)
	if err := storeutil.DestroyStore(s.engine, s.dir); err != nil {
		t.Fatalf("cannot destroy store %s (%s): %v", s.engine, s.dir, err)
	}
}

// makeRowKey returns the database row key for a given application key.
func makeRowKey(key string) string {
	return common.JoinKeyParts(common.RowPrefix, key)
}

func makeRowKeyFromParts(collection, row string) string {
	return common.JoinKeyParts(common.RowPrefix, collection, row)
}

func makePermsKeyFromParts(collection, row string) string {
	return common.JoinKeyParts(common.PermsPrefix, collection, row)
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
