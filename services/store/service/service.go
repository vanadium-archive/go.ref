package service

import (
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/query"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/storage"
)

// Object is like storage.Object, but it include extra server-side parameters.
// In perticular, each method takes the identity of the client.
type Object interface {
	// Exists returns true iff the Entry has a value.
	Exists(clientID security.PublicID, t storage.Transaction) (bool, error)

	// Get returns the value for the Object.  The value returned is from the
	// most recent mutation of the entry in the storage.Transaction, or from the
	// storage.Transaction's snapshot if there is no mutation.
	Get(clientID security.PublicID, t storage.Transaction) (*storage.Entry, error)

	// Put adds or modifies the Object.  If there is no current value, the
	// object is created with default attributes.  It is legal to update a
	// subfield of a value.  Returns the updated *Stat of the store value.  If
	// putting a subfield, the *Stat is for the enclosing store value.
	Put(clientID security.PublicID, t storage.Transaction, v interface{}) (*storage.Stat, error)

	// Remove removes the Object.
	Remove(clientID security.PublicID, t storage.Transaction) error

	// SetAttr changes the attributes of the entry, such as permissions and
	// replication groups.  Attributes are associated with the value, not the
	// path.
	SetAttr(clientID security.PublicID, t storage.Transaction, attrs ...storage.Attr) error

	// Stat returns entry info.
	Stat(clientID security.PublicID, t storage.Transaction) (*storage.Stat, error)

	// Query returns entries matching the given query.
	Query(clientID security.PublicID, t storage.Transaction, q query.Query) (QueryStream, error)

	// Glob returns the sequence of names that match the given pattern.
	Glob(clientID security.PublicID, t storage.Transaction, pattern string) (GlobStream, error)
}

// Store is the server-side interface to the storage system. It is expected to
// sit directly behind the rpc handlers defined by
// veyron2/services/store/service.idl.
type Store interface {
	// Bind returns the Object associated with a path.
	Bind(path string) Object

	// PutMutations atomically commits a stream of Mutations when the stream is
	// closed. Mutations are not committed if the request is cancelled before
	// the stream has been closed.
	PutMutations(ctx ipc.Context, stream raw.StoreServicePutMutationsStream) error

	// SetConflictResolver specifies a function to perform conflict resolution.
	// The <ty> represents the IDL name for the type.
	SetConflictResolver(ty string, r storage.ConflictResolver)

	// Close closes the Store.
	Close() error
}

// GlobStream represents a sequence of results from a glob call.
type GlobStream interface {
	// IsValid returns true iff the iterator refers to an element.
	IsValid() bool

	// Return one possible name for this entry.
	Name() string

	// Next advances to the next element.
	Next()
}

// QueryStream yields the results of a query. Usage:
//   qs, _ := obj.Query
//   for qs.Next() {
//     result := qs.Get()
//     ...
//     if enough_results {
//       qs.Abort()
//     }
//   }
//   if err := qs.Err(); err != nil {
//     ...
//   }
// Iterator is thread-safe.
type QueryStream interface {
	// Next advances the iterator.  It must be called before calling Get.
	// Returns true if there is a value to be retrieved with Get.  Returns
	// false when iteration is finished.
	Next() bool

	// Get retrieves a query result.  It is idempotent.
	Get() *store.QueryResult

	// Err returns the first error encountered during query evaluation.  It is
	// idempotent.
	Err() error

	// Abort stops query evaluation early.  The client must call Abort unless
	// iteration goes to completion (i.e. Next returns false).  It is
	// idempotent and can be called from any thread.
	Abort()
}

// Watcher is the interface for watching store updates that match a query.
type Watcher interface {
	// Watch returns a stream of changes that match a query.
	Watch(ctx ipc.Context, req watch.Request, stream watch.WatcherServiceWatchStream) error

	// Close closes the Watcher, blocking until all Watch invocations complete.
	Close() error
}
