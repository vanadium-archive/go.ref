package server

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"testing"
	"time"

	watchtesting "veyron/services/store/memstore/watch/testing"
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/storage"
	"veyron2/vom"
)

var (
	rootPublicID    security.PublicID = security.FakePublicID("root")
	rootName                          = fmt.Sprintf("%s", rootPublicID)
	blessedPublicId security.PublicID = security.FakePublicID("root/blessed")

	nextTransactionID store.TransactionID = 1

	rootCtx    ipc.ServerContext = &testContext{rootPublicID}
	blessedCtx ipc.ServerContext = &testContext{blessedPublicId}
)

type testContext struct {
	id security.PublicID
}

func (*testContext) Server() ipc.Server {
	return nil
}

func (*testContext) Method() string {
	return ""
}

func (*testContext) Name() string {
	return ""
}

func (*testContext) Suffix() string {
	return ""
}

func (*testContext) Label() (l security.Label) {
	return
}

func (*testContext) CaveatDischarges() security.CaveatDischargeMap {
	return nil
}

func (ctx *testContext) LocalID() security.PublicID {
	return ctx.id
}

func (ctx *testContext) RemoteID() security.PublicID {
	return ctx.id
}

func (*testContext) Blessing() security.PublicID {
	return nil
}

func (*testContext) LocalEndpoint() naming.Endpoint {
	return nil
}

func (*testContext) RemoteEndpoint() naming.Endpoint {
	return nil
}

func (*testContext) Deadline() (t time.Time) {
	return
}

func (testContext) IsClosed() bool {
	return false
}

func (testContext) Closed() <-chan struct{} {
	return nil
}

// Dir is a simple directory.
type Dir struct {
	Entries map[string]storage.ID
}

func init() {
	vom.Register(&Dir{})
}

func newValue() interface{} {
	return &Dir{}
}

func newTransaction() store.TransactionID {
	nextTransactionID++
	return nextTransactionID
}

func closeTest(config ServerConfig, s *Server) {
	s.Close()
	os.Remove(config.DBName)
}

func newServer() (*Server, func()) {
	dbName, err := ioutil.TempDir(os.TempDir(), "test_server_test.db")
	if err != nil {
		log.Fatal("ioutil.TempDir() failed: ", err)
	}
	config := ServerConfig{
		Admin:  rootPublicID,
		DBName: dbName,
	}
	s, err := New(config)
	if err != nil {
		log.Fatal("server.New() failed: ", err)
	}
	closer := func() { closeTest(config, s) }
	return s, closer
}

func TestPutGetRemoveRoot(t *testing.T) {
	s, c := newServer()
	defer c()

	o := s.lookupObject("/")
	testPutGetRemove(t, s, o)
}

func TestPutGetRemoveChild(t *testing.T) {
	s, c := newServer()
	defer c()

	{
		// Create a root.
		o := s.lookupObject("/")
		value := newValue()
		tr1 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr1, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if _, err := o.Put(rootCtx, tr1, value); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if err := s.Commit(rootCtx, tr1); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}

		tr2 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr2, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr2); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootCtx, tr2); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
		if err := s.Abort(rootCtx, tr2); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	o := s.lookupObject("/Entries/a")
	testPutGetRemove(t, s, o)
}

func testPutGetRemove(t *testing.T, s *Server, o *object) {
	value := newValue()
	{
		// Check that the object does not exist.
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := o.Get(rootCtx, tr); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}
	}

	{
		// Add the object.
		tr1 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr1, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if _, err := o.Put(rootCtx, tr1, value); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr1); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootCtx, tr1); err != nil {
			t.Errorf("Object should exist: %s", err)
		}

		// Transactions are isolated.
		tr2 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr2, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr2); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := o.Get(rootCtx, tr2); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}

		// Apply tr1.
		if err := s.Commit(rootCtx, tr1); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}

		// tr2 is still isolated.
		if ok, err := o.Exists(rootCtx, tr2); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := o.Get(rootCtx, tr2); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}

		// tr3 observes the commit.
		tr3 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr3, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr3); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootCtx, tr3); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
	}

	{
		// Remove the object.
		tr1 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr1, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if err := o.Remove(rootCtx, tr1); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr1); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := o.Get(rootCtx, tr1); v.Stat.ID.IsValid() || err == nil {
			t.Errorf("Object should not exist: %T, %v, %s", v, v, err)
		}

		// The removal is isolated.
		tr2 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr2, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr2); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootCtx, tr2); err != nil {
			t.Errorf("Object should exist: %s", err)
		}

		// Apply tr1.
		if err := s.Commit(rootCtx, tr1); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}

		// The removal is isolated.
		if ok, err := o.Exists(rootCtx, tr2); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootCtx, tr2); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
	}

	{
		// Check that the object does not exist.
		tr1 := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr1, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr1); ok || err != nil {
			t.Errorf("Should not exist")
		}
		if v, err := o.Get(rootCtx, tr1); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}
	}
}

func TestNilTransaction(t *testing.T) {
	s, c := newServer()
	defer c()

	if err := s.Commit(rootCtx, nullTransactionID); err != errTransactionDoesNotExist {
		t.Errorf("Unexpected error: %v", err)
	}

	if err := s.Abort(rootCtx, nullTransactionID); err != errTransactionDoesNotExist {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestWatch(t *testing.T) {
	s, c := newServer()
	defer c()

	path1 := "/"
	value1 := "v1"
	var id1 storage.ID

	// Before the watch request has been made, commit a transaction that puts /.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path1)
		st, err := o.Put(rootCtx, tr, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := s.Commit(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, s.Watch, req)

	// Check that watch detects the changes in the first transaction.
	{
		cb, err := ws.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	path2 := "/a"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /a.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path2)
		st, err := o.Put(rootCtx, tr, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := s.Commit(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		cb, err := ws.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if !change.Continued {
			t.Error("Expected change to continue the transaction")
		}
		change = changes[1]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id2, value2)
	}
}

func TestWatchGlob(t *testing.T) {
	s, c := newServer()
	defer c()

	value1 := "v1"
	var id1 storage.ID

	o1 := s.lookupObject("/")
	o2 := s.lookupObject("/a")

	// Before the watch request has been made, commit a transaction that puts /.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		st, err := o1.Put(rootCtx, tr, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := s.Commit(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start watch requests on / and /a.
	req := watch.GlobRequest{Pattern: "..."}
	ws1 := watchtesting.WatchGlob(rootPublicID, o1.WatchGlob, req)
	ws2 := watchtesting.WatchGlob(rootPublicID, o2.WatchGlob, req)

	// The watch on / should send a change on /.
	{
		cb, err := ws1.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectServiceEntryExists(t, changes, "", id1, value1)
	}
	// The watch on /a should send no change. The first change it sends is
	// verified below.

	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /a.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		st, err := o2.Put(rootCtx, tr, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := s.Commit(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// The watch on / should send changes on / and /a.
	{
		cb, err := ws1.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if !change.Continued {
			t.Error("Expected change to continue the transaction")
		}
		change = changes[1]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectServiceEntryExists(t, changes, "", id1, value1)
		watchtesting.ExpectServiceEntryExists(t, changes, "a", id2, value2)
	}
	// The watch on /a should send a change on /a.
	{
		cb, err := ws2.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectServiceEntryExists(t, changes, "a", id2, value2)
	}
}

func TestGarbageCollectionOnCommit(t *testing.T) {
	s, c := newServer()
	defer c()

	path1 := "/"
	value1 := "v1"
	var id1 storage.ID

	// Before the watch request has been made, commit a transaction that puts /.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path1)
		st, err := o.Put(rootCtx, tr, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := s.Commit(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, s.Watch, req)

	// Check that watch detects the changes in the first transaction.
	{
		cb, err := ws.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	path2 := "/a"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /a.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path2)
		st, err := o.Put(rootCtx, tr, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := s.Commit(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		cb, err := ws.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if !change.Continued {
			t.Error("Expected change to continue the transaction")
		}
		change = changes[1]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id2, value2)
	}

	// Commit a third transaction that removes /a.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject("/a")
		if err := o.Remove(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if err := s.Commit(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the third transaction.
	{
		cb, err := ws.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	// Check that watch detects the garbage collection of /a.
	{
		cb, err := ws.Recv()
		if err != nil {
			t.Error("Recv() failed: %v", err)
		}
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationDoesNotExistNoVersionCheck(t, changes, id2)
	}
}

func TestTransactionSecurity(t *testing.T) {
	s, c := newServer()
	defer c()

	// Create a root.
	o := s.lookupObject("/")
	value := newValue()

	// Create a transaction in the root's session.
	tr := newTransaction()
	if err := s.CreateTransaction(rootCtx, tr, nil); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	// Check that the transaction cannot be created or accessed by the blessee.
	if err := s.CreateTransaction(blessedCtx, tr, nil); err != errTransactionAlreadyExists {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := o.Exists(blessedCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := o.Get(blessedCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := o.Put(blessedCtx, tr, value); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := o.Remove(blessedCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := s.Abort(blessedCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := s.Commit(blessedCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}

	// Create a transaction in the blessee's session.
	tr = newTransaction()
	if err := s.CreateTransaction(blessedCtx, tr, nil); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	// Check that the transaction cannot be created or accessed by the root.
	if err := s.CreateTransaction(rootCtx, tr, nil); err != errTransactionAlreadyExists {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := o.Exists(rootCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := o.Get(rootCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := o.Put(rootCtx, tr, value); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := o.Remove(rootCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := s.Abort(rootCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := s.Commit(rootCtx, tr); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestStoreDispatcher(t *testing.T) {
	storeType := reflect.PtrTo(reflect.TypeOf(store.ServerStubStore{}))
	rawType := reflect.PtrTo(reflect.TypeOf(raw.ServerStubStore{}))
	objectType := reflect.PtrTo(reflect.TypeOf(store.ServerStubObject{}))

	tests := []struct {
		name string
		t    reflect.Type
	}{
		{store.StoreSuffix, storeType},
		{"a/b/" + store.StoreSuffix, storeType},
		{"a/b/c" + store.StoreSuffix, storeType},
		{raw.RawStoreSuffix, rawType},
		{"a/b/" + raw.RawStoreSuffix, rawType},
		{"a/b/c" + raw.RawStoreSuffix, rawType},
		{"", objectType},
		{"a/b/", objectType},
		{"a/b/c", objectType},
	}

	s, c := newServer()
	defer c()

	// TODO(bprosnitz) Switch this to use just exported methods (using signature) once signature stabilizes.
	d := NewStoreDispatcher(s, nil).(*storeDispatcher)
	for _, test := range tests {
		srvr := d.lookupServer(test.name)
		if reflect.TypeOf(srvr) != test.t {
			t.Errorf("error looking up %s. got %T, expected %v", test.name, srvr, test.t)
		}
	}
}
