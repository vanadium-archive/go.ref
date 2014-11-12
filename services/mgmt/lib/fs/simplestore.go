// Implements a map-based store substitute that implements the legacy store API.
package fs

import (
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"

	"veyron.io/veyron/veyron/services/mgmt/profile"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/verror"
)

// TODO(rjkroege@google.com) Switch Memstore to the mid-August 2014
// style store API.

// Memstore contains the state of the memstore. It supports a single
// transaction at a time. The current state of a Memstore under a
// transactional name binding is the contents of puts then the contents
// of (data - removes). puts and removes will be empty at the beginning
// of a transaction and after an Unlock operation.
type Memstore struct {
	sync.Mutex
	persistedFile              string
	haveTransactionNameBinding bool
	locked                     bool
	data                       map[string]interface{}
	puts                       map[string]interface{}
	removes                    map[string]struct{}
}

const (
	startingMemstoreSize  = 10
	transactionNamePrefix = "memstore-transaction"
)

var keyExists = struct{}{}

// The implementation of set requires gob instead of json.
func init() {
	gob.Register(profile.Specification{})
	gob.Register(application.Envelope{})
}

// NewMemstore persists the Memstore to os.TempDir() if no file is
// configured.
func NewMemstore(configuredPersistentFile string) (*Memstore, error) {
	data := make(map[string]interface{}, startingMemstoreSize)
	if configuredPersistentFile == "" {
		f, err := ioutil.TempFile(os.TempDir(), "memstore-gob")
		if err != nil {
			return nil, fmt.Errorf("TempFile(%q, %q) failed: %v", os.TempDir(), "memstore-gob", err)
		}
		defer f.Close()
		configuredPersistentFile = f.Name()
		return &Memstore{
			data:          data,
			persistedFile: configuredPersistentFile,
		}, nil
	}

	file, err := os.Open(configuredPersistentFile)
	if err != nil {
		// The file doesn't exist. Attempt to create it instead.
		file, cerr := os.Create(configuredPersistentFile)
		if cerr != nil {
			return nil, fmt.Errorf("File (%q) could neither be opened (%v) nor created (%v)", configuredPersistentFile, err, cerr)
		}
		defer file.Close()
	} else {
		decoder := gob.NewDecoder(file)
		if err := decoder.Decode(&data); err != nil {
			// Two situations. One is not an error.
			fi, err := os.Stat(configuredPersistentFile)
			if err != nil {
				// Someone probably deleted the file out from underneath us. Give up.
				return nil, fmt.Errorf("Decode() failed, file went missing: %v", err)
			}
			if fi.Size() != 0 {
				return nil, fmt.Errorf("Decode() failed, backing file truncated: %v", err)
			}
			// An empty backing file deserializes to an empty memstore.
		}
	}
	return &Memstore{
		data:          data,
		persistedFile: configuredPersistentFile,
	}, nil
}

type MemstoreObject interface {
	Remove(_ interface{}) error
	Exists(_ interface{}) (bool, error)
}

type boundObject struct {
	path  string
	ms    *Memstore
	Value interface{}
}

// BindObject sets the path string for subsequent operations.
func (ms *Memstore) BindObject(path string) *boundObject {
	pathParts := strings.SplitN(path, "/", 2)
	if pathParts[0] == transactionNamePrefix {
		ms.haveTransactionNameBinding = true
	} else {
		ms.haveTransactionNameBinding = false
	}
	return &boundObject{path: pathParts[1], ms: ms}
}

func (ms *Memstore) removeChildren(path string) bool {
	deleted := false
	for k, _ := range ms.data {
		if strings.HasPrefix(k, path) {
			deleted = true
			ms.removes[k] = keyExists
		}
	}
	for k, _ := range ms.puts {
		if strings.HasPrefix(k, path) {
			deleted = true
			delete(ms.puts, k)
		}
	}
	return deleted
}

type Transaction interface {
	CreateTransaction(_ interface{}) (string, error)
	Commit(_ interface{}) error
}

// BindTransactionRoot on a Memstore always operates over the
// entire Memstore. As a result, the root parameter is ignored.
func (ms *Memstore) BindTransactionRoot(_ string) Transaction {
	return ms
}

// BindTransaction on a Memstore can only use the single Memstore
// transaction.
func (ms *Memstore) BindTransaction(_ string) Transaction {
	return ms
}

func (ms *Memstore) newTransactionState() {
	ms.puts = make(map[string]interface{}, startingMemstoreSize)
	ms.removes = make(map[string]struct{}, startingMemstoreSize)
}

func (ms *Memstore) clearTransactionState() {
	ms.puts = nil
	ms.removes = nil
}

// Unlock abandons an in-progress transaction before releasing the lock.
func (ms *Memstore) Unlock() {
	ms.locked = false
	ms.clearTransactionState()
	ms.Mutex.Unlock()
}

// Lock acquires a lock and caches the state of the lock.
func (ms *Memstore) Lock() {
	ms.Mutex.Lock()
	ms.clearTransactionState()
	ms.locked = true
}

// CreateTransaction requires the caller to acquire a lock on the Memstore.
func (ms *Memstore) CreateTransaction(_ interface{}) (string, error) {
	if ms.puts != nil || ms.removes != nil {
		return "", verror.BadProtocolf("recursive CreateTransaction() not permitted")
	}
	ms.newTransactionState()
	return transactionNamePrefix, nil
}

// Commit updates the store and persists the result.
func (ms *Memstore) Commit(_ interface{}) error {
	if !ms.locked || ms.puts == nil || ms.removes == nil {
		return verror.BadProtocolf("illegal attempt to commit previously committed or abandonned transaction")
	}
	for k, v := range ms.puts {
		ms.data[k] = v
	}
	for k, _ := range ms.removes {
		delete(ms.data, k)
	}
	return ms.persist()
}

func (ms *Memstore) Abort(_ interface{}) error {
	if !ms.locked {
		return verror.BadProtocolf("illegal attempt to abort non-existent transaction")
	}
	return nil
}

func (o *boundObject) Remove(_ interface{}) error {
	if !o.ms.locked {
		return verror.BadProtocolf("Remove() without a transaction.")
	}

	if _, pendingRemoval := o.ms.removes[o.path]; pendingRemoval {
		return verror.NoExistf("path %s not in Memstore", o.path)
	}

	_, found := o.ms.data[o.path]
	if !found && !o.ms.removeChildren(o.path) {
		return verror.NoExistf("path %s not in Memstore", o.path)
	}
	delete(o.ms.puts, o.path)
	o.ms.removes[o.path] = keyExists
	return nil
}

// transactionExists implements Exists() for bound names that have the
// transaction prefix.
func (o *boundObject) transactionExists() bool {
	// Determine if the bound name point to a real object.
	_, inBase := o.ms.data[o.path]
	_, inPuts := o.ms.puts[o.path]
	_, inRemoves := o.ms.removes[o.path]

	// not yet committed.
	if inPuts || (inBase && !inRemoves) {
		return true
	}

	// The bound names might be a prefix of the path for a real object. For
	// example, BindObject("/test/a"). Put(o) creates a real object o at path
	/// test/a so the code above will cause BindObject("/test/a").Exists() to
	// return true. Testing this is however not sufficient because
	// BindObject(any prefix of on object path).Exists() needs to also be
	// true. For example, here BindObject("/test").Exists() is true.
	//
	// Consequently, transactionExists scans all object names in the Memstore
	// to determine if any of their names have the bound name as a prefix.
	// Puts take precedence over removes so we scan it first.

	for k, _ := range o.ms.puts {
		if strings.HasPrefix(k, o.path) {
			return true
		}
	}

	// Then we scan data for matches and verify that at least one of the
	// object names with the bound prefix have not been removed.

	for k, _ := range o.ms.data {
		if _, inRemoves := o.ms.removes[k]; strings.HasPrefix(k, o.path) && !inRemoves {
			return true
		}
	}
	return false
}

func (o *boundObject) Exists(_ interface{}) (bool, error) {
	if o.ms.haveTransactionNameBinding {
		return o.transactionExists(), nil
	} else {
		_, inBase := o.ms.data[o.path]
		if inBase {
			return true, nil
		}
		for k, _ := range o.ms.data {
			if strings.HasPrefix(k, o.path) {
				return true, nil
			}
		}
	}
	return false, nil
}

// transactionBoundGet implements Get while the bound name has the
// transaction prefix.
func (o *boundObject) transactionBoundGet() (*boundObject, error) {
	bv, inBase := o.ms.data[o.path]
	_, inRemoves := o.ms.removes[o.path]
	pv, inPuts := o.ms.puts[o.path]

	found := inPuts || (inBase && !inRemoves)
	if !found {
		return nil, verror.NoExistf("path %s not in Memstore", o.path)
	}

	if inPuts {
		o.Value = pv
	} else {
		o.Value = bv
	}
	return o, nil
}

func (o *boundObject) bareGet() (*boundObject, error) {
	bv, inBase := o.ms.data[o.path]

	if !inBase {
		return nil, verror.NoExistf("path %s not in Memstore", o.path)
	}

	o.Value = bv
	return o, nil
}

func (o *boundObject) Get(_ interface{}) (*boundObject, error) {
	if o.ms.haveTransactionNameBinding {
		return o.transactionBoundGet()
	} else {
		return o.bareGet()
	}
}

func (o *boundObject) Put(_ interface{}, envelope interface{}) (*boundObject, error) {
	if !o.ms.locked {
		return nil, verror.BadProtocolf("Put() without a transaction.")
	}
	switch v := envelope.(type) {
	case application.Envelope, profile.Specification:
		o.ms.puts[o.path] = v
		delete(o.ms.removes, o.path)
		o.Value = o.path
		return o, nil
	default:
		return o, verror.BadProtocolf("attempted Put to Memstore of unsupported type")
	}
}

func (o *boundObject) Children() ([]string, error) {
	if !o.ms.locked {
		return nil, verror.BadProtocolf("Children() without a lock.")
	}
	found := false
	set := make(map[string]struct{})
	for k, _ := range o.ms.data {
		if strings.HasPrefix(k, o.path) || o.path == "" {
			name := strings.TrimPrefix(k, o.path)
			// Found the object itself.
			if len(name) == 0 {
				found = true
				continue
			}
			// This was only a prefix match, not what we're looking for.
			if name[0] != '/' && o.path != "" {
				continue
			}
			found = true
			name = strings.TrimLeft(name, "/")
			if idx := strings.Index(name, "/"); idx != -1 {
				name = name[:idx]
			}
			set[name] = keyExists
		}
	}
	if !found {
		return nil, verror.NoExistf("object %q does not exist", o.path)
	}
	children := make([]string, len(set))
	i := 0
	for k, _ := range set {
		children[i] = k
		i++
	}
	sort.Strings(children)
	return children, nil
}

// persist() writes the state of the Memstore to persistent storage.
func (ms *Memstore) persist() error {
	file, err := os.Create(ms.persistedFile)
	if err != nil {
		return fmt.Errorf("Create(%v) failed: %v", ms.persistedFile, err)
	}
	defer file.Close()

	// TODO(rjkroege): Switch to the vom encoder.
	enc := gob.NewEncoder(file)
	err = enc.Encode(ms.data)
	if err := enc.Encode(ms.data); err != nil {
		return fmt.Errorf("Encode() failed: %v", err)
	}
	ms.clearTransactionState()
	return nil
}
