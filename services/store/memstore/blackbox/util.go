package blackbox

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"testing"
	"time"

	"veyron/services/store/memstore"
	memwatch "veyron/services/store/memstore/watch"
	"veyron/services/store/raw"
	"veyron/services/store/service"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/services/watch"
	"veyron2/storage"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
	rootCtx      ipc.ServerContext = rootContext{}
	nullMutation                   = raw.Mutation{}
)

// rootContext implements ipc.Context.
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

func (rootContext) Blessing() security.PublicID {
	return nil
}

func (rootContext) LocalEndpoint() naming.Endpoint {
	return nil
}

func (rootContext) RemoteEndpoint() naming.Endpoint {
	return nil
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

func Get(t *testing.T, st *memstore.Store, tr service.Transaction, path string) *storage.Entry {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e
}

func Put(t *testing.T, st *memstore.Store, tr service.Transaction, path string, v interface{}) storage.ID {
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

func Remove(t *testing.T, st *memstore.Store, tr service.Transaction, path string) {
	_, file, line, _ := runtime.Caller(1)
	if err := st.Bind(path).Remove(rootPublicID, tr); err != nil {
		t.Fatalf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func Commit(t *testing.T, tr service.Transaction) {
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

// putMutationsStream implements raw.StoreServicePutMutationsStream.
type putMutationsStream struct {
	mus   []raw.Mutation
	index int
}

func newPutMutationsStream(mus []raw.Mutation) raw.StoreServicePutMutationsStream {
	return &putMutationsStream{
		mus: mus,
	}
}

func (s *putMutationsStream) Recv() (raw.Mutation, error) {
	if s.index < len(s.mus) {
		index := s.index
		s.index++
		return s.mus[index], nil
	}
	return nullMutation, io.EOF
}

func PutMutations(t *testing.T, st *memstore.Store, mus []raw.Mutation) {
	stream := newPutMutationsStream(mus)
	if err := st.PutMutations(rootCtx, stream); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't put mutations %s: %s", file, line, mus, err)
	}
}

func Mutations(changes []watch.Change) []raw.Mutation {
	mutations := make([]raw.Mutation, len(changes))
	for i, change := range changes {
		mutations[i] = *(change.Value.(*raw.Mutation))
	}
	return mutations
}
