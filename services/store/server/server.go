// Package server implements a storage service.

package server

// This file defines Server, which implements the server-side Store API from
// veyron2/services/store/service.vdl.

import (
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"veyron/services/store/memstore"
	memwatch "veyron/services/store/memstore/watch"
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/verror"
)

const (
	// transactionMaxLifetime is the maximum duration before a transaction will
	// be garbage collected.
	//
	// TODO(jyh): This should probably be a configuration parameter.
	transactionMaxLifetime = 30 * time.Second
)

var (
	errNestedTransaction = verror.BadArgf("cannot create a nested Transaction")
	// Note, this can happen e.g. due to expiration.
	errTransactionDoesNotExist = verror.NotFoundf("transaction does not exist")
	// Transaction exists, but may not be used by the caller.
	errPermissionDenied = verror.NotAuthorizedf("permission denied")

	rng = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))

	nullTransactionID transactionID
)

// Server implements Store and uses memstore internally.
type Server struct {
	mutex sync.RWMutex

	// store is the actual store implementation.
	store *memstore.Store

	// transactions is the set of active transactions.
	transactions map[transactionID]*transaction

	// Transaction garbage collection.
	pending sync.WaitGroup
	ticker  *time.Ticker
	closed  chan struct{}

	// watcher is the actual store watcher implementation.
	watcher *memwatch.Watcher
}

// transactionID is an internal transaction identifier chosen by the server.
//
// TODO(jyh): Consider using a larger identifier space to reduce chance of
// collisions. (Note, createTransaction handles collisions when generating
// transactionIDs.)
type transactionID uint64

// transactionContext defines the context in which a transaction is used. A
// transaction may be used only in the context that created it.
// transactionContext weakly identifies a session by the local and remote
// principals involved in the RPC.
// TODO(tilaks): Use the local and remote addresses to identify the session.
// Does a session with a mobile device break if the remote address changes?
type transactionContext interface {
	// LocalID returns the PublicID of the principal at the local end of the
	// request.
	LocalID() security.PublicID
	// RemoteID returns the PublicID of the principal at the remote end of the
	// request.
	RemoteID() security.PublicID
}

type transaction struct {
	trans      *memstore.Transaction
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
		transactions: make(map[transactionID]*transaction),
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

// findTransactionComponent returns the (begin, end) offsets of the "$tid.*"
// component in the given object name, or (-1, -1) if oname does not contain a
// transaction component.
func findTransactionComponent(oname string) (int, int) {
	begin := 0
	if !strings.HasPrefix(oname, "$tid") {
		begin = strings.Index(oname, "/$tid")
	}
	if begin == -1 {
		return -1, -1
	}
	end := strings.Index(oname[begin+1:], "/")
	if end == -1 {
		end = len(oname)
	} else {
		end += begin + 1
	}
	return begin, end
}

// TODO(sadovsky): One of the following:
// - Reserve prefix string "$tid." for internal use.
// - Reserve prefix char "$" for internal use.
// - Require users to escape prefix char "$" when they are referring to their
//   own data, e.g. "\$foo".
func makeTransactionComponent(id transactionID) string {
	return fmt.Sprintf("$tid.%d", id)
}

// stripTransactionComponent returns the given object name with its "$tid.*"
// component removed, and also returns the stripped transactionID.
// Examples:
//   "/foo/$tid.123/bar" => {"/foo/bar", transactionID(123)}
//   "/foo/bar"          => {"/foo/bar", nullTransactionID}
func stripTransactionComponent(oname string) (string, transactionID, error) {
	begin, end := findTransactionComponent(oname)
	if begin == -1 {
		return oname, nullTransactionID, nil
	}
	tc := oname[begin:end]
	id, err := strconv.ParseInt(tc[strings.LastIndex(tc, ".")+1:], 10, 64)
	if err != nil {
		return "", nullTransactionID, fmt.Errorf("Failed to extract id from %q", tc)
	}
	return oname[:begin] + oname[end:], transactionID(id), nil
}

func (s *Server) createTransaction(ctx transactionContext, oname string) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var id transactionID
	for {
		id = transactionID(rng.Int63())
		_, ok := s.transactions[id]
		if !ok {
			break
		}
	}
	info := &transaction{
		trans:      memstore.NewTransaction(),
		expires:    time.Now().Add(transactionMaxLifetime),
		creatorCtx: ctx,
	}
	s.transactions[id] = info
	return makeTransactionComponent(id), nil
}

// findTransaction returns the transaction for the given transaction ID.
func (s *Server) findTransaction(ctx transactionContext, id transactionID) (*memstore.Transaction, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.findTransactionLocked(ctx, id)
}

func (s *Server) findTransactionLocked(ctx transactionContext, id transactionID) (*memstore.Transaction, error) {
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

// Commit commits the changes in the transaction to the store.  The
// operation is atomic, so all mutations are performed, or none.  Returns an
// error if the transaction aborted.
func (s *Server) commitTransaction(ctx transactionContext, id transactionID) error {
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

// Abort discards a transaction.  This is an optimization; transactions
// eventually time out and get discarded.  However, live transactions
// consume resources, so if you know that you won't be using a transaction
// anymore, you should discard it explicitly.
func (s *Server) abortTransaction(ctx transactionContext, id transactionID) error {
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

// Watch returns a stream of all changes.
func (s *Server) Watch(ctx ipc.ServerContext, req raw.Request, stream raw.StoreServiceWatchStream) error {
	return s.watcher.WatchRaw(ctx, req, stream)
}

// PutMutations atomically commits a stream of Mutations when the stream is
// closed. Mutations are not committed if the request is cancelled before the
// stream has been closed.
func (s *Server) PutMutations(ctx ipc.ServerContext, stream raw.StoreServicePutMutationsStream) error {
	return s.store.PutMutations(ctx, stream)
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
	serv, err := d.lookupServer(suffix)
	if err != nil {
		return nil, nil, err
	}
	return ipc.ReflectInvoker(serv), d.auth, nil
}

func (d *storeDispatcher) lookupServer(suffix string) (interface{}, error) {
	// Strip leading "/" if present so that server internals can reliably use
	// naming.Join(suffix, "foo").
	suffix = strings.TrimPrefix(suffix, "/")
	if strings.HasSuffix(suffix, raw.RawStoreSuffix) {
		return raw.NewServerStore(d.s), nil
	} else {
		// TODO(sadovsky): Create Object, Transaction, and TransactionRoot stubs,
		// merge them, and return the result. See TODO in
		// veyron2/services/store/service.vdl.
		o, err := d.s.lookupObject(suffix)
		if err != nil {
			return nil, err
		}
		return store.NewServerObject(o), nil
	}
}

func (s *Server) lookupObject(name string) (*object, error) {
	oname, tid, err := stripTransactionComponent(name)
	if err != nil {
		return nil, err
	}
	return &object{name: oname, obj: s.store.Bind(oname), tid: tid, server: s}, nil
}
