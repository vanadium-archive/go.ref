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
	prefixutil "v.io/syncbase/v23/syncbase/util"
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
	st     store.Store // stores all data for a single database

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
	_ util.Layer                 = (*database)(nil)
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

// NewDatabase creates a new database instance and returns it.
// Returns a VDL-compatible error.
// Designed for use from within App.CreateNoSQLDatabase.
func NewDatabase(ctx *context.T, call rpc.ServerCall, a interfaces.App, name string, opts DatabaseOptions) (*database, error) {
	if opts.Perms == nil {
		return nil, verror.New(verror.ErrInternal, ctx, "perms must be specified")
	}
	st, err := util.OpenStore(opts.Engine, path.Join(opts.RootDir, opts.Engine))
	if err != nil {
		return nil, err
	}
	st, err = watchable.Wrap(st, &watchable.Options{
		ManagedPrefixes: []string{util.RowPrefix},
	})
	if err != nil {
		return nil, err
	}
	d := &database{
		name:   name,
		a:      a,
		exists: true,
		st:     st,
		sns:    make(map[uint64]store.Snapshot),
		txs:    make(map[uint64]store.Transaction),
	}
	data := &databaseData{
		Name:  d.name,
		Perms: opts.Perms,
	}
	if err := util.Put(ctx, d.st, d, data); err != nil {
		return nil, err
	}
	return d, nil
}

////////////////////////////////////////
// RPC methods

func (d *databaseReq) Create(ctx *context.T, call rpc.ServerCall, perms access.Permissions) error {
	if d.exists {
		return verror.New(verror.ErrExist, ctx, d.name)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	// This database does not yet exist; d is just an ephemeral handle that holds
	// {name string, a *app}. d.a.CreateNoSQLDatabase will create a new database
	// handle and store it in d.a.dbs[d.name].
	return d.a.CreateNoSQLDatabase(ctx, call, d.name, perms)
}

func (d *databaseReq) Delete(ctx *context.T, call rpc.ServerCall) error {
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return d.a.DeleteNoSQLDatabase(ctx, call, d.name)
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
	if err := util.Get(ctx, call, d.st, d, data); err != nil {
		return nil, "", err
	}
	return data.Perms, util.FormatVersion(data.Version), nil
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
	if err := util.Get(ctx, call, sn, d, &databaseData{}); err != nil {
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

func (d *database) App() interfaces.App {
	return d.a
}

func (d *database) CheckPermsInternal(ctx *context.T, call rpc.ServerCall, st store.StoreReadWriter) error {
	if !d.exists {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return util.Get(ctx, call, st, d, &databaseData{})
}

func (d *database) SetPermsInternal(ctx *context.T, call rpc.ServerCall, perms access.Permissions, version string) error {
	if !d.exists {
		vlog.Fatalf("database %q does not exist", d.name)
	}
	return store.RunInTransaction(d.st, func(st store.StoreReadWriter) error {
		data := &databaseData{}
		return util.Update(ctx, call, st, d, data, func() error {
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

func (d *database) Name() string {
	return d.name
}

func (d *database) StKey() string {
	return util.DatabasePrefix
}

////////////////////////////////////////
// query_db implementations

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
	if err := util.Get(db.ctx, db.call, db.st, tDb.req, &tableData{}); err != nil {
		return nil, err
	}
	return tDb, nil
}

type tableDb struct {
	qdb *queryDb
	req *tableReq
}

func (t *tableDb) Scan(prefixes []string) (query_db.KeyValueStream, error) {
	return &kvs{
		t:        t,
		prefixes: prefixes,
		curr:     -1,
		validRow: false,
		it:       nil,
		err:      nil,
	}, nil
}

type kvs struct {
	t         *tableDb
	prefixes  []string
	curr      int // current index into prefixes, -1 at start
	validRow  bool
	currKey   string
	currValue *vdl.Value
	it        store.Stream // current prefix key value stream
	err       error
}

func (s *kvs) Advance() bool {
	if s.err != nil {
		return false
	}
	if s.curr == -1 {
		s.curr++
	}
	for s.curr < len(s.prefixes) {
		if s.it == nil {
			start := prefixutil.PrefixRangeStart(s.prefixes[s.curr])
			limit := prefixutil.PrefixRangeLimit(s.prefixes[s.curr])
			s.it = s.t.qdb.st.Scan(util.ScanRangeArgs(util.JoinKeyParts(util.RowPrefix, s.t.req.name), string(start), string(limit)))
		}
		if s.it.Advance() {
			// key
			keyBytes := s.it.Key(nil)
			parts := util.SplitKeyParts(string(keyBytes))
			// TODO(rogulenko): Check access for the key.
			s.currKey = parts[len(parts)-1]
			// value
			valueBytes := s.it.Value(nil)
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
		if err := s.it.Err(); err != nil {
			s.validRow = false
			s.err = err
			return false
		}
		// We've reached the end of the iterator for this prefix.
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
		s.it.Cancel()
		s.it = nil
	}
	// set curr to end of prefixes so Advance will return false
	s.curr = len(s.prefixes)
}

////////////////////////////////////////
// Internal helpers

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
