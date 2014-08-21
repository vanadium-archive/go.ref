package server

// This file defines object, which implements the server-side Object API from
// veyron2/services/store/service.vdl.

import (
	"veyron/services/store/memstore"

	"veyron2/ipc"
	"veyron2/query"
	"veyron2/services/mounttable"
	"veyron2/services/mounttable/types"
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

	nullEntry storage.Entry
	nullStat  storage.Stat
)

func (o *object) String() string {
	return o.name
}

func (o *object) Attributes(arg string) map[string]string {
	return map[string]string{
		"health":     "ok",
		"servertype": o.String(),
	}
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
func (o *object) Get(ctx ipc.ServerContext) (storage.Entry, error) {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return nullEntry, err
	}
	entry, err := o.obj.Get(ctx.RemoteID(), t)
	if err != nil {
		return nullEntry, err
	}
	return *entry, err
}

// Put modifies the value of the Object.
func (o *object) Put(ctx ipc.ServerContext, val vdlutil.Any) (storage.Stat, error) {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return nullStat, err
	}
	s, err := o.obj.Put(ctx.RemoteID(), t, interface{}(val))
	if err != nil {
		return nullStat, err
	}
	return *s, err
}

// Remove removes the Object.
func (o *object) Remove(ctx ipc.ServerContext) error {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return err
	}
	return o.obj.Remove(ctx.RemoteID(), t)
}

// Stat returns entry info.
func (o *object) Stat(ctx ipc.ServerContext) (storage.Stat, error) {
	t, err := o.server.findTransaction(ctx, o.tid)
	if err != nil {
		return nullStat, err
	}
	s, err := o.obj.Stat(ctx.RemoteID(), t)
	if err != nil {
		return nullStat, err
	}
	return *s, err
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
		if err := stream.SendStream().Send(*it.Get()); err != nil {
			it.Abort()
			return err
		}
	}
	return it.Err()
}

type globStreamSenderAdapter struct {
	stream interface {
		Send(entry types.MountEntry) error
	}
}

func (a *globStreamSenderAdapter) Send(item string) error {
	return a.stream.Send(types.MountEntry{Name: item})
}

type globStreamAdapter struct {
	stream mounttable.GlobbableServiceGlobStream
}

func (a *globStreamAdapter) SendStream() interface {
	Send(item string) error
} {
	return &globStreamSenderAdapter{a.stream.SendStream()}
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
		if err := gsa.SendStream().Send(it.Name()); err != nil {
			return err
		}
	}
	return nil
}

// WatchGlob returns a stream of changes that match a pattern.
func (o *object) WatchGlob(ctx ipc.ServerContext, req watch.GlobRequest, stream watch.GlobWatcherServiceWatchGlobStream) error {
	return o.server.watcher.WatchGlob(ctx, storage.ParsePath(o.name), req, stream)
}

// WatchQuery returns a stream of changes that satisfy a query.
func (o *object) WatchQuery(ctx ipc.ServerContext, req watch.QueryRequest, stream watch.QueryWatcherServiceWatchQueryStream) error {
	return o.server.watcher.WatchQuery(ctx, storage.ParsePath(o.name), req, stream)
}
