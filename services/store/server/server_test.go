package server

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"runtime"
	"testing"

	_ "veyron/lib/testutil" // initialize vlog
	storetest "veyron/services/store/memstore/testing"
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/watch/types"
	"veyron2/storage"
	_ "veyron2/vlog"
	"veyron2/vom"
)

var (
	rootPublicID    security.PublicID = security.FakePublicID("root")
	rootName                          = fmt.Sprintf("%s", rootPublicID)
	blessedPublicId security.PublicID = security.FakePublicID("root/blessed")
)

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

func lookupThingOrDie(s *Server, name string) *thing {
	t, err := s.lookupThing(name)
	if err != nil {
		panic(err)
	}
	return t
}

// createTransaction creates a new transaction and returns its name relative to
// the root of the store.
func createTransaction(t *testing.T, s *Server, ctx ipc.ServerContext, name string) string {
	_, file, line, _ := runtime.Caller(1)
	tid, err := lookupThingOrDie(s, name).NewTransaction(ctx, nil)
	if err != nil {
		t.Fatalf("%s(%d): can't create transaction %s: %s", file, line, name, err)
	}
	return naming.Join(name, tid)
}

func TestLookupInvalidTransactionName(t *testing.T) {
	s, c := newServer()
	defer c()

	_, err := s.lookupThing("/$tid.bad/foo")
	if err == nil {
		t.Fatalf("lookupThing should've failed, but didn't")
	}
}

func TestNestedTransactionError(t *testing.T) {
	rt.Init()
	s, c := newServer()
	defer c()

	rootCtx := storetest.NewFakeServerContext(rootPublicID)
	tname := createTransaction(t, s, rootCtx, "/")
	if _, err := lookupThingOrDie(s, tname).NewTransaction(rootCtx, nil); err == nil {
		t.Fatalf("creating nested transaction at %s should've failed, but didn't", tname)
	}
	// Try again with a valid object in between the two $tid components;
	// CreateTransaction should still fail.
	lookupThingOrDie(s, tname).Put(rootCtx, newValue())
	foo := naming.Join(tname, "foo")
	if _, err := lookupThingOrDie(s, foo).NewTransaction(rootCtx, nil); err == nil {
		t.Fatalf("creating nested transaction at %s should've failed, but didn't", foo)
	}
}

func TestPutGetRemoveObject(t *testing.T) {
	s, c := newServer()
	defer c()

	testPutGetRemove(t, s, "a")
}

func testPutGetRemove(t *testing.T, s *Server, name string) {
	rt.Init()
	rootCtx := storetest.NewFakeServerContext(rootPublicID)
	value := newValue()
	{
		// Check that the object does not exist.
		tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj.Exists(rootCtx); ok || err != nil {
			t.Fatalf("Should not exist: %s", err)
		}
		if v, err := tobj.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Fatalf("Should not exist: %v, %s", v, err)
		}
	}

	{
		// Add the object.
		tobj1 := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))
		if _, err := tobj1.Put(rootCtx, value); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if ok, err := tobj1.Exists(rootCtx); !ok || err != nil {
			t.Fatalf("Should exist: %s", err)
		}
		if _, err := tobj1.Get(rootCtx); err != nil {
			t.Fatalf("Object should exist: %s", err)
		}

		// Transactions are isolated.
		tobj2 := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj2.Exists(rootCtx); ok || err != nil {
			t.Fatalf("Should not exist: %s", err)
		}
		if v, err := tobj2.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Fatalf("Should not exist: %v, %s", v, err)
		}

		// Apply tobj1.
		if err := tobj1.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		// tobj2 is still isolated.
		if ok, err := tobj2.Exists(rootCtx); ok || err != nil {
			t.Fatalf("Should not exist: %s", err)
		}
		if v, err := tobj2.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Fatalf("Should not exist: %v, %s", v, err)
		}

		// tobj3 observes the commit.
		tobj3 := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj3.Exists(rootCtx); !ok || err != nil {
			t.Fatalf("Should exist: %s", err)
		}
		if _, err := tobj3.Get(rootCtx); err != nil {
			t.Fatalf("Object should exist: %s", err)
		}
	}

	{
		// Remove the object.
		tobj1 := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))
		if err := tobj1.Remove(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if ok, err := tobj1.Exists(rootCtx); ok || err != nil {
			t.Fatalf("Should not exist: %s", err)
		}
		if v, err := tobj1.Get(rootCtx); v.Stat.ID.IsValid() || err == nil {
			t.Fatalf("Object should not exist: %T, %v, %s", v, v, err)
		}

		// The removal is isolated.
		tobj2 := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj2.Exists(rootCtx); !ok || err != nil {
			t.Fatalf("Should exist: %s", err)
		}
		if _, err := tobj2.Get(rootCtx); err != nil {
			t.Fatalf("Object should exist: %s", err)
		}

		// Apply tobj1.
		if err := tobj1.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		// The removal is isolated.
		if ok, err := tobj2.Exists(rootCtx); !ok || err != nil {
			t.Fatalf("Should exist: %s", err)
		}
		if _, err := tobj2.Get(rootCtx); err != nil {
			t.Fatalf("Object should exist: %s", err)
		}
	}

	{
		// Check that the object does not exist.
		tobj1 := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))
		if ok, err := tobj1.Exists(rootCtx); ok || err != nil {
			t.Fatalf("Should not exist")
		}
		if v, err := tobj1.Get(rootCtx); v.Stat.ID.IsValid() && err == nil {
			t.Fatalf("Should not exist: %v, %s", v, err)
		}
	}
}

// TODO(sadovsky): Add more test cases for Commit/Abort:
//  - expired transaction: server should return errTransactionDoesNotExist
//  - no transaction: server should return errNoTransaction

func TestWatchGlob(t *testing.T) {
	rt.Init()
	rootCtx := storetest.NewFakeServerContext(rootPublicID)

	s, c := newServer()
	defer c()

	dirname, objname := "/a", "/a/b"
	dir, obj := lookupThingOrDie(s, dirname), lookupThingOrDie(s, objname)

	// Before the watch request has been made, commit a transaction that makes
	// directory /a.
	{
		tdir := lookupThingOrDie(s, createTransaction(t, s, rootCtx, dirname))
		err := tdir.Make(rootCtx)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if err := tdir.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}

	// Start watch requests on /a and /a/b.
	req := types.GlobRequest{Pattern: "..."}
	wdir := storetest.WatchGlob(rootPublicID, dir.WatchGlob, req)
	wobj := storetest.WatchGlob(rootPublicID, obj.WatchGlob, req)

	rStreamDir := wdir.RecvStream()
	rStreamObj := wobj.RecvStream()

	// The watch on /a should send a change on /a.
	{
		if !rStreamDir.Advance() {
			t.Fatalf("Advance() failed: %v", rStreamDir.Err())
		}
		change := rStreamDir.Value()
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
	}
	// The watch on /a/b should send no change. The first change it sends is
	// verified below.

	value := "v"
	var id storage.ID

	// Commit a second transaction that puts /a/b.
	{
		tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, objname))
		st, err := tobj.Put(rootCtx, value)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		id = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}

	// The watch on /a should send changes on /a and /a/b.
	{
		changes := []types.Change{}
		if !rStreamDir.Advance() {
			t.Fatalf("Advance() failed: %v", rStreamDir.Err())
		}
		change := rStreamDir.Value()
		changes = append(changes, change)
		if !change.Continued {
			t.Fatalf("Expected change to NOT be the last in this transaction")
		}
		if !rStreamDir.Advance() {
			t.Fatalf("Advance() failed: %v", rStreamDir.Err())
		}
		change = rStreamDir.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		storetest.ExpectEntryExistsNameOnly(t, changes, "a")
		storetest.ExpectEntryExists(t, changes, "a/b", id, value)
	}
	// The watch on /a/b should send a change on /a/b.
	{
		changes := []types.Change{}
		if !rStreamObj.Advance() {
			t.Fatalf("Advance() failed: %v", rStreamObj.Err())
		}
		change := rStreamObj.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		storetest.ExpectEntryExists(t, changes, "a/b", id, value)
	}
}

func TestRawWatch(t *testing.T) {
	rt.Init()
	rootCtx := storetest.NewFakeServerContext(rootPublicID)

	s, c := newServer()
	defer c()

	name1 := "/a"
	value1 := "v1"
	var id1 storage.ID

	// Before the watch request has been made, commit a transaction that puts /a.
	{
		tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name1))
		st, err := tobj.Put(rootCtx, value1)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := raw.Request{}
	ws := storetest.WatchRaw(rootPublicID, s.Watch, req)

	rStream := ws.RecvStream()
	// Check that watch detects the changes in the first transaction.
	{
		changes := []types.Change{}
		// First change is making the root dir (in server.go), second is updating
		// the root dir (adding a dir entry), third is putting /a.
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change := rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change = rStream.Value()
		changes = append(changes, change)
		if !change.Continued {
			t.Fatalf("Expected change to NOT be the last in this transaction")
		}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change = rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		storetest.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	name2 := "/b"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /b.
	{
		tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name2))
		st, err := tobj.Put(rootCtx, value2)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		changes := []types.Change{}
		// First change is updating the root dir (adding a dir entry), second is
		// putting /b.
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change := rStream.Value()
		changes = append(changes, change)
		if !change.Continued {
			t.Fatalf("Expected change to NOT be the last in this transaction")
		}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change = rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		// Note, we don't know the ID of the root dir so we can't check that it
		// exists in 'changes'.
		storetest.ExpectMutationExistsNoVersionCheck(t, changes, id2, value2)
	}
}

// Note, this test is identical to TestRawWatch up until the removal of /b.
func TestGarbageCollectionOnCommit(t *testing.T) {
	rt.Init()
	rootCtx := storetest.NewFakeServerContext(rootPublicID)

	s, c := newServer()
	defer c()

	name1 := "/a"
	value1 := "v1"
	var id1 storage.ID

	// Before the watch request has been made, commit a transaction that puts /a.
	{
		tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name1))
		st, err := tobj.Put(rootCtx, value1)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := raw.Request{}
	ws := storetest.WatchRaw(rootPublicID, s.Watch, req)

	rStream := ws.RecvStream()
	// Check that watch detects the changes in the first transaction.
	{
		changes := []types.Change{}
		// First change is making the root dir (in server.go), second is updating
		// the root dir (adding a dir entry), third is putting /a.
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change := rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change = rStream.Value()
		changes = append(changes, change)
		if !change.Continued {
			t.Fatalf("Expected change to NOT be the last in this transaction")
		}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change = rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		storetest.ExpectMutationExistsNoVersionCheck(t, changes, id1, value1)
	}

	name2 := "/b"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /b.
	{
		tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name2))
		st, err := tobj.Put(rootCtx, value2)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := tobj.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		changes := []types.Change{}
		// First change is updating the root dir (adding a dir entry), second is
		// putting /b.
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change := rStream.Value()
		changes = append(changes, change)
		if !change.Continued {
			t.Fatalf("Expected change to NOT be the last in this transaction")
		}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change = rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		// Note, we don't know the ID of the root dir so we can't check that it
		// exists in 'changes'.
		storetest.ExpectMutationExistsNoVersionCheck(t, changes, id2, value2)
	}

	// Commit a third transaction that removes /b.
	{
		tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name2))
		if err := tobj.Remove(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if err := tobj.Commit(rootCtx); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the third transaction.
	{
		changes := []types.Change{}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change := rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		// Note, we don't know the ID of the root dir so we can't check that it
		// exists in 'changes'.
	}

	// Check that watch detects the garbage collection of /b.
	{
		changes := []types.Change{}
		if !rStream.Advance() {
			t.Fatalf("Advance() failed: %v", rStream.Err())
		}
		change := rStream.Value()
		changes = append(changes, change)
		if change.Continued {
			t.Fatalf("Expected change to be the last in this transaction")
		}
		storetest.ExpectMutationDoesNotExistNoVersionCheck(t, changes, id2)
	}
}

func TestTransactionSecurity(t *testing.T) {
	rt.Init()
	rootCtx := storetest.NewFakeServerContext(rootPublicID)
	blessedCtx := storetest.NewFakeServerContext(blessedPublicId)

	s, c := newServer()
	defer c()

	// Create a root.
	name := "/"
	value := newValue()

	// Create a transaction in the root's session.
	tobj := lookupThingOrDie(s, createTransaction(t, s, rootCtx, name))

	// Check that the transaction cannot be accessed by the blessee.
	if _, err := tobj.Exists(blessedCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, err := tobj.Get(blessedCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, err := tobj.Put(blessedCtx, value); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if err := tobj.Remove(blessedCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if err := tobj.Abort(blessedCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if err := tobj.Commit(blessedCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Create a transaction in the blessee's session.
	tobj = lookupThingOrDie(s, createTransaction(t, s, blessedCtx, name))

	// Check that the transaction cannot be accessed by the root.
	if _, err := tobj.Exists(rootCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, err := tobj.Get(rootCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, err := tobj.Put(rootCtx, value); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if err := tobj.Remove(rootCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if err := tobj.Abort(rootCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
	if err := tobj.Commit(rootCtx); err != errPermissionDenied {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestStoreDispatcher(t *testing.T) {
	rawType := reflect.PtrTo(reflect.TypeOf(raw.ServerStubStore{}))
	thingType := reflect.PtrTo(reflect.TypeOf(ServerStubstoreThing{}))

	tests := []struct {
		name string
		t    reflect.Type
	}{
		{raw.RawStoreSuffix, rawType},
		{"a/b/" + raw.RawStoreSuffix, rawType},
		{"a/b/c" + raw.RawStoreSuffix, rawType},
		{"", thingType},
		{"a/b/", thingType},
		{"a/b/c", thingType},
	}

	s, c := newServer()
	defer c()

	// TODO(bprosnitz): Switch this to use just exported methods (using signature)
	// once signature stabilizes.
	d := NewStoreDispatcher(s, nil).(*storeDispatcher)
	for _, test := range tests {
		serv, err := d.lookupServer(test.name)
		if err != nil {
			t.Fatalf("error looking up %s: %s", test.name, err)
		}
		if reflect.TypeOf(serv) != test.t {
			t.Fatalf("error looking up %s. got %T, expected %v", test.name, serv, test.t)
		}
	}
}
