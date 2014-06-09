package memstore

import (
	iquery "veyron/services/store/memstore/query"
	"veyron/services/store/service"

	"veyron2/query"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/verror"
)

// object is a binding to a store value.  This is currently represented as a
// path.  This means that in different transactions, the object may refer to
// different values with different store.IDs.
//
// TODO(jyh): Perhaps a more sensible alternative is to resolve the path at
// Bind time, and have the object then refer to the value by storage.ID.  However,
// there are two problems with that,
//
//    - We can't resolve the path for objects that don't yet exist.
//    - Bind is transactionless, so the path resolution wouldn't be consistent.
//
// Discuss and decide on a semantics.
type object struct {
	path  storage.PathName
	store *Store
}

// Bind returns an Object representing a value in the store.  The value need not
// exist; the Put method can be used to add the value if it doesn't already
// exist.
func (st *Store) Bind(path string) service.Object {
	return &object{path: storage.ParsePath(path), store: st}
}

// Exists returns true iff the object has a value in the current transaction.
func (o *object) Exists(pid security.PublicID, trans service.Transaction) (bool, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return false, err
	}
	v, err := tr.snapshot.Get(pid, o.path)
	ok := v != nil && err == nil
	return ok, nil
}

// Get returns the value for an object.
func (o *object) Get(pid security.PublicID, trans service.Transaction) (*storage.Entry, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	return tr.snapshot.Get(pid, o.path)
}

// Put updates the value for an object.
func (o *object) Put(pid security.PublicID, trans service.Transaction, v interface{}) (*storage.Stat, error) {
	tr, commit, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	st, err := tr.snapshot.Put(pid, o.path, v)
	if err != nil {
		return nil, err
	}
	if commit {
		err = tr.Commit()
	}
	return st, err
}

// Remove removes the value for an object.
func (o *object) Remove(pid security.PublicID, trans service.Transaction) error {
	tr, commit, err := o.store.getTransaction(trans)
	if err != nil {
		return err
	}
	if err := tr.snapshot.Remove(pid, o.path); err != nil {
		return err
	}
	if commit {
		return tr.Commit()
	}
	return nil
}

// SetAttr changes the attributes of the entry, such as permissions and
// replication groups.  Attributes are associated with the value, not the
// path.
func (o *object) SetAttr(pid security.PublicID, tr service.Transaction, attrs ...storage.Attr) error {
	return verror.Internalf("SetAttr not yet implemented")
}

// Stat returns entry info.
func (o *object) Stat(pid security.PublicID, tr service.Transaction) (*storage.Stat, error) {
	return nil, verror.Internalf("Stat not yet implemented")
}

// Query returns entries matching the given query.
func (o *object) Query(pid security.PublicID, trans service.Transaction, q query.Query) (service.QueryStream, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	stream := iquery.Eval(tr.snapshot.GetSnapshot(), pid, o.path, q)
	return stream, nil
}

// Glob returns the sequence of names that match the given pattern.
func (o *object) Glob(pid security.PublicID, trans service.Transaction, pattern string) (service.GlobStream, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	return iquery.Glob(tr.snapshot.GetSnapshot(), pid, o.path, pattern)
}
