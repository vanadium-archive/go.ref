package server

// This file defines object, which implements the server-side Object API from
// veyron2/services/store/service.vdl.

import (
	"veyron/services/store/memstore"

	"veyron2/ipc"
	"veyron2/query"
	"veyron2/services/mounttable"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/storage"
	"veyron2/vdl/vdlutil"
	"veyron2/verror"
)

type object struct {
	name   string // will never contain a transaction id
	obj    *memstore.Object
	tid    transactionID // may be nullTransactionID
	server *Server
}

var (
	errNotAValue      = verror.BadArgf("not a storage.Value")
	errNotAnAttribute = verror.BadArgf("not a storage.Attr")

	_ store.ObjectService = (*object)(nil)

	nullEntry store.Entry
	nullStat  store.Stat
)

func fillServiceStat(result *store.Stat, stat *storage.Stat) {
	result.ID = stat.ID
	result.MTimeNS = stat.MTime.UnixNano()
	result.Attrs = attrsToAnyData(stat.Attrs)
}

func makeServiceStat(stat *storage.Stat) store.Stat {
	if stat == nil {
		return nullStat
	}
	var result store.Stat
	fillServiceStat(&result, stat)
	return result
}

func makeServiceEntry(e *storage.Entry) store.Entry {
	if e == nil {
		return nullEntry
	}
	result := store.Entry{Value: e.Value}
	fillServiceStat(&result.Stat, &e.Stat)
	return result
}

func (o *object) String() string {
	return o.name
}

func (o *object) Attributes(arg string) map[string]string {
	return map[string]string{
		"health":     "ok",
		"servertype": o.String(),
	}
}

func attrsFromAnyData(attrs []vdlutil.Any) ([]storage.Attr, error) {
	typedAttrs := make([]storage.Attr, len(attrs))
	for i, x := range attrs {
		a, ok := x.(storage.Attr)
		if !ok {
			return nil, errNotAnAttribute
		}
		typedAttrs[i] = a
	}
	return typedAttrs, nil
}

func attrsToAnyData(attrs []storage.Attr) []vdlutil.Any {
	uattrs := make([]vdlutil.Any, len(attrs))
	for i, x := range attrs {
		uattrs[i] = x
	}
	return uattrs
}

// CreateTransaction creates a transaction.
func (o *object) CreateTransaction(ctx ipc.ServerContext, opts []vdlutil.Any) (string, error) {
	if o.tid != nullTransactionID {
		return "", errNestedTransaction
	}
	return o.server.createTransaction(ctx, o.name)
}

func (o *object) Commit(ctx ipc.ServerContext) error {
	return o.server.commitTransaction(ctx, o.tid)
}

func (o *object) Abort(ctx ipc.ServerContext) error {
	return o.server.abortTransaction(ctx, o.tid)
}

// Exists returns true iff the Entry has a value.
func (o *object) Exists(ctx ipc.ServerContext) (bool, error) {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return false, err
	}
	return o.obj.Exists(ctx.RemoteID(), t)
}

// Get returns the value for the Object.  The value returned is from the
// most recent mutation of the entry in the Transaction, or from the
// Transaction's snapshot if there is no mutation.
func (o *object) Get(ctx ipc.ServerContext) (store.Entry, error) {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return nullEntry, err
	}
	entry, err := o.obj.Get(ctx.RemoteID(), t)
	if err != nil {
		return nullEntry, err
	}
	return makeServiceEntry(entry), err
}

// Put modifies the value of the Object.
func (o *object) Put(ctx ipc.ServerContext, val vdlutil.Any) (store.Stat, error) {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return nullStat, err
	}
	stat, err := o.obj.Put(ctx.RemoteID(), t, interface{}(val))
	if err != nil {
		return nullStat, err
	}
	return makeServiceStat(stat), nil
}

// Remove removes the Object.
func (o *object) Remove(ctx ipc.ServerContext) error {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return err
	}
	return o.obj.Remove(ctx.RemoteID(), t)
}

// SetAttr changes the attributes of the entry, such as permissions and
// replication groups.  Attributes are associated with the value, not the
// path.
func (o *object) SetAttr(ctx ipc.ServerContext, attrs []vdlutil.Any) error {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return err
	}
	typedAttrs, err := attrsFromAnyData(attrs)
	if err != nil {
		return err
	}
	return o.obj.SetAttr(ctx.RemoteID(), t, typedAttrs...)
}

// Stat returns entry info.
func (o *object) Stat(ctx ipc.ServerContext) (store.Stat, error) {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return nullStat, err
	}
	stat, err := o.obj.Stat(ctx.RemoteID(), t)
	if err != nil {
		return nullStat, err
	}
	return makeServiceStat(stat), nil
}

// Query returns a sequence of objects that match the given query.
func (o *object) Query(ctx ipc.ServerContext, q query.Query, stream store.ObjectServiceQueryStream) error {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return err
	}
	it, err := o.obj.Query(ctx.RemoteID(), t, q)
	if err != nil {
		return err
	}
	for it.Next() {
		if err := stream.Send(*it.Get()); err != nil {
			it.Abort()
			return err
		}
	}
	return it.Err()
}

type globStreamAdapter struct {
	stream mounttable.GlobbableServiceGlobStream
}

func (a *globStreamAdapter) Send(item string) error {
	return a.stream.Send(mounttable.MountEntry{
		Name: item,
	})
}

// Glob streams a series of names that match the given pattern.
func (o *object) Glob(ctx ipc.ServerContext, pattern string, stream mounttable.GlobbableServiceGlobStream) error {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return err
	}
	it, err := o.obj.Glob(ctx.RemoteID(), t, pattern)
	if err != nil {
		return err
	}
	gsa := &globStreamAdapter{stream}
	for ; it.IsValid(); it.Next() {
		if ctx.IsClosed() {
			break
		}
		if err := gsa.Send(it.Name()); err != nil {
			return err
		}
	}
	return nil
}

// ChangeBatchStream is an interface for streaming responses of type ChangeBatch.
type ChangeBatchStream interface {
	// Send places the item onto the output stream, blocking if there is no buffer
	// space available.
	Send(watch.ChangeBatch) error
}

// entryTransformStream implements GlobWatcherServiceWatchGlobStream and
// QueryWatcherServiceWatchQueryStream. It wraps a ChangeBatchStream,
// transforming the value in each change from *storage.Entry to *store.Entry.
type entryTransformStream struct {
	delegate ChangeBatchStream
}

func (s *entryTransformStream) Send(cb watch.ChangeBatch) error {
	// Copy and transform the ChangeBatch.
	changes := cb.Changes
	changesCp := make([]watch.Change, len(changes))
	cbCp := watch.ChangeBatch{changesCp}
	for i, changeCp := range changes {
		if changes[i].Value != nil {
			entry := changes[i].Value.(*storage.Entry)
			serviceEntry := makeServiceEntry(entry)
			changeCp.Value = &serviceEntry
		}
		changesCp[i] = changeCp
	}
	return s.delegate.Send(cbCp)
}

// WatchGlob returns a stream of changes that match a pattern.
func (o *object) WatchGlob(ctx ipc.ServerContext, req watch.GlobRequest, stream watch.GlobWatcherServiceWatchGlobStream) error {
	path := storage.ParsePath(o.name)
	stream = &entryTransformStream{stream}
	return o.server.watcher.WatchGlob(ctx, path, req, stream)
}

// WatchQuery returns a stream of changes that satisfy a query.
func (o *object) WatchQuery(ctx ipc.ServerContext, req watch.QueryRequest, stream watch.QueryWatcherServiceWatchQueryStream) error {
	path := storage.ParsePath(o.name)
	stream = &entryTransformStream{stream}
	return o.server.watcher.WatchQuery(ctx, path, req, stream)
}
