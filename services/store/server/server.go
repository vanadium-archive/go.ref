// Package server implements a storage service.
package server

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	"veyron/services/store/memstore"
	memwatch "veyron/services/store/memstore/watch"
	"veyron/services/store/raw"
	"veyron/services/store/service"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/vdl"
	"veyron2/verror"
)

const (
	// transactionMAxLifetime is the maximum duration before a transaction will
	// be garbage collected.
	//
	// TODO(jyh): This should probably be a configuration parameter.
	transactionMaxLifetime = 30 * time.Second
)

var (
	// Server implements the StoreService interface.
	_ store.StoreService = (*Server)(nil)

	nullTransactionID store.TransactionID

	errTransactionAlreadyExists = errors.New("transaction already exists")
	errTransactionDoesNotExist  = errors.New("transaction does not exist")
	// Transaction exists, but may not be used by the caller.
	errPermissionDenied = errors.New("permission denied")
)

// Server stores the dictionary of all media items.  It has a scanner.Scanner
// for collecting files from the filesystem.  For each file, a FileService is
// registered to serve the file.
type Server struct {
	mutex sync.RWMutex

	// store is the actual store implementation.
	store service.Store

	// transactions are the set of active transactions.
	transactions map[store.TransactionID]*transaction

	// Transaction garbage collection.
	pending sync.WaitGroup
	ticker  *time.Ticker
	closed  chan struct{}

	// watcher is the actual store watcher implementation.
	watcher service.Watcher
}

// transactionContext defines the context in which a transaction is used. A
// transaction may be used only in the context that created it.
// transactionContext weakly identifies a session by the local and remote
// principals involved in the RPC.
// TODO(tilaks): Use the local and remote addresses to identify the session.
// Does a session with a mobile device break if the remote address changes?
type transactionContext interface {
	// LocalID returns the PublicID of the principal at the local end of the request.
	LocalID() security.PublicID
	// RemoteID returns the PublicID of the principal at the remote end of the request.
	RemoteID() security.PublicID
}

type transaction struct {
	trans      service.Transaction
	expires    time.Time
	creatorCtx transactionContext
}

// ServerConfig provides the parameters needed to construct a Server.
type ServerConfig struct {
	Admin  security.PublicID // Administrator.
	DBName string            // DBName is the name if the database directory.
}

// New creates a new server.
func New(config ServerConfig) (*Server, error) {
	mstore, err := memstore.New(config.Admin, config.DBName)
	if err != nil {
		return nil, err
	}
	mwatcher, err := memwatch.New(config.Admin, config.DBName)
	if err != nil {
		return nil, err
	}
	s := &Server{
		store:        mstore,
		transactions: make(map[store.TransactionID]*transaction),
		ticker:       time.NewTicker(time.Second),
		closed:       make(chan struct{}),
		watcher:      mwatcher,
	}
	s.pending.Add(1)
	go s.gcLoop()
	return s, nil
}

func (s *Server) Close() {
	close(s.closed)
	s.ticker.Stop()
	s.pending.Wait()
	s.store.Close()
	s.watcher.Close()
}

func (s *Server) String() string {
	return "StoreServer"
}

// Attributes returns the server status.
func (s *Server) Attributes(arg string) map[string]string {
	return map[string]string{
		"health":     "ok",
		"servertype": s.String(),
	}
}

// findTransaction returns the transaction for the TransactionID.
func (s *Server) findTransaction(ctx transactionContext, id store.TransactionID) (service.Transaction, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.findTransactionLocked(ctx, id)
}

func (s *Server) findTransactionLocked(ctx transactionContext, id store.TransactionID) (service.Transaction, error) {
	if id == nullTransactionID {
		return nil, nil
	}
	info, ok := s.transactions[id]
	if !ok {
		return nil, errTransactionDoesNotExist
	}
	// A transaction may be used only by the session (and therefore client)
	// that created it.
	if !info.matchesContext(ctx) {
		return nil, errPermissionDenied
	}
	return info.trans, nil
}

func (t *transaction) matchesContext(ctx transactionContext) bool {
	creatorCtx := t.creatorCtx
	return membersEqual(creatorCtx.LocalID().Names(), ctx.LocalID().Names()) &&
		membersEqual(creatorCtx.RemoteID().Names(), ctx.RemoteID().Names())
}

// membersEquals checks whether two slices of strings have the same set of
// members, regardless of order.
func membersEqual(slice1, slice2 []string) bool {
	set1 := make(map[string]bool, len(slice1))
	for _, s := range slice1 {
		set1[s] = true
	}
	set2 := make(map[string]bool, len(slice2))
	for _, s := range slice2 {
		set2[s] = true
	}
	// DeepEqual tests keys for == equality, which is sufficient for strings.
	return reflect.DeepEqual(set1, set2)
}

// gcLoop drops transactions that have expired.
func (s *Server) gcLoop() {
	for {
		select {
		case <-s.closed:
			s.pending.Done()
			return
		case <-s.ticker.C:
		}

		s.mutex.Lock()
		now := time.Now()
		for id, t := range s.transactions {
			if now.After(t.expires) {
				t.trans.Abort()
				delete(s.transactions, id)
			}
		}
		s.mutex.Unlock()
	}
}

// CreateTransaction creates a transaction.
func (s *Server) CreateTransaction(ctx ipc.ServerContext, id store.TransactionID, opts []vdl.Any) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	info, ok := s.transactions[id]
	if ok {
		return errTransactionAlreadyExists
	}
	info = &transaction{
		trans:      memstore.NewTransaction(),
		expires:    time.Now().Add(transactionMaxLifetime),
		creatorCtx: ctx,
	}
	s.transactions[id] = info
	return nil
}

// Commit commits the changes in the transaction to the store.  The
// operation is atomic, so all mutations are performed, or none.  Returns an
// error if the transaction aborted.
func (s *Server) Commit(ctx ipc.ServerContext, id store.TransactionID) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	t, err := s.findTransactionLocked(ctx, id)
	if err != nil {
		return err
	}
	if t == nil {
		return errTransactionDoesNotExist
	}
	err = t.Commit()
	delete(s.transactions, id)
	return err
}

// Abort discards a transaction.
func (s *Server) Abort(ctx ipc.ServerContext, id store.TransactionID) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	t, err := s.findTransactionLocked(ctx, id)
	if err != nil {
		return err
	}
	if t == nil {
		return errTransactionDoesNotExist
	}
	err = t.Abort()
	delete(s.transactions, id)
	return err
}

// Watch returns a stream of changes.
func (s *Server) Watch(ctx ipc.ServerContext, req watch.Request, stream watch.WatcherServiceWatchStream) error {
	return s.watcher.Watch(ctx, req, stream)
}

// PutMutations atomically commits a stream of Mutations when the stream is
// closed. Mutations are not committed if the request is cancelled before the
// stream has been closed.
func (s *Server) PutMutations(ctx ipc.ServerContext, stream raw.StoreServicePutMutationsStream) error {
	return s.store.PutMutations(ctx, stream)
}

// ReadConflicts returns the stream of conflicts to store values.  A
// conflict occurs when there is a concurrent modification to a value.
func (s *Server) ReadConflicts(_ ipc.ServerContext, stream store.StoreServiceReadConflictsStream) error {
	return verror.Internalf("ReadConflicts not yet implemented")
}

type storeDispatcher struct {
	s    *Server
	auth security.Authorizer
}

// NewStoreDispatcher returns an object dispatcher.
func NewStoreDispatcher(s *Server, auth security.Authorizer) ipc.Dispatcher {
	return &storeDispatcher{s: s, auth: auth}
}

func (d *storeDispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(d.lookupServer(suffix)), d.auth, nil
}

func (d *storeDispatcher) lookupServer(suffix string) interface{} {
	if strings.HasSuffix(suffix, store.StoreSuffix) {
		return store.NewServerStore(d.s)
	} else if strings.HasSuffix(suffix, raw.RawStoreSuffix) {
		return raw.NewServerStore(d.s)
	} else {
		o := d.s.lookupObject(suffix)
		return store.NewServerObject(o)
	}
}

func (s *Server) lookupObject(name string) *object {
	return &object{name: name, obj: s.store.Bind(name), server: s}
}
