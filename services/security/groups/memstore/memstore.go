// Package memstore provides a simple, in-memory implementation of server.Store.
// Since it's a prototype implementation, it doesn't bother with entry-level
// locking.
package memstore

import (
	"strconv"
	"sync"

	"v.io/core/veyron/services/security/groups/server"
)

type entry struct {
	v    interface{}
	etag int
}

type memstore struct {
	mu   sync.Mutex
	data map[string]*entry
}

var _ server.Store = (*memstore)(nil)

func New() server.Store {
	return &memstore{data: map[string]*entry{}}
}

func (st *memstore) Get(k string) (v interface{}, etag string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.data[k]
	if !ok {
		return nil, "", &server.ErrUnknownKey{Key: k}
	}
	return e.v, strconv.Itoa(e.etag), nil
}

func (st *memstore) Insert(k string, v interface{}) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.data[k]; ok {
		return &server.ErrKeyAlreadyExists{Key: k}
	}
	st.data[k] = &entry{v: v}
	return nil
}

func (st *memstore) Update(k string, v interface{}, etag string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.data[k]
	if !ok {
		return &server.ErrUnknownKey{Key: k}
	}
	if err := e.checkEtag(etag); err != nil {
		return err
	}
	e.v = v
	e.etag++
	return nil
}

func (st *memstore) Delete(k string, etag string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.data[k]
	if !ok {
		return &server.ErrUnknownKey{Key: k}
	}
	if err := e.checkEtag(etag); err != nil {
		return err
	}
	delete(st.data, k)
	return nil
}

func (e *entry) checkEtag(etag string) error {
	newEtag := strconv.Itoa(e.etag)
	if etag != newEtag {
		return &server.ErrBadEtag{}
	}
	return nil
}
