package memstore

import (
	iquery "veyron/services/store/memstore/query"
	"veyron/services/store/service"

	"veyron2/query"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/verror"
)

// Object is a binding to a store value.  This is currently represented as a
// path.  This means that in different transactions, the object may refer to
// different values with different store.IDs.
type Object struct {
	path  storage.PathName
	store *Store
}

// Exists returns true iff the object has a value in the current transaction.
func (o *Object) Exists(pid security.PublicID, trans service.Transaction) (bool, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return false, err
	}
	v, err := tr.snapshot.Get(pid, o.path)
	ok := v != nil && err == nil
	return ok, nil
}

// Get returns the value for an object.
func (o *Object) Get(pid security.PublicID, trans service.Transaction) (*storage.Entry, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	return tr.snapshot.Get(pid, o.path)
}

// Put updates the value for an object.
func (o *Object) Put(pid security.PublicID, trans service.Transaction, v interface{}) (*storage.Stat, error) {
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
func (o *Object) Remove(pid security.PublicID, trans service.Transaction) error {
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
func (o *Object) SetAttr(pid security.PublicID, tr service.Transaction, attrs ...storage.Attr) error {
	return verror.Internalf("SetAttr not yet implemented")
}

// Stat returns entry info.
func (o *Object) Stat(pid security.PublicID, tr service.Transaction) (*storage.Stat, error) {
	return nil, verror.Internalf("Stat not yet implemented")
}

// Query returns entries matching the given query.
func (o *Object) Query(pid security.PublicID, trans service.Transaction, q query.Query) (iquery.QueryStream, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	stream := iquery.Eval(tr.snapshot.GetSnapshot(), pid, o.path, q)
	return stream, nil
}

// Glob returns names that match the given pattern.
func (o *Object) Glob(pid security.PublicID, trans service.Transaction, pattern string) (iquery.GlobStream, error) {
	tr, _, err := o.store.getTransaction(trans)
	if err != nil {
		return nil, err
	}
	return iquery.Glob(tr.snapshot.GetSnapshot(), pid, o.path, pattern)
}
