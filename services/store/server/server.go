// Package server implements a storage service.
package server

import (
	"errors"
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
	"veyron2/storage"
	"veyron2/vdl"
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

type transaction struct {
	trans   storage.Transaction
	expires time.Time
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
func (s *Server) findTransaction(id store.TransactionID) (storage.Transaction, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.findTransactionLocked(id)
}

func (s *Server) findTransactionLocked(id store.TransactionID) (storage.Transaction, bool) {
	if id == nullTransactionID {
		return nil, true
	}
	info, ok := s.transactions[id]
	if !ok {
		return nil, false
	}
	return info.trans, true
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
func (s *Server) CreateTransaction(_ ipc.Context, id store.TransactionID, opts []vdl.Any) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	info, ok := s.transactions[id]
	if ok {
		return errTransactionAlreadyExists
	}
	info = &transaction{
		trans:   memstore.NewTransaction(),
		expires: time.Now().Add(transactionMaxLifetime),
	}
	s.transactions[id] = info
	return nil
}

// Commit commits the changes in the transaction to the store.  The
// operation is atomic, so all mutations are performed, or none.  Returns an
// error if the transaction aborted.
func (s *Server) Commit(_ ipc.Context, id store.TransactionID) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	t, ok := s.findTransactionLocked(id)
	if !ok {
		return errTransactionDoesNotExist
	}
	err := t.Commit()
	delete(s.transactions, id)
	return err
}

// Abort discards a transaction.
func (s *Server) Abort(_ ipc.Context, id store.TransactionID) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	t, ok := s.transactions[id]
	if !ok {
		return errTransactionDoesNotExist
	}
	t.trans.Abort()
	delete(s.transactions, id)
	return nil
}

// Watch returns a stream of changes.
func (s *Server) Watch(ctx ipc.Context, req watch.Request, stream watch.WatcherServiceWatchStream) error {
	return s.watcher.Watch(ctx, req, stream)
}

// PutMutations atomically commits a stream of Mutations when the stream is
// closed. Mutations are not committed if the request is cancelled before the
// stream has been closed.
func (s *Server) PutMutations(ctx ipc.Context, stream raw.StoreServicePutMutationsStream) error {
	return s.store.PutMutations(ctx, stream)
}

// ReadConflicts returns the stream of conflicts to store values.  A
// conflict occurs when there is a concurrent modification to a value.
func (s *Server) ReadConflicts(_ ipc.Context, stream store.StoreServiceReadConflictsStream) error {
	panic("not implemented")
}

// Store, raw store and object dispatchers.
// Typically, the store dispatcher handles paths with ".store" prefix, the
// raw store dispatcher handles paths with ".store.raw" prefix, and the
// object dispatcher handles paths with "" prefix.
// TODO(sadovsky): Revisit this scheme. Seems simpler to have one dispatcher?

// NewStoreDispatcher returns a store dispatcher.
func NewStoreDispatcher(s *Server, auth security.Authorizer) ipc.Dispatcher {
	return ipc.SoloDispatcher(store.NewServerStore(s), auth)
}

// NewRawStoreDispatcher returns a raw store dispatcher.
func NewRawStoreDispatcher(s *Server, auth security.Authorizer) ipc.Dispatcher {
	return ipc.SoloDispatcher(raw.NewServerStore(s), auth)
}

type objectDispatcher struct {
	s    *Server
	auth security.Authorizer
}

// NewObjectDispatcher returns an object dispatcher.
func NewObjectDispatcher(s *Server, auth security.Authorizer) ipc.Dispatcher {
	return &objectDispatcher{s: s, auth: auth}
}

func (d *objectDispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	o := d.s.lookupObject(suffix)
	serverObject := store.NewServerObject(o)
	return ipc.ReflectInvoker(serverObject), d.auth, nil
}

func (s *Server) lookupObject(name string) *object {
	return &object{name: name, obj: s.store.Bind(name), server: s}
}
