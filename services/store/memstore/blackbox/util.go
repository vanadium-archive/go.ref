package blackbox

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"testing"
	"time"

	"veyron/services/store/estore"
	"veyron/services/store/memstore"
	memwatch "veyron/services/store/memstore/watch"
	"veyron/services/store/service"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/watch"
	"veyron2/storage"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
	rootCtx      ipc.Context       = rootContext{}
)

type rootContext struct{}

func (rootContext) Server() ipc.Server {
	return nil
}

func (rootContext) Method() string {
	return ""
}

func (rootContext) Name() string {
	return ""
}

func (rootContext) Suffix() string {
	return ""
}

func (rootContext) Label() (l security.Label) {
	return
}

func (rootContext) CaveatDischarges() security.CaveatDischargeMap {
	return nil
}

func (rootContext) LocalID() security.PublicID {
	return rootPublicID
}

func (rootContext) RemoteID() security.PublicID {
	return rootPublicID
}

func (rootContext) Deadline() (t time.Time) {
	return
}

func (rootContext) IsClosed() bool {
	return false
}

func (rootContext) Closed() <-chan struct{} {
	return nil
}

func Get(t *testing.T, st *memstore.Store, tr storage.Transaction, path string) *storage.Entry {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e
}

func Put(t *testing.T, st *memstore.Store, tr storage.Transaction, path string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	stat, err := st.Bind(path).Put(rootPublicID, tr, v)
	if err != nil {
		t.Fatalf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	if stat != nil {
		return stat.ID
	}
	if id, ok := v.(storage.ID); ok {
		return id
	}
	t.Errorf("%s(%d): can't find id", file, line)
	return storage.ID{}
}

func Remove(t *testing.T, st *memstore.Store, tr storage.Transaction, path string) {
	_, file, line, _ := runtime.Caller(1)
	if err := st.Bind(path).Remove(rootPublicID, tr); err != nil {
		t.Fatalf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func Commit(t *testing.T, tr storage.Transaction) {
	if err := tr.Commit(); err != nil {
		t.Fatalf("Transaction aborted: %s", err)
	}
}

func ExpectExists(t *testing.T, st *memstore.Store, id storage.ID) {
	_, err := st.Bind(fmt.Sprintf("/uid/%s", id)).Get(rootPublicID, nil)
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): Expected value for ID: %x", file, line, id)
	}
}

func ExpectNotExists(t *testing.T, st *memstore.Store, id storage.ID) {
	x, err := st.Bind(fmt.Sprintf("/uid/%s", id)).Get(rootPublicID, nil)
	if err == nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): Unexpected value: %v", file, line, x)
	}
}

func GC(t *testing.T, st *memstore.Store) {
	if err := st.GC(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): can't gc: %s", file, line, err)
	}
}

func CreateStore(t *testing.T, dbSuffix string) (string, *memstore.Store, func()) {
	dbName, err := ioutil.TempDir(os.TempDir(), dbSuffix)
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	cleanup := func() {
		os.RemoveAll(dbName)
	}

	st, err := memstore.New(rootPublicID, dbName)
	if err != nil {
		cleanup()
		t.Fatalf("New() failed: %v", err)
	}

	return dbName, st, cleanup
}

func OpenStore(t *testing.T, dbName string) (*memstore.Store, func()) {
	st, err := memstore.New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	return st, func() {
		os.RemoveAll(dbName)
	}
}

func OpenWatch(t *testing.T, dbName string) (service.Watcher, func()) {
	w, err := memwatch.New(rootPublicID, dbName)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	return w, func() {
		w.Close()
	}
}

type testWatcherServiceWatchStream struct {
	c    chan<- watch.ChangeBatch
	done <-chan bool
}

func (s *testWatcherServiceWatchStream) Send(item watch.ChangeBatch) error {
	select {
	case s.c <- item:
		return nil
	case <-s.done:
		return errors.New("Send() failed")
	}
}

type testWatcherWatchStream struct {
	c    <-chan watch.ChangeBatch
	done chan bool
}

func (s *testWatcherWatchStream) Recv() (watch.ChangeBatch, error) {
	select {
	case cb, ok := <-s.c:
		if !ok {
			return cb, errors.New("Recv() failed")
		}
		return cb, nil
	case <-s.done:
		return watch.ChangeBatch{}, errors.New("Recv() failed: closed")
	}
}

func (s *testWatcherWatchStream) Finish() error {
	close(s.done)
	return nil
}

func (s *testWatcherWatchStream) Cancel() {
	close(s.done)
}

func Watch(t *testing.T, w service.Watcher, ctx ipc.Context, req watch.Request) watch.WatcherWatchStream {
	c := make(chan watch.ChangeBatch)
	done := make(chan bool)
	go func() {
		serviceStream := &testWatcherServiceWatchStream{c, done}
		// Check that io.EOF was returned on watch cancellation.
		if err := w.Watch(ctx, req, serviceStream); err != io.EOF {
			t.Fatalf("Watch() failed : %v", err)
		}
	}()
	return &testWatcherWatchStream{c, done}
}

func Mutations(changes []watch.Change) []estore.Mutation {
	mutations := make([]estore.Mutation, len(changes))
	for i, change := range changes {
		mutations[i] = *(change.Value.(*estore.Mutation))
	}
	return mutations
}
