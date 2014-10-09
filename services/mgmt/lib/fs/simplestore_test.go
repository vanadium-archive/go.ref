package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	_ "veyron.io/veyron/veyron/services/mgmt/profile"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/verror"
)

func TestNewMemstore(t *testing.T) {
	memstore, err := NewMemstore("")

	if err != nil {
		t.Fatalf("NewMemstore() failed: %v", err)
	}

	_, err = os.Stat(memstore.persistedFile)
	if err != nil {
		t.Fatalf("Stat(%v) failed: %v", memstore.persistedFile, err)
	}
}

func TestNewNamedMemstore(t *testing.T) {
	path := filepath.Join(os.TempDir(), "namedms")
	memstore, err := NewMemstore(path)
	if err != nil {
		t.Fatalf("NewMemstore() failed: %v", err)
	}
	defer os.Remove(path)

	_, err = os.Stat(memstore.persistedFile)
	if err != nil {
		t.Fatalf("Stat(%v) failed: %v", path, err)
	}
}

// Verify that all of the listed paths Exists().
// Caller is responsible for setting up any transaction state necessary.
func allPathsExist(ts *Memstore, paths []string) error {
	for _, p := range paths {
		exists, err := ts.BindObject(p).Exists(nil)
		if err != nil {
			return fmt.Errorf("Exists(%s) expected to succeed but failed: %v", p, err)
		}
		if !exists {
			return fmt.Errorf("Exists(%s) expected to be true but is false", p)
		}
	}
	return nil
}

// Verify that all of the listed paths !Exists().
// Caller is responsible for setting up any transaction state necessary.
func allPathsDontExist(ts *Memstore, paths []string) error {
	for _, p := range paths {
		exists, err := ts.BindObject(p).Exists(nil)
		if err != nil {
			return fmt.Errorf("Exists(%s) expected to succeed but failed: %v", p, err)
		}
		if exists {
			return fmt.Errorf("Exists(%s) expected to be false but is true", p)
		}
	}
	return nil
}

type PathValue struct {
	Path     string
	Expected interface{}
}

// getEquals tests that every provided path is equal to the specified value.
func allPathsEqual(ts *Memstore, pvs []PathValue) error {
	for _, p := range pvs {
		v, err := ts.BindObject(p.Path).Get(nil)
		if err != nil {
			return fmt.Errorf("Get(%s) expected to succeed but failed", p, err)
		}
		if !reflect.DeepEqual(p.Expected, v.Value) {
			return fmt.Errorf("Unexpected non-equality for %s: got %v, expected %v", p.Path, v.Value, p.Expected)
		}
	}
	return nil
}

// tP is a convenience function. It prepends the transactionNamePrefix
// to the given path.
func tP(path string) string {
	return naming.Join(transactionNamePrefix, path)
}

func TestSerializeDeserialize(t *testing.T) {
	path := filepath.Join(os.TempDir(), "namedms")
	memstoreOriginal, err := NewMemstore(path)
	if err != nil {
		t.Fatalf("NewMemstore() failed: %v", err)
	}
	defer os.Remove(path)

	// Create example data.
	envelope := application.Envelope{
		Args:   []string{"--help"},
		Env:    []string{"DEBUG=1"},
		Binary: "/veyron/name/of/binary",
	}
	secondEnvelope := application.Envelope{
		Args:   []string{"--save"},
		Env:    []string{"VEYRON=42"},
		Binary: "/veyron/name/of/binary/is/memstored",
	}

	// TRANSACTION BEGIN
	// Insert a value into the Memstore at /test/a
	memstoreOriginal.Lock()
	tname, err := memstoreOriginal.BindTransactionRoot("ignored").CreateTransaction(nil)
	if err != nil {
		t.Fatalf("CreateTransaction() failed: %v", err)
	}
	if _, err := memstoreOriginal.BindObject(tP("/test/a")).Put(nil, envelope); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	if err := allPathsExist(memstoreOriginal, []string{tP("/test/a"), tP("/test")}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{{tP("/test/a"), envelope}}); err != nil {
		t.Fatalf("%v", err)
	}

	if err := memstoreOriginal.BindTransaction(tname).Commit(nil); err != nil {
		t.Fatalf("Commit() failed: %v", err)
	}
	memstoreOriginal.Unlock()
	// TRANSACTION END

	// Validate persisted state.
	if err := allPathsExist(memstoreOriginal, []string{"/test/a", "/test"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{{"/test/a", envelope}}); err != nil {
		t.Fatalf("%v", err)
	}

	// TRANSACTION BEGIN Write a value to /test/b as well.
	memstoreOriginal.Lock()
	tname, err = memstoreOriginal.BindTransactionRoot("also ignored").CreateTransaction(nil)
	bindingTnameTestB := memstoreOriginal.BindObject(tP("/test/b"))
	if _, err := bindingTnameTestB.Put(nil, envelope); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// Validate persisted state during transaction
	if err := allPathsExist(memstoreOriginal, []string{"/test/a", "/test"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{{"/test/a", envelope}}); err != nil {
		t.Fatalf("%v", err)
	}
	// Validate pending state during transaction
	if err := allPathsExist(memstoreOriginal, []string{tP("/test/a"), tP("/test"), tP("/test/b")}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{
		{tP("/test/a"), envelope},
		{tP("/test/b"), envelope}}); err != nil {
		t.Fatalf("%v", err)
	}

	// Commit the <tname>/test/b to /test/b
	if err := memstoreOriginal.Commit(nil); err != nil {
		t.Fatalf("Commit() failed: %v", err)
	}
	memstoreOriginal.Unlock()
	// TODO(rjkroege): Consider ensuring that Get() on  <tname>/test/b should now fail.
	// TRANSACTION END

	// Validate persisted state after transaction
	if err := allPathsExist(memstoreOriginal, []string{"/test/a", "/test", "/test/b"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{
		{"/test/a", envelope},
		{"/test/b", envelope}}); err != nil {
		t.Fatalf("%v", err)
	}

	// TRANSACTION BEGIN (to be abandonned)
	memstoreOriginal.Lock()
	tname, err = memstoreOriginal.BindTransactionRoot("").CreateTransaction(nil)

	// Exists is true before doing anything.
	if err := allPathsExist(memstoreOriginal, []string{tP("/test")}); err != nil {
		t.Fatalf("%v", err)
	}

	if _, err := memstoreOriginal.BindObject(tP("/test/b")).Put(nil, secondEnvelope); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	// Validate persisted state during transaction
	if err := allPathsExist(memstoreOriginal, []string{
		"/test/a",
		"/test/b",
		"/test",
		tP("/test"),
		tP("/test/a"),
		tP("/test/b"),
	}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{
		{"/test/a", envelope},
		{"/test/b", envelope},
		{tP("/test/b"), secondEnvelope},
		{tP("/test/a"), envelope},
	}); err != nil {
		t.Fatalf("%v", err)
	}

	// Pending Remove() of /test
	if err := memstoreOriginal.BindObject(tP("/test")).Remove(nil); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Verify that all paths are successfully removed from the in-progress transaction.
	if err := allPathsDontExist(memstoreOriginal, []string{tP("/test/a"), tP("/test"), tP("/test/b")}); err != nil {
		t.Fatalf("%v", err)
	}
	// But all paths remain in the persisted version.
	if err := allPathsExist(memstoreOriginal, []string{"/test/a", "/test", "/test/b"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{
		{"/test/a", envelope},
		{"/test/b", envelope},
	}); err != nil {
		t.Fatalf("%v", err)
	}

	// At which point, Get() on the transaction won't find anything.
	if _, err := memstoreOriginal.BindObject(tP("/test/a")).Get(nil); !verror.Is(err, verror.NoExist) {
		t.Fatalf("Get() should have failed: got %v, expected %v", err, verror.NoExistf("path %s not in Memstore", tname+"/test/a"))
	}

	// Attempting to Remove() it over again will fail.
	if err := memstoreOriginal.BindObject(tP("/test/a")).Remove(nil); !verror.Is(err, verror.NoExist) {
		t.Fatalf("Remove() should have failed: got %v, expected %v", err, verror.NoExistf("path %s not in Memstore", tname+"/test/a"))
	}

	// Attempting to Remove() a non-existing path will fail.
	if err := memstoreOriginal.BindObject(tP("/foo")).Remove(nil); !verror.Is(err, verror.NoExist) {
		t.Fatalf("Remove() should have failed: got %v, expected %v", err, verror.NoExistf("path %s not in Memstore", tname+"/foo"))
	}

	// Exists() a non-existing path will fail.
	if present, _ := memstoreOriginal.BindObject(tP("/foo")).Exists(nil); present {
		t.Fatalf("Exists() should have failed for non-existing path %s", tname+"/foo")
	}

	// Abort the transaction without committing it.
	memstoreOriginal.Abort(nil)
	memstoreOriginal.Unlock()
	// TRANSACTION END (ABORTED)

	// Validate that persisted state after abandonned transaction has not changed.
	if err := allPathsExist(memstoreOriginal, []string{"/test/a", "/test", "/test/b"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreOriginal, []PathValue{
		{"/test/a", envelope},
		{"/test/b", envelope}}); err != nil {
		t.Fatalf("%v", err)
	}

	// Validate that Get will fail on a non-existent path.
	if _, err := memstoreOriginal.BindObject("/test/c").Get(nil); !verror.Is(err, verror.NoExist) {
		t.Fatalf("Get() should have failed: got %v, expected %v", err, verror.NoExistf("path %s not in Memstore", tname+"/test/c"))
	}

	// Verify that the previous Commit() operations have persisted to
	// disk by creating a new Memstore from the contents on disk.
	memstoreCopy, err := NewMemstore(path)
	if err != nil {
		t.Fatalf("NewMemstore() failed: %v", err)
	}
	// Verify that memstoreCopy is an exact copy of memstoreOriginal.
	if err := allPathsExist(memstoreCopy, []string{"/test/a", "/test", "/test/b"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreCopy, []PathValue{
		{"/test/a", envelope},
		{"/test/b", envelope}}); err != nil {
		t.Fatalf("%v", err)
	}

	// TRANSACTION BEGIN
	memstoreCopy.Lock()
	tname, err = memstoreCopy.BindTransactionRoot("also ignored").CreateTransaction(nil)

	// Add a pending object c to test that pending objects are deleted.
	if _, err := memstoreCopy.BindObject(tP("/test/c")).Put(nil, secondEnvelope); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if err := allPathsExist(memstoreCopy, []string{
		tP("/test/a"),
		"/test/a",
		tP("/test"),
		"/test",
		tP("/test/b"),
		"/test/b",
		tP("/test/c"),
	}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsEqual(memstoreCopy, []PathValue{
		{tP("/test/a"), envelope},
		{tP("/test/b"), envelope},
		{tP("/test/c"), secondEnvelope},
		{"/test/a", envelope},
		{"/test/b", envelope},
	}); err != nil {
		t.Fatalf("%v", err)
	}

	// Remove /test/a /test/b /test/c /test
	if err := memstoreCopy.BindObject(tP("/test")).Remove(nil); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}
	// Verify that all paths are successfully removed from the in-progress transaction.
	if err := allPathsDontExist(memstoreCopy, []string{
		tP("/test/a"),
		tP("/test"),
		tP("/test/b"),
		tP("/test/c"),
	}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := allPathsExist(memstoreCopy, []string{
		"/test/a",
		"/test",
		"/test/b",
	}); err != nil {
		t.Fatalf("%v", err)
	}
	// Commit the change.
	if err = memstoreCopy.Commit(nil); err != nil {
		t.Fatalf("Commit() failed: %v", err)
	}
	memstoreCopy.Unlock()
	// TRANSACTION END

	// Create a new Memstore from file to see if Remove operates are
	// persisted.
	memstoreRemovedCopy, err := NewMemstore(path)
	if err != nil {
		t.Fatalf("NewMemstore() failed for removed copy: %v", err)
	}
	if err := allPathsDontExist(memstoreRemovedCopy, []string{
		"/test/a",
		"/test",
		"/test/b",
		"/test/c",
	}); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestOperationsNeedValidBinding(t *testing.T) {
	path := filepath.Join(os.TempDir(), "namedms")
	memstoreOriginal, err := NewMemstore(path)
	if err != nil {
		t.Fatalf("NewMemstore() failed: %v", err)
	}
	defer os.Remove(path)

	// Create example data.
	envelope := application.Envelope{
		Args:   []string{"--help"},
		Env:    []string{"DEBUG=1"},
		Binary: "/veyron/name/of/binary",
	}

	// TRANSACTION BEGIN
	// Attempt inserting a value at /test/a.
	memstoreOriginal.Lock()
	tname, err := memstoreOriginal.BindTransactionRoot("").CreateTransaction(nil)
	if err != nil {
		t.Fatalf("CreateTransaction() failed: %v", err)
	}

	if err := memstoreOriginal.BindTransaction(tname).Commit(nil); err != nil {
		t.Fatalf("Commit() failed: %v", err)
	}
	memstoreOriginal.Unlock()
	// TRANSACTION END

	// Put outside ot a transaction should fail.
	bindingTnameTestA := memstoreOriginal.BindObject(naming.Join("fooey", "/test/a"))
	if _, err := bindingTnameTestA.Put(nil, envelope); !verror.Is(err, verror.BadProtocol) {
		t.Fatalf("Put() failed: got %v, expected %v", err, verror.BadProtocolf("Put() without a transactional binding"))
	}

	// Remove outside of a transaction should fail
	if err := bindingTnameTestA.Remove(nil); !verror.Is(err, verror.BadProtocol) {
		t.Fatalf("Put() failed: got %v, expected %v", err, verror.BadProtocolf("Remove() without a transactional binding"))
	}

	// Commit outside of a transaction should fail
	if err := memstoreOriginal.BindTransaction(tname).Commit(nil); !verror.Is(err, verror.BadProtocol) {
		t.Fatalf("Commit() failed: got %v, expected %v", err, verror.BadProtocolf("illegal attempt to commit previously committed or abandonned transaction"))
	}

	// Attempt inserting a value at /test/b
	memstoreOriginal.Lock()
	tname, err = memstoreOriginal.BindTransactionRoot("").CreateTransaction(nil)
	if err != nil {
		t.Fatalf("CreateTransaction() failed: %v", err)
	}

	bindingTnameTestB := memstoreOriginal.BindObject(tP("/test/b"))
	if _, err := bindingTnameTestB.Put(nil, envelope); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	// Abandon transaction.
	memstoreOriginal.Unlock()

	// Remove should definitely fail on an abndonned transaction.
	if err := bindingTnameTestB.Remove(nil); !verror.Is(err, verror.BadProtocol) {
		t.Fatalf("Remove() failed: got %v, expected %v", err, verror.Internalf("Remove() without a transactional binding"))
	}
}

func TestOpenEmptyMemstore(t *testing.T) {
	path := filepath.Join(os.TempDir(), "namedms")
	defer os.Remove(path)

	// Create a brand new memstore persisted to namedms. This will
	// have the side-effect of creating an empty backing file.
	_, err := NewMemstore(path)
	if err != nil {
		t.Fatalf("NewMemstore() failed: %v", err)
	}

	// Create another memstore that will attempt to deserialize the empty
	// backing file. 
	_, err = NewMemstore(path)
	if err != nil {
		t.Fatalf("NewMemstore() failed: %v", err)
	}
}
