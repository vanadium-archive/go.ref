package server

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/storage"
	"veyron2/vom"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
	rootName                       = rootPublicID.Names()[0]

	nextTransactionID store.TransactionID = 1

	rootCtx ipc.Context = &rootContext{}
)

type rootContext struct{}

func (*rootContext) Server() ipc.Server {
	return nil
}

func (*rootContext) Method() string {
	return ""
}

func (*rootContext) Name() string {
	return ""
}

func (*rootContext) Suffix() string {
	return ""
}

func (*rootContext) Label() (l security.Label) {
	return
}

func (*rootContext) CaveatDischarges() security.CaveatDischargeMap {
	return nil
}

func (*rootContext) LocalID() security.PublicID {
	return rootPublicID
}

func (*rootContext) RemoteID() security.PublicID {
	return rootPublicID
}

func (*rootContext) LocalAddr() net.Addr {
	return nil
}

func (*rootContext) RemoteAddr() net.Addr {
	return nil
}

func (*rootContext) Deadline() (t time.Time) {
	return
}

func (rootContext) IsClosed() bool {
	return false
}

func (rootContext) Closed() <-chan struct{} {
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

type watchResult struct {
	changes chan watch.Change
	err     error
}

func (wr *watchResult) Send(cb watch.ChangeBatch) error {
	for _, change := range cb.Changes {
		wr.changes <- change
	}
	return nil
}

// doWatch executes a watch request and returns a new watchResult.
// Change events may be received on the channel "changes".
// Once "changes" is closed, any error that occurred is stored to "err".
func doWatch(s *Server, ctx ipc.Context, req watch.Request) *watchResult {
	wr := &watchResult{changes: make(chan watch.Change)}
	go func() {
		defer close(wr.changes)
		if err := s.Watch(ctx, req, wr); err != nil {
			wr.err = err
		}
	}()
	return wr
}

func expectExists(t *testing.T, changes []watch.Change, id storage.ID, value string) {
	change := findChange(t, changes, id)
	if change.State != watch.Exists {
		t.Fatalf("Expected id to exist: %v", id)
	}
	cv := change.Value.(*raw.Mutation)
	if cv.Value != value {
		t.Fatalf("Expected Value to be: %v, but was: %v", value, cv.Value)
	}
}

func expectDoesNotExist(t *testing.T, changes []watch.Change, id storage.ID) {
	change := findChange(t, changes, id)
	if change.State != watch.DoesNotExist {
		t.Fatalf("Expected id to not exist: %v", id)
	}
}

func findChange(t *testing.T, changes []watch.Change, id storage.ID) watch.Change {
	for _, change := range changes {
		cv, ok := change.Value.(*raw.Mutation)
		if !ok {
			t.Fatal("Expected a Mutation")
		}
		if cv.ID == id {
			return change
		}
	}
	t.Fatalf("Expected a change for id: %v", id)
	panic("should not reach here")
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
		if err := s.CreateTransaction(nil, tr1, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if _, err := o.Put(rootCtx, tr1, value); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if err := s.Commit(nil, tr1); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}

		tr2 := newTransaction()
		if err := s.CreateTransaction(nil, tr2, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr2); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootCtx, tr2); err != nil {
			t.Errorf("Object should exist: %s", err)
		}
		if err := s.Abort(nil, tr2); err != nil {
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
		if err := s.CreateTransaction(nil, tr, nil); err != nil {
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
		if err := s.CreateTransaction(nil, tr1, nil); err != nil {
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
		if err := s.CreateTransaction(nil, tr2, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr2); ok || err != nil {
			t.Errorf("Should not exist: %s", err)
		}
		if v, err := o.Get(rootCtx, tr2); v.Stat.ID.IsValid() && err == nil {
			t.Errorf("Should not exist: %v, %s", v, err)
		}

		// Apply tr1.
		if err := s.Commit(nil, tr1); err != nil {
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
		if err := s.CreateTransaction(nil, tr3, nil); err != nil {
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
		if err := s.CreateTransaction(nil, tr1, nil); err != nil {
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
		if err := s.CreateTransaction(nil, tr2, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if ok, err := o.Exists(rootCtx, tr2); !ok || err != nil {
			t.Errorf("Should exist: %s", err)
		}
		if _, err := o.Get(rootCtx, tr2); err != nil {
			t.Errorf("Object should exist: %s", err)
		}

		// Apply tr1.
		if err := s.Commit(nil, tr1); err != nil {
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
		if err := s.CreateTransaction(nil, tr1, nil); err != nil {
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

func TestWatch(t *testing.T) {
	s, c := newServer()
	defer c()

	path1 := "/"
	value1 := "v1"
	var id1 storage.ID

	// Before the watch request has been made, commit a transaction that puts /.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(nil, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path1)
		st, err := o.Put(rootCtx, tr, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := s.Commit(nil, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := watch.Request{}
	wr := doWatch(s, rootCtx, req)

	// Check that watch detects the changes in the first transaction.
	{
		change, ok := <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		expectExists(t, []watch.Change{change}, id1, value1)
	}

	path2 := "/a"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /a.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(nil, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path2)
		st, err := o.Put(rootCtx, tr, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := s.Commit(nil, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		changes := make([]watch.Change, 0, 0)
		change, ok := <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if !change.Continued {
			t.Error("Expected change to continue the transaction")
		}
		changes = append(changes, change)
		change, ok = <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		changes = append(changes, change)
		expectExists(t, changes, id1, value1)
		expectExists(t, changes, id2, value2)
	}

	// Check that no errors were encountered.
	if err := wr.err; err != nil {
		t.Errorf("Unexpected error: %s", err)
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
		if err := s.CreateTransaction(nil, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path1)
		st, err := o.Put(rootCtx, tr, value1)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id1 = st.ID
		if err := s.Commit(nil, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Start a watch request.
	req := watch.Request{}
	wr := doWatch(s, rootCtx, req)

	// Check that watch detects the changes in the first transaction.
	{
		change, ok := <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		expectExists(t, []watch.Change{change}, id1, value1)
	}

	path2 := "/a"
	value2 := "v2"
	var id2 storage.ID

	// Commit a second transaction that puts /a.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(nil, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject(path2)
		st, err := o.Put(rootCtx, tr, value2)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		id2 = st.ID
		if err := s.Commit(nil, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the second transaction.
	{
		changes := make([]watch.Change, 0, 0)
		change, ok := <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if !change.Continued {
			t.Error("Expected change to continue the transaction")
		}
		changes = append(changes, change)
		change, ok = <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		changes = append(changes, change)
		expectExists(t, changes, id1, value1)
		expectExists(t, changes, id2, value2)
	}

	// Commit a third transaction that removes /a.
	{
		tr := newTransaction()
		if err := s.CreateTransaction(nil, tr, nil); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		o := s.lookupObject("/a")
		if err := o.Remove(rootCtx, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		if err := s.Commit(nil, tr); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
	}

	// Check that watch detects the changes in the third transaction.
	{
		changes := make([]watch.Change, 0, 0)
		change, ok := <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		changes = append(changes, change)
		expectExists(t, changes, id1, value1)
	}

	// Check that watch detects the garbage collection of /a.
	{
		changes := make([]watch.Change, 0, 0)
		change, ok := <-wr.changes
		if !ok {
			t.Error("Expected a change.")
		}
		if change.Continued {
			t.Error("Expected change to be the last in this transaction")
		}
		changes = append(changes, change)
		expectDoesNotExist(t, changes, id2)
	}
}
