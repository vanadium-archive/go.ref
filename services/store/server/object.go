package server

import (
	"errors"

	"veyron/services/store/service"

	"veyron2/idl"
	"veyron2/ipc"
	"veyron2/services/mounttable"
	"veyron2/services/store"
	"veyron2/storage"
)

type object struct {
	name   string
	obj    service.Object
	server *Server
}

var (
	errNotAValue      = errors.New("not a storage.Value")
	errNotAnAttribute = errors.New("not a storage.Attr")

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

func attrsFromAnyData(attrs []idl.AnyData) ([]storage.Attr, error) {
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

func attrsToAnyData(attrs []storage.Attr) []idl.AnyData {
	uattrs := make([]idl.AnyData, len(attrs))
	for i, x := range attrs {
		uattrs[i] = x
	}
	return uattrs
}

// Exists returns true iff the Entry has a value.
func (o *object) Exists(ctx ipc.Context, tid store.TransactionID) (bool, error) {
	t, ok := o.server.findTransaction(tid)
	if !ok {
		return false, errTransactionDoesNotExist
	}
	return o.obj.Exists(ctx.RemoteID(), t)
}

// Get returns the value for the Object.  The value returned is from the
// most recent mutation of the entry in the Transaction, or from the
// Transaction's snapshot if there is no mutation.
func (o *object) Get(ctx ipc.Context, tid store.TransactionID) (store.Entry, error) {
	t, ok := o.server.findTransaction(tid)
	if !ok {
		return nullEntry, errTransactionDoesNotExist
	}
	entry, err := o.obj.Get(ctx.RemoteID(), t)
	if err != nil {
		return nullEntry, err
	}
	return makeServiceEntry(entry), err
}

// Put modifies the value of the Object.
func (o *object) Put(ctx ipc.Context, tid store.TransactionID, val idl.AnyData) (store.Stat, error) {
	t, ok := o.server.findTransaction(tid)
	if !ok {
		return nullStat, errTransactionDoesNotExist
	}
	stat, err := o.obj.Put(ctx.RemoteID(), t, interface{}(val))
	if err != nil {
		return nullStat, err
	}
	return makeServiceStat(stat), nil
}

// Remove removes the Object.
func (o *object) Remove(ctx ipc.Context, tid store.TransactionID) error {
	t, ok := o.server.findTransaction(tid)
	if !ok {
		return errTransactionDoesNotExist
	}
	return o.obj.Remove(ctx.RemoteID(), t)
}

// SetAttr changes the attributes of the entry, such as permissions and
// replication groups.  Attributes are associated with the value, not the
// path.
func (o *object) SetAttr(ctx ipc.Context, tid store.TransactionID, attrs []idl.AnyData) error {
	t, ok := o.server.findTransaction(tid)
	if !ok {
		return errTransactionDoesNotExist
	}
	typedAttrs, err := attrsFromAnyData(attrs)
	if err != nil {
		return err
	}
	return o.obj.SetAttr(ctx.RemoteID(), t, typedAttrs...)
}

// Stat returns entry info.
func (o *object) Stat(ctx ipc.Context, tid store.TransactionID) (store.Stat, error) {
	t, ok := o.server.findTransaction(tid)
	if !ok {
		return nullStat, errTransactionDoesNotExist
	}
	stat, err := o.obj.Stat(ctx.RemoteID(), t)
	if err != nil {
		return nullStat, err
	}
	return makeServiceStat(stat), nil
}

type globStreamAdapter struct {
	stream mounttable.GlobableServiceGlobStream
}

func (a *globStreamAdapter) Send(item string) error {
	return a.stream.Send(mounttable.MountEntry{
		Name: item,
	})
}

// Glob streams a series of names that match the given pattern.
func (o *object) Glob(ctx ipc.Context, pattern string, stream mounttable.GlobableServiceGlobStream) error {
	return o.GlobT(ctx, nullTransactionID, pattern, &globStreamAdapter{stream})
}

// Glob streams a series of names that match the given pattern.
func (o *object) GlobT(ctx ipc.Context, tid store.TransactionID, pattern string, stream store.ObjectServiceGlobTStream) error {
	t, ok := o.server.findTransaction(tid)
	if !ok {
		return errTransactionDoesNotExist
	}
	it, err := o.obj.Glob(ctx.RemoteID(), t, pattern)
	if err != nil {
		return err
	}
	for ; it.IsValid(); it.Next() {
		if ctx.IsClosed() {
			break
		}
		if err := stream.Send(it.Name()); err != nil {
			return err
		}
	}
	return nil
}
