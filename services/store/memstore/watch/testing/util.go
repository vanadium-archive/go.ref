package blackbox

import (
	"errors"
	"sync"
	"testing"
	"time"

	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/storage"
)

var (
	ErrStreamClosed = errors.New("stream closed")
)

// CancellableContext implements ipc.ServerContext.
type CancellableContext struct {
	id        security.PublicID
	mu        sync.Mutex
	cancelled chan struct{}
}

func NewCancellableContext(id security.PublicID) *CancellableContext {
	return &CancellableContext{
		id:        id,
		cancelled: make(chan struct{}),
	}
}

func (*CancellableContext) Server() ipc.Server {
	return nil
}

func (*CancellableContext) Method() string {
	return ""
}

func (*CancellableContext) Name() string {
	return ""
}

func (*CancellableContext) Suffix() string {
	return ""
}

func (*CancellableContext) Label() (l security.Label) {
	return
}

func (*CancellableContext) CaveatDischarges() security.CaveatDischargeMap {
	return nil
}

func (ctx *CancellableContext) LocalID() security.PublicID {
	return ctx.id
}

func (ctx *CancellableContext) RemoteID() security.PublicID {
	return ctx.id
}

func (*CancellableContext) Blessing() security.PublicID {
	return nil
}

func (*CancellableContext) LocalEndpoint() naming.Endpoint {
	return nil
}

func (*CancellableContext) RemoteEndpoint() naming.Endpoint {
	return nil
}

func (*CancellableContext) Deadline() (t time.Time) {
	return
}

func (ctx *CancellableContext) IsClosed() bool {
	select {
	case <-ctx.cancelled:
		return true
	default:
		return false
	}
}

// cancel synchronously closes the context. After cancel returns, calls to
// IsClosed will return true and the stream returned by Closed will be closed.
func (ctx *CancellableContext) Cancel() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if !ctx.IsClosed() {
		close(ctx.cancelled)
	}
}

func (ctx *CancellableContext) Closed() <-chan struct{} {
	return ctx.cancelled
}

// watcherServiceWatchStream implements watch.WatcherServiceWatchStream.
type watcherServiceWatchStream struct {
	mu     *sync.Mutex
	ctx    ipc.ServerContext
	output chan<- watch.ChangeBatch
}

func (s *watcherServiceWatchStream) Send(cb watch.ChangeBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.output <- cb:
		return nil
	case <-s.ctx.Closed():
		return ErrStreamClosed
	}
}

// watcherWatchStream implements watch.WatcherWatchStream.
type watcherWatchStream struct {
	ctx   *CancellableContext
	input <-chan watch.ChangeBatch
	err   <-chan error
}

func (s *watcherWatchStream) Recv() (watch.ChangeBatch, error) {
	cb, ok := <-s.input
	if !ok {
		return cb, ErrStreamClosed
	}
	return cb, nil
}

func (s *watcherWatchStream) Finish() error {
	<-s.input
	return <-s.err
}

func (s *watcherWatchStream) Cancel() {
	s.ctx.Cancel()
}

func watchImpl(id security.PublicID, watchFn func(ipc.ServerContext, *watcherServiceWatchStream) error) *watcherWatchStream {
	mu := &sync.Mutex{}
	ctx := NewCancellableContext(id)
	c := make(chan watch.ChangeBatch, 1)
	errc := make(chan error, 1)
	go func() {
		stream := &watcherServiceWatchStream{
			mu:     mu,
			ctx:    ctx,
			output: c,
		}
		err := watchFn(ctx, stream)
		mu.Lock()
		defer mu.Unlock()
		ctx.Cancel()
		close(c)
		errc <- err
		close(errc)
	}()
	return &watcherWatchStream{
		ctx:   ctx,
		input: c,
		err:   errc,
	}
}

func WatchRaw(id security.PublicID, watchFn func(ipc.ServerContext, raw.Request, raw.StoreServiceWatchStream) error, req raw.Request) raw.StoreWatchStream {
	return watchImpl(id, func(ctx ipc.ServerContext, stream *watcherServiceWatchStream) error {
		return watchFn(ctx, req, stream)
	})
}

func WatchGlob(id security.PublicID, watchFn func(ipc.ServerContext, watch.GlobRequest, watch.GlobWatcherServiceWatchGlobStream) error, req watch.GlobRequest) watch.GlobWatcherWatchGlobStream {
	return watchImpl(id, func(ctx ipc.ServerContext, stream *watcherServiceWatchStream) error {
		return watchFn(ctx, req, stream)
	})
}

func WatchGlobOnPath(id security.PublicID, watchFn func(ipc.ServerContext, storage.PathName, watch.GlobRequest, watch.GlobWatcherServiceWatchGlobStream) error, path storage.PathName, req watch.GlobRequest) watch.GlobWatcherWatchGlobStream {
	return watchImpl(id, func(ctx ipc.ServerContext, stream *watcherServiceWatchStream) error {
		return watchFn(ctx, path, req, stream)
	})
}

func ExpectInitialStateSkipped(t *testing.T, change watch.Change) {
	if change.Name != "" {
		t.Fatalf("Expect Name to be \"\" but was: %v", change.Name)
	}
	if change.State != watch.InitialStateSkipped {
		t.Fatalf("Expect State to be InitialStateSkipped but was: %v", change.State)
	}
	if len(change.ResumeMarker) != 0 {
		t.Fatalf("Expect no ResumeMarker but was: %v", change.ResumeMarker)
	}
}

func ExpectEntryExists(t *testing.T, changes []watch.Change, name string, id storage.ID, value string) {
	change := findEntry(t, changes, name)
	if change.State != watch.Exists {
		t.Fatalf("Expected name to exist: %v", name)
	}
	cv, ok := change.Value.(*storage.Entry)
	if !ok {
		t.Fatal("Expected an Entry")
	}
	if cv.Stat.ID != id {
		t.Fatalf("Expected ID to be %v, but was: %v", id, cv.Stat.ID)
	}
	if cv.Value != value {
		t.Fatalf("Expected Value to be %v, but was: %v", value, cv.Value)
	}
}

func ExpectEntryDoesNotExist(t *testing.T, changes []watch.Change, name string) {
	change := findEntry(t, changes, name)
	if change.State != watch.DoesNotExist {
		t.Fatalf("Expected name to not exist: %v", name)
	}
	if change.Value != nil {
		t.Fatal("Expected entry to be nil")
	}
}

func ExpectServiceEntryExists(t *testing.T, changes []watch.Change, name string, id storage.ID, value string) {
	change := findEntry(t, changes, name)
	if change.State != watch.Exists {
		t.Fatalf("Expected name to exist: %v", name)
	}
	cv, ok := change.Value.(*store.Entry)
	if !ok {
		t.Fatal("Expected a service Entry")
	}
	if cv.Stat.ID != id {
		t.Fatalf("Expected ID to be %v, but was: %v", id, cv.Stat.ID)
	}
	if cv.Value != value {
		t.Fatalf("Expected Value to be %v, but was: %v", value, cv.Value)
	}
}

func ExpectServiceEntryDoesNotExist(t *testing.T, changes []watch.Change, name string) {
	change := findEntry(t, changes, name)
	if change.State != watch.DoesNotExist {
		t.Fatalf("Expected name to not exist: %v", name)
	}
	if change.Value != nil {
		t.Fatal("Expected entry to be nil")
	}
}

func findEntry(t *testing.T, changes []watch.Change, name string) watch.Change {
	for _, change := range changes {
		if change.Name == name {
			return change
		}
	}
	t.Fatalf("Expected a change for name: %v", name)
	panic("Should not reach here")
}

var (
	EmptyDir = []storage.DEntry{}
)

func DirOf(name string, id storage.ID) []storage.DEntry {
	return []storage.DEntry{storage.DEntry{
		Name: name,
		ID:   id,
	}}
}

func ExpectMutationExists(t *testing.T, changes []watch.Change, id storage.ID, pre, post storage.Version, isRoot bool, value string, dir []storage.DEntry) {
	change := findMutation(t, changes, id)
	if change.State != watch.Exists {
		t.Fatalf("Expected id to exist: %v", id)
	}
	cv := change.Value.(*raw.Mutation)
	if cv.PriorVersion != pre {
		t.Fatalf("Expected PriorVersion to be %v, but was: %v", pre, cv.PriorVersion)
	}
	if cv.Version != post {
		t.Fatalf("Expected Version to be %v, but was: %v", post, cv.Version)
	}
	if cv.IsRoot != isRoot {
		t.Fatalf("Expected IsRoot to be: %v, but was: %v", isRoot, cv.IsRoot)
	}
	if cv.Value != value {
		t.Fatalf("Expected Value to be: %v, but was: %v", value, cv.Value)
	}
	expectDirEquals(t, cv.Dir, dir)
}

func ExpectMutationDoesNotExist(t *testing.T, changes []watch.Change, id storage.ID, pre storage.Version, isRoot bool) {
	change := findMutation(t, changes, id)
	if change.State != watch.DoesNotExist {
		t.Fatalf("Expected id to not exist: %v", id)
	}
	cv := change.Value.(*raw.Mutation)
	if cv.PriorVersion != pre {
		t.Fatalf("Expected PriorVersion to be %v, but was: %v", pre, cv.PriorVersion)
	}
	if cv.Version != storage.NoVersion {
		t.Fatalf("Expected Version to be NoVersion, but was: %v", cv.Version)
	}
	if cv.IsRoot != isRoot {
		t.Fatalf("Expected IsRoot to be: %v, but was: %v", isRoot, cv.IsRoot)
	}
	if cv.Value != nil {
		t.Fatal("Expected Value to be nil")
	}
	if cv.Dir != nil {
		t.Fatal("Expected Dir to be nil")
	}
}

func ExpectMutationExistsNoVersionCheck(t *testing.T, changes []watch.Change, id storage.ID, value string) {
	change := findMutation(t, changes, id)
	if change.State != watch.Exists {
		t.Fatalf("Expected id to exist: %v", id)
	}
	cv := change.Value.(*raw.Mutation)
	if cv.Value != value {
		t.Fatalf("Expected Value to be: %v, but was: %v", value, cv.Value)
	}
}

func ExpectMutationDoesNotExistNoVersionCheck(t *testing.T, changes []watch.Change, id storage.ID) {
	change := findMutation(t, changes, id)
	if change.State != watch.DoesNotExist {
		t.Fatalf("Expected id to not exist: %v", id)
	}
}

func findMutation(t *testing.T, changes []watch.Change, id storage.ID) watch.Change {
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
	panic("Should not reach here")
}

func expectDirEquals(t *testing.T, actual, expected []storage.DEntry) {
	if len(actual) != len(expected) {
		t.Fatalf("Expected Dir to have %v refs, but had %v", len(expected), len(actual))
	}
	for i, e := range expected {
		a := actual[i]
		if a != e {
			t.Fatalf("Expected Dir entry %v to be %v, but was %v", i, e, a)
		}
	}
}
