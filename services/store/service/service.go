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

// Transaction is like storage.Transaction, but doesn't include extra client-side
// parameters.
type Transaction interface {
	// Commit commits the changes (the Set and Delete operations) in the
	// transaction to the store.  The operation is atomic, so all Set/Delete
	// operations are performed, or none.  Returns an error if the transaction
	// aborted.
	//
	// The Transaction should be discarded once Commit is called.  It can no
	// longer be used.
	Commit() error

	// Abort discards a transaction.  This is an optimization; transactions
	// eventually time out and get discarded.  However, live transactions
	// consume resources, so it is good practice to clean up.
	Abort() error
}

// Object is like storage.Object, but it include extra server-side parameters.
// In perticular, each method takes the identity of the client.
type Object interface {
	// Exists returns true iff the Entry has a value.
	Exists(clientID security.PublicID, t Transaction) (bool, error)

	// Get returns the value for the Object.  The value returned is from the
	// most recent mutation of the entry in the Transaction, or from the
	// Transaction's snapshot if there is no mutation.
	Get(clientID security.PublicID, t Transaction) (*storage.Entry, error)

	// Put adds or modifies the Object.  If there is no current value, the
	// object is created with default attributes.  It is legal to update a
	// subfield of a value.  Returns the updated *Stat of the store value.  If
	// putting a subfield, the *Stat is for the enclosing store value.
	Put(clientID security.PublicID, t Transaction, v interface{}) (*storage.Stat, error)

	// Remove removes the Object.
	Remove(clientID security.PublicID, t Transaction) error

	// SetAttr changes the attributes of the entry, such as permissions and
	// replication groups.  Attributes are associated with the value, not the
	// path.
	SetAttr(clientID security.PublicID, t Transaction, attrs ...storage.Attr) error

	// Stat returns entry info.
	Stat(clientID security.PublicID, t Transaction) (*storage.Stat, error)

	// Query returns entries matching the given query.
	Query(clientID security.PublicID, t Transaction, q query.Query) (QueryStream, error)

	// Glob returns the sequence of names that match the given pattern.
	Glob(clientID security.PublicID, t Transaction, pattern string) (GlobStream, error)
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
	PutMutations(ctx ipc.ServerContext, stream raw.StoreServicePutMutationsStream) error

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

// Watcher is the interface for watching store updates matching a pattern or query.
type Watcher interface {
	// WatchRaw returns a stream of all changes.
	WatchRaw(ctx ipc.ServerContext, req raw.Request, stream raw.StoreServiceWatchStream) error

	// WatchGlob returns a stream of changes that match a pattern.
	WatchGlob(ctx ipc.ServerContext, path storage.PathName, req watch.GlobRequest,
		stream watch.GlobWatcherServiceWatchGlobStream) error

	// WatchQuery returns a stream of changes that satisfy a query.
	WatchQuery(ctx ipc.ServerContext, path storage.PathName, req watch.QueryRequest,
		stream watch.QueryWatcherServiceWatchQueryStream) error

	// Close closes the Watcher, blocking until all Watch invocations complete.
	Close() error
}
