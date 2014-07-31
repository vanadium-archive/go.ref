package service

// Defines the store server Go API.
// NOTE(sadovsky): See comments in go/vcl/3292 for why we have this API. It may
// no longer be necessary.

import (
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/services/watch"
	"veyron2/storage"
)

// Transaction is like storage.Transaction, but doesn't include extra
// client-side parameters.
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
