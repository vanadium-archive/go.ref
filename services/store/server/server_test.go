package server

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	_ "veyron/lib/testutil" // initialize vlog
	watchtesting "veyron/services/store/memstore/testing"
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/storage"
	_ "veyron2/vlog"
	"veyron2/vom"
)

var (
	rootPublicID    security.PublicID = security.FakePublicID("root")
	rootName                          = fmt.Sprintf("%s", rootPublicID)
	blessedPublicId security.PublicID = security.FakePublicID("root/blessed")

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

func lookupObjectOrDie(s *Server, name string) *object {
	o, err := s.lookupObject(name)
	if err != nil {
		panic(err)
	}
	return o
}

// createTransaction creates a new transaction and returns its store-relative
// name.
func createTransaction(t *testing.T, s *Server, ctx ipc.ServerContext, name string) string {
	_, file, line, _ := runtime.Caller(1)
	tid, err := lookupObjectOrDie(s, name).CreateTransaction(ctx, nil)
	if err != nil {
		t.Fatalf("%s(%d): can't create transaction %s: %s", file, line, name, err)
	}
	return naming.Join(name, tid)
}

func TestLookupInvalidTransactionName(t *testing.T) {
	s, c := newServer()
	defer c()

	_, err := s.lookupObject("/$tid.bad/foo")
	if err == nil {
		t.Errorf("lookupObject should've failed, but didn't")
	}
}

func TestNestedTransactionError(t *testing.T) {
	s, c := newServer()
	defer c()
	tname := createTransaction(t, s, rootCtx, "/")
	if _, err := lookupObjectOrDie(s, tname).CreateTransaction(rootCtx, nil); err == nil {
		t.Fatalf("creating nested transaction at %s should've failed, but didn't", tname)
	}
	// Try again with a valid object in between the two $tid components;
	// CreateTransaction should still fail.
	lookupObjectOrDie(s, tname).Put(rootCtx, newValue())
	foo := naming.Join(tname, "foo")
	if _, err := lookupObjectOrDie(s, foo).CreateTransaction(rootCtx, nil); err == nil {
		t.Fatalf("creating nested transaction at %s should've failed, but didn't", foo)
	}
}

func TestPutGetRemoveRoot(t *testing.T) {
	s, c := newServer()
	defer c()

	testPutGetRemove(t, s, "/")
}

func TestPutGetRemoveChild(t *testing.T) {
	s, c := newServer()
	defer c()

	{
		// Create a root.
		name := "/"
		value := newValue()

		tobj1 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if _, err := tobj1.Put(rootCtx, value); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if err := tobj1.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}

		tobj2 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj2.Exists(rootCtx); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := tobj2.Get(rootCtx); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
		if err := tobj2.Abort(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	testPutGetRemove(t, s, "/Entries/a")
}

func testPutGetRemove(t *testing.T, s *Server, name string) {
	value := newValue()
	{
		// Check that the object does not exist.
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj.Exists(rootCtx); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := tobj.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}
	}

	{
		// Add the object.
		tobj1 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if _, err := tobj1.Put(rootCtx, value); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := tobj1.Exists(rootCtx); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := tobj1.Get(rootCtx); err != nil {
			t.Errorf("Object should exist: %s", err)
		}

		// Transactions are isolated.
		tobj2 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj2.Exists(rootCtx); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := tobj2.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}

		// Apply tobj1.
		if err := tobj1.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}

		// tobj2 is still isolated.
		if ok, err := tobj2.Exists(rootCtx); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := tobj2.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}

		// tobj3 observes the commit.
		tobj3 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj3.Exists(rootCtx); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := tobj3.Get(rootCtx); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
	}

	{
		// Remove the object.
		tobj1 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if err := tobj1.Remove(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := tobj1.Exists(rootCtx); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := tobj1.Get(rootCtx); v.Stat.ID.IsValid() || err == nil {
			t.Errorf("Object should not exist: %T, %v, %s", v, v, err)
		}

		// The removal is isolated.
		tobj2 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj2.Exists(rootCtx); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := tobj2.Get(rootCtx); err != nil {
			t.Errorf("Object should exist: %s", err)
		}

		// Apply tobj1.
		if err := tobj1.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}

		// The removal is isolated.
		if ok, err := tobj2.Exists(rootCtx); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := tobj2.Get(rootCtx); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
	}

	{
		// Check that the object does not exist.
		tobj1 := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj1.Exists(rootCtx); ok || err != nil {
			t.Errorf("Should not exist")
		}
		if v, err := tobj1.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}
	}
}

// TODO(sadovsky): Add test cases for committing and aborting an expired
// transaction. The client should get back errTransactionDoesNotExist.

func TestWatch(t *testing.T) {
	s, c := newServer()
	defer c()

	name1 := "/"
	value1 := "v1"
	var id1 storage.ID

	// Before the watch request has been made, commit a transaction that puts /.
	{
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name1))
		st, err := tobj.Put(rootCtx, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, s.Watch, req)

	// Check that watch detects the changes in the first transaction.
	{
		if !ws.Advance() {
			t.Error("Advance() failed: %v", ws.Err())
		}
		cb := ws.Value()
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	name2 := "/a"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /a.
	{
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name2))
		st, err := tobj.Put(rootCtx, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		if !ws.Advance() {
			t.Error("Advance() failed: %v", ws.Err())
		}
		cb := ws.Value()
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

	name1, name2 := "/", "/a"
	o1, o2 := lookupObjectOrDie(s, name1), lookupObjectOrDie(s, name2)

	// Before the watch request has been made, commit a transaction that puts /.
	{
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name1))
		st, err := tobj.Put(rootCtx, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start watch requests on / and /a.
	req := watch.GlobRequest{Pattern: "..."}
	ws1 := watchtesting.WatchGlob(rootPublicID, o1.WatchGlob, req)
	ws2 := watchtesting.WatchGlob(rootPublicID, o2.WatchGlob, req)

	// The watch on / should send a change on /.
	{
		if !ws1.Advance() {
			t.Error("Advance() failed: %v", ws1.Err())
		}
		cb := ws1.Value()
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
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name2))
		st, err := tobj.Put(rootCtx, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// The watch on / should send changes on / and /a.
	{
		if !ws1.Advance() {
			t.Error("Advance() failed: %v", ws1.Err())
		}
		cb := ws1.Value()
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
		if !ws2.Advance() {
			t.Error("Advance() failed: %v", ws2.Err())
		}
		cb := ws2.Value()
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

	name1 := "/"
	value1 := "v1"
	var id1 storage.ID

	// Before the watch request has been made, commit a transaction that puts /.
	{
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name1))
		st, err := tobj.Put(rootCtx, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := raw.Request{}
	ws := watchtesting.WatchRaw(rootPublicID, s.Watch, req)

	// Check that watch detects the changes in the first transaction.
	{
		if !ws.Advance() {
			t.Error("Advance() failed: %v", ws.Err())
		}
		cb := ws.Value()
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	name2 := "/a"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /a.
	{
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name2))
		st, err := tobj.Put(rootCtx, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		if !ws.Advance() {
			t.Error("Advance() failed: %v", ws.Err())
		}
		cb := ws.Value()
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
		tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, "/a"))
		if err := tobj.Remove(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if err := tobj.Commit(rootCtx); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the third transaction.
	{
		if !ws.Advance() {
			t.Error("Advance() failed: %v", ws.Err())
		}
		cb := ws.Value()
		changes := cb.Changes
		change := changes[0]
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		watchtesting.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	// Check that watch detects the garbage collection of /a.
	{
		if !ws.Advance() {
			t.Error("Advance() failed: %v", ws.Err())
		}
		cb := ws.Value()
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
	name := "/"
	value := newValue()

	// Create a transaction in the root's session.
	tobj := lookupObjectOrDie(s, createTransaction(t, s, rootCtx, name))

	// Check that the transaction cannot be accessed by the blessee.
	if _, err := tobj.Exists(blessedCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := tobj.Get(blessedCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := tobj.Put(blessedCtx, value); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := tobj.Remove(blessedCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := tobj.Abort(blessedCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := tobj.Commit(blessedCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}

	// Create a transaction in the blessee's session.
	tobj = lookupObjectOrDie(s, createTransaction(t, s, blessedCtx, name))

	// Check that the transaction cannot be accessed by the root.
	if _, err := tobj.Exists(rootCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := tobj.Get(rootCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if _, err := tobj.Put(rootCtx, value); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := tobj.Remove(rootCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := tobj.Abort(rootCtx); err != errPermissionDenied {
		t.Errorf("Unexpected error: %v", err)
	}
	if err := tobj.Commit(rootCtx); err != errPermissionDenied {
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

	// TODO(bprosnitz): Switch this to use just exported methods (using signature)
	// once signature stabilizes.
	d := NewStoreDispatcher(s, nil).(*storeDispatcher)
	for _, test := range tests {
		serv, err := d.lookupServer(test.name)
		if err != nil {
			t.Errorf("error looking up %s: %s", test.name, err)
		}
		if reflect.TypeOf(serv) != test.t {
			t.Errorf("error looking up %s. got %T, expected %v", test.name, serv, test.t)
		}
	}
}
