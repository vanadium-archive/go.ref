package server

// This file defines thing, which implements the server-side Thing API from
// veyron2/services/store/service.vdl.

import (
	"veyron/services/store/memstore"

	"veyron2/ipc"
	"veyron2/query"
	"veyron2/security"
	"veyron2/services/mounttable"
	mttypes "veyron2/services/mounttable/types"
	"veyron2/services/store"
	"veyron2/services/watch"
	watchtypes "veyron2/services/watch/types"
	"veyron2/storage"
	"veyron2/vdl/vdlutil"
	"veyron2/verror"
)

const (
	// Large random value, used to indicate that this memstore object represents
	// a directory.
	dirValue int64 = 9380751577234
)

type thing struct {
	name   string // will never contain a transaction id
	obj    *memstore.Object
	tid    transactionID // may be nullTransactionID
	server *Server
}

var (
	// TODO(sadovsky): Make sure all of these error cases are covered by tests.
	// TODO(sadovsky): Revisit these error types.
	errMakeDirObjectExists     = verror.Existsf("Object exists in Dir.Make path")
	errCalledObjectMethodOnDir = verror.BadArgf("called Object method on Dir")
	errPutObjectInObject       = verror.BadArgf("put Object in another Object")
	errCannotRemoveRootDir     = verror.BadArgf("cannot remove root Dir")

	_ storeThingService = (*thing)(nil)

	nullEntry storage.Entry
	nullStat  storage.Stat
)

func isDir(entry *storage.Entry) bool {
	value, ok := entry.Value.(int64)
	return ok && value == dirValue
}

func isObject(entry *storage.Entry) bool {
	return !isDir(entry)
}

func (t *thing) String() string {
	return t.name
}

func (t *thing) Attributes(arg string) map[string]string {
	return map[string]string{
		"health":     "ok",
		"servertype": t.String(),
	}
}

////////////////////////////////////////
// DirOrObject methods

// Remove removes this thing.
func (t *thing) Remove(ctx ipc.ServerContext) error {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return err
	}
	path := storage.ParsePath(t.name)
	if len(path) == 0 {
		return errCannotRemoveRootDir
	}
	return t.obj.Remove(ctx.RemoteID(), tx)
}

// Query returns a sequence of objects that match the given query.
func (t *thing) Query(ctx ipc.ServerContext, q query.Query, stream store.DirOrObjectServiceQueryStream) error {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return err
	}
	it, err := t.obj.Query(ctx.RemoteID(), tx, q)
	if err != nil {
		return err
	}
	for it.Next() {
		if err := stream.SendStream().Send(*it.Get()); err != nil {
			it.Abort()
			return err
		}
	}
	return it.Err()
}

// Stat returns information about this thing.
func (t *thing) Stat(ctx ipc.ServerContext) (storage.Stat, error) {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return nullStat, err
	}
	s, err := t.obj.Stat(ctx.RemoteID(), tx)
	if err != nil {
		return nullStat, err
	}
	// Determine the Kind.
	entry, err := t.obj.Get(ctx.RemoteID(), tx)
	if err != nil {
		// TODO(sadovsky): Is this the right thing to return here? If obj.Get()
		// returns state.errNotFound, it probably makes more sense to return
		// nullStat and nil error. (Note, for now t.obj.Stat() is not implemented
		// and always returns an error, so we never actually get here.)
		return nullStat, err
	}
	if isDir(entry) {
		s.Kind = storage.DirKind
	} else {
		s.Kind = storage.ObjectKind
	}
	return *s, err
}

// Exists returns true iff this thing is present in the store.
func (t *thing) Exists(ctx ipc.ServerContext) (bool, error) {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return false, err
	}
	return t.obj.Exists(ctx.RemoteID(), tx)
}

// NewTransaction creates a transaction with the given options.  It returns the
// name of the transaction relative to this thing's name.
func (t *thing) NewTransaction(ctx ipc.ServerContext, opts []vdlutil.Any) (string, error) {
	if t.tid != nullTransactionID {
		return "", errNestedTransaction
	}
	return t.server.createTransaction(ctx, t.name)
}

// Glob streams a series of names that match the given pattern.
func (t *thing) Glob(ctx ipc.ServerContext, pattern string, stream mounttable.GlobbableServiceGlobStream) error {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return err
	}
	it, err := t.obj.Glob(ctx.RemoteID(), tx, pattern)
	if err != nil {
		return err
	}
	gsa := &globStreamAdapter{stream}
	for ; it.IsValid(); it.Next() {
		if err := gsa.SendStream().Send(it.Name()); err != nil {
			return err
		}
	}
	return nil
}

// WatchGlob returns a stream of changes that match a pattern.
func (t *thing) WatchGlob(ctx ipc.ServerContext, req watchtypes.GlobRequest, stream watch.GlobWatcherServiceWatchGlobStream) error {
	return t.server.watcher.WatchGlob(ctx, storage.ParsePath(t.name), req, stream)
}

// WatchQuery returns a stream of changes that satisfy a query.
func (t *thing) WatchQuery(ctx ipc.ServerContext, req watchtypes.QueryRequest, stream watch.QueryWatcherServiceWatchQueryStream) error {
	return t.server.watcher.WatchQuery(ctx, storage.ParsePath(t.name), req, stream)
}

////////////////////////////////////////
// Dir-only methods

// Called by Make(), and also called directly by server.go with a nil
// transaction to create the root directory.
func (t *thing) makeInternal(remoteID security.PublicID, tx *memstore.Transaction) error {
	// Make dirs from the top down. Return error if we encounter an Object.
	parts := storage.PathName{""}
	parts = append(parts, storage.ParsePath(t.name)...)
	// Set to true once we encounter a path component that doesn't already
	// exist. Create new dirs from there on down.
	newTree := false
	for i, _ := range parts {
		obj := t.server.store.Bind(parts[:i+1].String())
		if !newTree {
			// Note, obj.Get() returns state.errNotFound if the entry does not exist.
			// TODO(sadovsky): Check for that specific error type and propagate all
			// other errors.
			entry, err := obj.Get(remoteID, tx)
			if err != nil {
				newTree = true
			} else if isObject(entry) {
				return errMakeDirObjectExists
			}
		}
		if newTree {
			obj.Put(remoteID, tx, dirValue)
		}
	}
	return nil
}

func (t *thing) Make(ctx ipc.ServerContext) error {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return err
	}
	return t.makeInternal(ctx.RemoteID(), tx)
}

////////////////////////////////////////
// Object-only methods

// Get returns the value for the Object.  The value returned is from the
// most recent mutation of the entry in the Transaction, or from the
// Transaction's snapshot if there is no mutation.
func (t *thing) Get(ctx ipc.ServerContext) (storage.Entry, error) {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return nullEntry, err
	}
	// Note, obj.Get() returns state.errNotFound if the entry does not exist.
	entry, err := t.obj.Get(ctx.RemoteID(), tx)
	if err != nil {
		return nullEntry, err
	}
	if isDir(entry) {
		return nullEntry, errCalledObjectMethodOnDir
	}
	return *entry, err
}

// Put modifies the value of the Object.
func (t *thing) Put(ctx ipc.ServerContext, val vdlutil.Any) (storage.Stat, error) {
	tx, err := t.server.findTransaction(ctx, t.tid)
	if err != nil {
		return nullStat, err
	}
	// Verify that this entry either doesn't exist or exists and is an Object.
	// Note, obj.Get() returns state.errNotFound if the entry does not exist.
	// TODO(sadovsky): Check for that specific error type and propagate all
	// other errors.
	entry, err := t.obj.Get(ctx.RemoteID(), tx)
	if err == nil && isDir(entry) {
		return nullStat, errCalledObjectMethodOnDir
	}
	// Verify that the parent already exists and is a Dir.
	// Note, at this point we know t.name isn't the root of the store, b/c if it
	// were, the check above would've failed.
	path := storage.ParsePath(t.name)
	path = path[:len(path)-1] // path to parent
	// Note, we don't return an error here if path doesn't exist -- we let the
	// memstore Put() code handle that case.
	entry, err = t.server.store.Bind(path.String()).Get(ctx.RemoteID(), tx)
	if err == nil && isObject(entry) {
		return nullStat, errPutObjectInObject
	}
	s, err := t.obj.Put(ctx.RemoteID(), tx, interface{}(val))
	if err != nil {
		return nullStat, err
	}
	// TODO(sadovsky): Add test for this.
	s.Kind = storage.ObjectKind
	return *s, err
}

////////////////////////////////////////
// Transaction methods

func (t *thing) Commit(ctx ipc.ServerContext) error {
	return t.server.commitTransaction(ctx, t.tid)
}

func (t *thing) Abort(ctx ipc.ServerContext) error {
	return t.server.abortTransaction(ctx, t.tid)
}

////////////////////////////////////////
// SyncGroup methods.
// See veyron2/services/store/service.vdl for detailed comments.
// TODO(hpucha): Actually implement these methods.

// GetSyncGroupNames returns the global names of all SyncGroups attached
// to this directory.
func (t *thing) GetSyncGroupNames(ctx ipc.ServerContext) (names []string, err error) {
	return nil, verror.Internalf("GetSyncGroupNames not yet implemented")
}

// CreateSyncGroup creates a new SyncGroup.
func (t *thing) CreateSyncGroup(ctx ipc.ServerContext, name string, config store.SyncGroupConfig) error {
	// One approach is that all syncgroup operations are
	// serialized with a single lock. This enables easily checking
	// nesting of syncgroups.

	// sgop.Lock(), defer sgop.Unlock()
	// acl check
	// sanity checks
	//      sgname in the dir does not exist
	//      syncgroup is not nested
	// call syncd
	// if err != nil return err
	// update dir->sgname mapping

	return verror.Internalf("CreateSyncGroup not yet implemented")
}

// JoinSyncGroup joins a SyncGroup with the specified global Veyron name.
func (t *thing) JoinSyncGroup(ctx ipc.ServerContext, name string) error {
	// sgop.Lock(), defer sgop.Unlock()
	// acl check (parentdir or dir)
	// sanity checks
	//      if dir exists
	//        sgname in the dir does not exist
	//      syncgroup is not nested
	// call syncd
	// if err != nil return err
	// if err == nil && rootoid does not exist && dir does not exist
	//      mkdir(rootoid) and setacl
	//      update dir->sgname mapping
	// if err == nil && rootoid exists && dir exists && oid(dir) == rootoid
	//      update dir->sgname mapping
	// All other cases are error, call syncd.leave(sg) and return err

	return verror.Internalf("JoinSyncGroup not yet implemented")
}

// LeaveSyncGroup leaves the SyncGroup.
func (t *thing) LeaveSyncGroup(ctx ipc.ServerContext, name string) error {
	// sgop.Lock(), defer sgop.Unlock()
	// acl check
	// sanity checks (sgname in the dir exists)
	// call syncd
	// if err != nil return err
	// update dir->sgname mapping
	// if sglist in dir is empty, rewrite oids.

	return verror.Internalf("LeaveSyncGroup not yet implemented")
}

// DestroySyncGroup destroys the SyncGroup.
func (t *thing) DestroySyncGroup(ctx ipc.ServerContext, name string) error {
	return verror.Internalf("DestroySyncGroup not yet implemented")
}

// EjectFromSyncGroup ejects a member from the SyncGroup.
func (t *thing) EjectFromSyncGroup(ctx ipc.ServerContext, name, member string) error {
	return verror.Internalf("EjectFromSyncGroup not yet implemented")
}

// GetSyncGroupConfig gets the config info of the SyncGroup.
func (t *thing) GetSyncGroupConfig(ctx ipc.ServerContext, name string) (config store.SyncGroupConfig, eTag string, err error) {
	return store.SyncGroupConfig{}, "", verror.Internalf("GetSyncGroupConfig not yet implemented")
}

// SetSyncGroupConfig sets the config info of the SyncGroup.
func (t *thing) SetSyncGroupConfig(ctx ipc.ServerContext, name string, config store.SyncGroupConfig, eTag string) error {
	return verror.Internalf("SetSyncGroupConfig not yet implemented")
}

// GetMembersOfSyncGroup gets the Veyron names of the Stores that joined
// this SyncGroup.
func (t *thing) GetMembersOfSyncGroup(ctx ipc.ServerContext, name string) ([]string, error) {
	return nil, verror.Internalf("GetMembersOfSyncGroup not yet implemented")
}

////////////////////////////////////////
// Internals

type globStreamSenderAdapter struct {
	stream interface {
		Send(entry mttypes.MountEntry) error
	}
}

func (a *globStreamSenderAdapter) Send(item string) error {
	return a.stream.Send(mttypes.MountEntry{Name: item})
}

type globStreamAdapter struct {
	stream mounttable.GlobbableServiceGlobStream
}

func (a *globStreamAdapter) SendStream() interface {
	Send(item string) error
} {
	return &globStreamSenderAdapter{a.stream.SendStream()}
}
