package memstore

import (
	iquery "veyron/services/store/memstore/query"
	"veyron/services/store/service"

	"veyron2/security"
	"veyron2/storage"
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
func (o *object) Exists(pid security.PublicID, trans storage.Transaction) (bool, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return false, err
	}
	v, err := tr.snapshot.Get(pid, o.path)
	ok := v != nil && err == nil
	return ok, nil
}

// Get returns the value for an object.
func (o *object) Get(pid security.PublicID, trans storage.Transaction) (*storage.Entry, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	return tr.snapshot.Get(pid, o.path)
}

// Put updates the value for an object.
func (o *object) Put(pid security.PublicID, trans storage.Transaction, v interface{}) (*storage.Stat, error) {
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
func (o *object) Remove(pid security.PublicID, trans storage.Transaction) error {
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

func (o *object) SetAttr(pid security.PublicID, tr storage.Transaction, attrs ...storage.Attr) error {
	panic("not implemented")
}

func (o *object) Stat(pid security.PublicID, tr storage.Transaction) (*storage.Stat, error) {
	panic("not implemented")
}

func (o *object) Glob(pid security.PublicID, trans storage.Transaction, pattern string) (service.GlobStream, error) {
	tr, commit, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	stream, err := iquery.Glob(tr.snapshot.GetSnapshot(), pid, o.path, pattern)
	if err != nil {
		return stream, err
	}
	if commit {
		err = tr.Commit()
	}
	return stream, err
}
