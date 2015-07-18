// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"math/rand"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/v23/syncbase/nosql/query_db"
	"v.io/syncbase/v23/syncbase/nosql/query_exec"
	"v.io/syncbase/x/ref/services/syncbase/localblobstore"
	"v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore"
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/server/watchable"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
)

// database is a per-database singleton (i.e. not per-request). It does not
// directly handle RPCs.
// Note: If a database does not exist at the time of a database RPC, the
// dispatcher creates a short-lived database object to service that particular
// request.
type database struct {
	name string
	a    interfaces.App
	// The fields below are initialized iff this database exists.
	exists bool
	// TODO(sadovsky): Make st point to a store.Store wrapper that handles paging,
	// and do not actually open the store in NewDatabase.
	st store.Store // stores all data for a single database

	// Local blob store associated with this database.
	bst localblobstore.BlobStore

	// Active snapshots and transactions corresponding to client batches.
	// TODO(sadovsky): Add timeouts and GC.
	mu  sync.Mutex // protects the fields below
	sns map[uint64]store.Snapshot
	txs map[uint64]store.Transaction
}

// databaseReq is a per-request object that handles Database RPCs.
// It embeds database and tracks request-specific batch state.
type databaseReq struct {
	*database
	// If non-nil, sn or tx will be non-nil.
	batchId *uint64
	sn      store.Snapshot
	tx      store.Transaction
}

var (
	_ wire.DatabaseServerMethods = (*databaseReq)(nil)
	_ interfaces.Database        = (*database)(nil)
)

// DatabaseOptions configures a database.
type DatabaseOptions struct {
	// Database-level permissions.
	Perms access.Permissions
	// Root dir for data storage.
	RootDir string
	// Storage engine to use.
	Engine string
}

// OpenDatabase opens a database and returns a *database for it. Designed for
// use from within NewDatabase and server.NewService.
func OpenDatabase(ctx *context.T, a interfaces.App, name string, opts DatabaseOptions, openOpts util.OpenOptions) (*database, error) {
	st, err := util.OpenStore(opts.Engine, path.Join(opts.RootDir, opts.Engine), openOpts)
	if err != nil {
		return nil, err
	}
	st, err = watchable.Wrap(st, &watchable.Options{
		ManagedPrefixes: []string{util.RowPrefix, util.PermsPrefix},
	})
	if err != nil {
		return nil, err
	}
	// Open a co-located blob store, adjacent to the structured store.
	bst, err := fs_cablobstore.Create(ctx, path.Join(opts.RootDir, "blobs"))
	if err != nil {
		return nil, err
	}
	return &database{
		name:   name,
		a:      a,
		exists: true,
		st:     st,
		bst:    bst,
		sns:    make(map[uint64]store.Snapshot),
		txs:    make(map[uint64]store.Transaction),
	}, nil
}

// NewDatabase creates a new database instance and returns it.
// Designed for use from within App.CreateNoSQLDatabase.
func NewDatabase(ctx *context.T, a interfaces.App, name string, metadata *wire.SchemaMetadata, opts DatabaseOptions) (*database, error) {
	if opts.Perms == nil {
		return nil, verror.New(verror.ErrInternal, ctx, "perms must be specified")
	}
	d, err := OpenDatabase(ctx, a, name, opts, util.OpenOptions{CreateIfMissing: true, ErrorIfExists: true})
	if err != nil {
		return nil, err
	}
	data := &databaseData{
		Name:           d.name,
		Perms:          opts.Perms,
		SchemaMetadata: metadata,
	}
	if err := util.Put(ctx, d.st, d.stKey(), data); err != nil {
		return nil, err
	}
	return d, nil
}

////////////////////////////////////////
// RPC methods

func (d *databaseReq) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions, metadata *wire.SchemaMetadata) error {
	if d.exists {
		return verror.New(verror.ErrExist, ctx, d.name)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	// This database does not yet exist; d is just an ephemeral handle that holds
	// {name string, a *app}. d.a.CreateNoSQLDatabase will create a new database
	// handle and store it in d.a.dbs[d.name].
	return d.a.CreateNoSQLDatabase(ctx, call, d.name, perms, metadata)
}

func (d *databaseReq) Delete(ctx *context.T, call rpc.ServerCall) error {
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return d.a.DeleteNoSQLDatabase(ctx, call, d.name)
}

func (d *databaseReq) Exists(ctx *context.T, call rpc.ServerCall) (bool, error) {
	if !d.exists {
		return false, nil
	}
	return util.ErrorToExists(util.GetWithAuth(ctx, call, d.st, d.stKey(), &databaseData{}))
}

var rng *rand.Rand = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))

func (d *databaseReq) BeginBatch(ctx *context.T, call rpc.ServerCall, bo wire.BatchOptions) (string, error) {
	if !d.exists {
		return "", verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return "", wire.NewErrBoundToBatch(ctx)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var id uint64
	var batchType string
	for {
		id = uint64(rng.Int63())
		if bo.ReadOnly {
			if _, ok := d.sns[id]; !ok {
				d.sns[id] = d.st.NewSnapshot()
				batchType = "sn"
				break
			}
		} else {
			if _, ok := d.txs[id]; !ok {
				d.txs[id] = d.st.NewTransaction()
				batchType = "tx"
				break
			}
		}
	}
	return strings.Join([]string{d.name, batchType, strconv.FormatUint(id, 10)}, util.BatchSep), nil
}

func (d *databaseReq) Commit(ctx *context.T, call rpc.ServerCall) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId == nil {
		return wire.NewErrNotBoundToBatch(ctx)
	}
	if d.tx == nil {
		return wire.NewErrReadOnlyBatch(ctx)
	}
	var err error
	if err = d.tx.Commit(); err == nil {
		d.mu.Lock()
		delete(d.txs, *d.batchId)
		d.mu.Unlock()
	}
	return err
}

func (d *databaseReq) Abort(ctx *context.T, call rpc.ServerCall) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId == nil {
		return wire.NewErrNotBoundToBatch(ctx)
	}
	var err error
	if d.tx != nil {
		if err = d.tx.Abort(); err == nil {
			d.mu.Lock()
			delete(d.txs, *d.batchId)
			d.mu.Unlock()
		}
	} else {
		if err = d.sn.Close(); err == nil {
			d.mu.Lock()
			delete(d.sns, *d.batchId)
			d.mu.Unlock()
		}
	}
	return err
}

func (d *databaseReq) Exec(ctx *context.T, call wire.DatabaseExecServerCall, q string) error {
	impl := func(headers []string, rs ResultStream, err error) error {
		if err != nil {
			return err
		}
		sender := call.SendStream()
		// Push the headers first -- the client will retrieve them and return
		// them separately from the results.
		var resultHeaders []*vdl.Value
		for _, header := range headers {
			resultHeaders = append(resultHeaders, vdl.ValueOf(header))
		}
		sender.Send(resultHeaders)
		for rs.Advance() {
			result := rs.Result()
			sender.Send(result)
		}
		return rs.Err()
	}
	var st store.StoreReader
	if d.batchId != nil {
		st = d.batchReader()
	} else {
		sn := d.st.NewSnapshot()
		st = sn
		defer sn.Close()
	}
	// queryDb implements query_db.Database
	// which is needed by the query package's
	// Exec function.
	db := &queryDb{
		ctx:  ctx,
		call: call,
		req:  d,
		st:   st,
	}

	return impl(query_exec.Exec(db, q))
}

func (d *databaseReq) SetPermissions(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return d.a.SetDatabasePerms(ctx, call, d.name, perms, version)
}

func (d *databaseReq) GetPermissions(ctx *context.T, call rpc.ServerCall) (perms access.Permissions, version string, err error) {
	if !d.exists {
		return nil, "", verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return nil, "", wire.NewErrBoundToBatch(ctx)
	}
	data := &databaseData{}
	if err := util.GetWithAuth(ctx, call, d.st, d.stKey(), data); err != nil {
		return nil, "", err
	}
	return data.Perms, util.FormatVersion(data.Version), nil
}

func (d *databaseReq) Watch(ctx *context.T, call wire.DatabaseWatcherWatchServerCall, req wire.WatchRequest) error {
	// TODO(rogulenko): Implement.
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return verror.NewErrNotImplemented(ctx)
}

func (d *databaseReq) GetResumeMarker(ctx *context.T, call rpc.ServerCall) (wire.ResumeMarker, error) {
	// TODO(rogulenko): Implement.
	if !d.exists {
		return "", verror.New(verror.ErrNoExist, ctx, d.name)
	}
	return "", verror.NewErrNotImplemented(ctx)
}

func (d *databaseReq) GlobChildren__(ctx *context.T, call rpc.ServerCall) (<-chan string, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.name)
	}
	if d.batchId != nil {
		return nil, wire.NewErrBoundToBatch(ctx)
	}
	// Check perms.
	sn := d.st.NewSnapshot()
	closeSnapshot := func() error {
		return sn.Close()
	}
	if err := util.GetWithAuth(ctx, call, sn, d.stKey(), &databaseData{}); err != nil {
		closeSnapshot()
		return nil, err
	}
	return util.Glob(ctx, call, "*", sn, closeSnapshot, util.TablePrefix)
}

////////////////////////////////////////
// ResultStream interface

// ResultStream is an interface for iterating through results (a.k.a, rows) returned from a
// query.  Each resulting rows are arrays of vdl objects.
type ResultStream interface {
	// Advance stages an element so the client can retrieve it with Result.
	// Advance returns true iff there is a result to retrieve. The client must
	// call Advance before calling Result. The client must call Cancel if it
	// does not iterate through all elements (i.e. until Advance returns false).
	// Advance may block if an element is not immediately available.
	Advance() bool

	// Result returns the row (i.e., array of vdl Values) that was staged by Advance.
	// Result may panic if Advance returned false or was not called at all.
	// Result does not block.
	Result() []*vdl.Value

	// Err returns a non-nil error iff the stream encountered any errors. Err does
	// not block.
	Err() error

	// Cancel notifies the ResultStream provider that it can stop producing results.
	// The client must call Cancel if it does not iterate through all results
	// (i.e. until Advance returns false). Cancel is idempotent and can be called
	// concurrently with a goroutine that is iterating via Advance/Result.
	// Cancel causes Advance to subsequently return false. Cancel does not block.
	Cancel()
}

////////////////////////////////////////
// interfaces.Database methods

func (d *database) St() store.Store {
	if !d.exists {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return d.st
}

func (d *database) BlobSt() localblobstore.BlobStore {
	if !d.exists {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return d.bst
}

func (d *database) App() interfaces.App {
	return d.a
}

func (d *database) CheckPermsInternal(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter) error {
	if !d.exists {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return util.GetWithAuth(ctx, call, st, d.stKey(), &databaseData{})
}

func (d *database) SetPermsInternal(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	if !d.exists {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return store.RunInTransaction(d.st, func(st store.StoreReadWriter) error {
		data := &databaseData{}
		return util.UpdateWithAuth(ctx, call, st, d.stKey(), data, func() error {
			if err := util.CheckVersion(ctx, version, data.Version); err != nil {
				return err
			}
			data.Perms = perms
			data.Version++
			return nil
		})
	})
}

func (d *database) Name() string {
	return d.name
}

////////////////////////////////////////
// query_db implementation

// Implement query_db's Database, Table and KeyValueStream interfaces.
type queryDb struct {
	ctx  *context.T
	call wire.DatabaseExecServerCall
	req  *databaseReq
	st   store.StoreReader
}

func (db *queryDb) GetContext() *context.T {
	return db.ctx
}

func (db *queryDb) GetTable(name string) (query_db.Table, error) {
	tDb := &tableDb{
		qdb: db,
		req: &tableReq{
			name: name,
			d:    db.req,
		},
	}
	// Now that we have a table, we need to check permissions.
	if err := util.GetWithAuth(db.ctx, db.call, db.st, tDb.req.stKey(), &tableData{}); err != nil {
		return nil, err
	}
	return tDb, nil
}

type tableDb struct {
	qdb *queryDb
	req *tableReq
}

func (t *tableDb) Scan(keyRanges query_db.KeyRanges) (query_db.KeyValueStream, error) {
	streams := []store.Stream{}
	for _, keyRange := range keyRanges {
		// TODO(jkline): For now, acquire all of the streams at once to minimize the race condition.
		//               Need a way to Scan multiple ranges at the same state of uncommitted changes.
		streams = append(streams, t.qdb.st.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, t.req.name), keyRange.Start, keyRange.Limit)))
	}
	return &kvs{
		t:        t,
		curr:     0,
		validRow: false,
		it:       streams,
		err:      nil,
	}, nil
}

type kvs struct {
	t         *tableDb
	curr      int
	validRow  bool
	currKey   string
	currValue *vdl.Value
	it        []store.Stream // array of store.Streams
	err       error
}

func (s *kvs) Advance() bool {
	if s.err != nil {
		return false
	}
	for s.curr < len(s.it) {
		if s.it[s.curr].Advance() {
			// key
			keyBytes := s.it[s.curr].Key(nil)
			parts := util.SplitKeyParts(string(keyBytes))
			// TODO(rogulenko): Check access for the key.
			s.currKey = parts[len(parts)-1]
			// value
			valueBytes := s.it[s.curr].Value(nil)
			var currValue *vdl.Value
			if err := vom.Decode(valueBytes, &currValue); err != nil {
				s.validRow = false
				s.err = err
				return false
			}
			s.currValue = currValue
			s.validRow = true
			return true
		}
		// Advance returned false.  It could be an err, or it could
		// be we've reached the end.
		if err := s.it[s.curr].Err(); err != nil {
			s.validRow = false
			s.err = err
			return false
		}
		// We've reached the end of the iterator for this keyRange.
		// Jump to the next one.
		s.curr++
		s.it = nil
		s.validRow = false
	}
	// There are no more prefixes to scan.
	return false
}

func (s *kvs) KeyValue() (string, *vdl.Value) {
	if !s.validRow {
		return "", nil
	}
	return s.currKey, s.currValue
}

func (s *kvs) Err() error {
	return s.err
}

func (s *kvs) Cancel() {
	if s.it != nil {
		for i := s.curr; i < len(s.it); i++ {
			s.it[i].Cancel()
		}
		s.it = nil
	}
	// set curr to end of keyRanges so Advance will return false
	s.curr = len(s.it)
}

////////////////////////////////////////
// Internal helpers

func (d *database) stKey() string {
	return util.DatabasePrefix
}

func (d *databaseReq) batchReader() store.StoreReader {
	if d.batchId == nil {
		return nil
	} else if d.sn != nil {
		return d.sn
	} else {
		return d.tx
	}
}

func (d *databaseReq) batchReadWriter() (store.StoreReadWriter, error) {
	if d.batchId == nil {
		return nil, nil
	} else if d.tx != nil {
		return d.tx, nil
	} else {
		return nil, wire.NewErrReadOnlyBatch(nil)
	}
}
