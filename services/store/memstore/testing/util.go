package testing

import (
	"io"
	"runtime"
	"sync"
	"testing"

	"veyron/services/store/raw"

	"veyron2/context"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/watch"
	"veyron2/storage"
)

// FakeServerContext implements ipc.ServerContext.
type FakeServerContext struct {
	context.T
	cancel context.CancelFunc
	id     security.PublicID
}

func NewFakeServerContext(id security.PublicID) *FakeServerContext {
	ctx, cancel := rt.R().NewContext().WithCancel()

	return &FakeServerContext{
		T:      ctx,
		cancel: cancel,
		id:     id,
	}
}

func (*FakeServerContext) Server() ipc.Server                            { return nil }
func (*FakeServerContext) Method() string                                { return "" }
func (*FakeServerContext) Name() string                                  { return "" }
func (*FakeServerContext) Suffix() string                                { return "" }
func (*FakeServerContext) Label() (l security.Label)                     { return }
func (*FakeServerContext) CaveatDischarges() security.CaveatDischargeMap { return nil }
func (ctx *FakeServerContext) LocalID() security.PublicID                { return ctx.id }
func (ctx *FakeServerContext) RemoteID() security.PublicID               { return ctx.id }
func (*FakeServerContext) Blessing() security.PublicID                   { return nil }
func (*FakeServerContext) LocalEndpoint() naming.Endpoint                { return nil }
func (*FakeServerContext) RemoteEndpoint() naming.Endpoint               { return nil }
func (ctx *FakeServerContext) Cancel()                                   { ctx.cancel() }

// Utilities for PutMutations.

// storeServicePutMutationsStream implements raw.StoreServicePutMutationsStream
type storeServicePutMutationsStream struct {
	mus   <-chan raw.Mutation
	value raw.Mutation
}

func (s *storeServicePutMutationsStream) RecvStream() interface {
	Advance() bool
	Value() raw.Mutation
	Err() error
} {
	return s
}

func (s *storeServicePutMutationsStream) Advance() bool {
	var ok bool
	s.value, ok = <-s.mus
	return ok
}

func (s *storeServicePutMutationsStream) Value() raw.Mutation {
	return s.value
}

func (s *storeServicePutMutationsStream) Err() error {
	return nil
}

// storePutMutationsStream implements raw.StorePutMutationsStream
type storePutMutationsStream struct {
	closed bool
	mus    chan<- raw.Mutation
}

func (s *storePutMutationsStream) Send(mu raw.Mutation) error {
	s.mus <- mu
	return nil
}

func (s *storePutMutationsStream) Close() error {
	if !s.closed {
		s.closed = true
		close(s.mus)
	}
	return nil
}

type storePutMutationsCall struct {
	ctx    ipc.ServerContext
	stream storePutMutationsStream
	err    <-chan error
}

func (s *storePutMutationsCall) SendStream() interface {
	Send(mu raw.Mutation) error
	Close() error
} {
	return &s.stream
}

func (s *storePutMutationsCall) Finish() error {
	s.stream.Close()
	return <-s.err
}

func (s *storePutMutationsCall) Cancel() {
	s.ctx.(*FakeServerContext).Cancel()
	s.stream.Close()
}

func PutMutations(id security.PublicID, putMutationsFn func(ipc.ServerContext, raw.StoreServicePutMutationsStream) error) raw.StorePutMutationsCall {
	ctx := NewFakeServerContext(id)
	mus := make(chan raw.Mutation)
	err := make(chan error)
	go func() {
		err <- putMutationsFn(ctx, &storeServicePutMutationsStream{mus: mus})
		close(err)
	}()
	return &storePutMutationsCall{
		ctx: ctx,
		err: err,
		stream: storePutMutationsStream{
			mus: mus,
		},
	}
}

func PutMutationsBatch(t *testing.T, id security.PublicID, putMutationsFn func(ipc.ServerContext, raw.StoreServicePutMutationsStream) error, mus []raw.Mutation) {
	storePutMutationsStream := PutMutations(id, putMutationsFn)
	for _, mu := range mus {
		storePutMutationsStream.SendStream().Send(mu)
	}
	if err := storePutMutationsStream.Finish(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't put mutations %s: %s", file, line, mus, err)
	}
}

// Utilities for Watch.

// watcherServiceWatchStreamSender implements watch.WatcherServiceWatchStreamSender
type watcherServiceWatchStreamSender struct {
	mu     *sync.Mutex
	ctx    ipc.ServerContext
	output chan<- watch.ChangeBatch
}

func (s *watcherServiceWatchStreamSender) Send(cb watch.ChangeBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.output <- cb:
		return nil
	case <-s.ctx.Done():
		return io.EOF
	}
}

// watcherServiceWatchStream implements watch.WatcherServiceWatchStream
type watcherServiceWatchStream struct {
	watcherServiceWatchStreamSender
}

func (s *watcherServiceWatchStream) SendStream() interface {
	Send(cb watch.ChangeBatch) error
} {
	return s
}
func (*watcherServiceWatchStream) Cancel() {}

// watcherWatchStream implements watch.WatcherWatchStream.
type watcherWatchStream struct {
	ctx   *FakeServerContext
	value watch.ChangeBatch
	input <-chan watch.ChangeBatch
	err   <-chan error
}

func (s *watcherWatchStream) Advance() bool {
	var ok bool
	s.value, ok = <-s.input
	return ok
}

func (s *watcherWatchStream) Value() watch.ChangeBatch {
	return s.value
}

func (*watcherWatchStream) Err() error {
	return nil
}

func (s *watcherWatchStream) Finish() error {
	<-s.input
	return <-s.err
}

func (s *watcherWatchStream) Cancel() {
	s.ctx.Cancel()
}

func (s *watcherWatchStream) RecvStream() interface {
	Advance() bool
	Value() watch.ChangeBatch
	Err() error
} {
	return s
}

func watchImpl(id security.PublicID, watchFn func(ipc.ServerContext, *watcherServiceWatchStream) error) *watcherWatchStream {
	mu := &sync.Mutex{}
	ctx := NewFakeServerContext(id)
	c := make(chan watch.ChangeBatch, 1)
	errc := make(chan error, 1)
	go func() {
		stream := &watcherServiceWatchStream{
			watcherServiceWatchStreamSender{
				mu:     mu,
				ctx:    ctx,
				output: c,
			},
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

func WatchRaw(id security.PublicID, watchFn func(ipc.ServerContext, raw.Request, raw.StoreServiceWatchStream) error, req raw.Request) raw.StoreWatchCall {
	return watchImpl(id, func(ctx ipc.ServerContext, stream *watcherServiceWatchStream) error {
		return watchFn(ctx, req, stream)
	})
}

func WatchGlob(id security.PublicID, watchFn func(ipc.ServerContext, watch.GlobRequest, watch.GlobWatcherServiceWatchGlobStream) error, req watch.GlobRequest) watch.GlobWatcherWatchGlobCall {
	return watchImpl(id, func(ctx ipc.ServerContext, iterator *watcherServiceWatchStream) error {
		return watchFn(ctx, req, iterator)
	})
}

func WatchGlobOnPath(id security.PublicID, watchFn func(ipc.ServerContext, storage.PathName, watch.GlobRequest, watch.GlobWatcherServiceWatchGlobStream) error, path storage.PathName, req watch.GlobRequest) watch.GlobWatcherWatchGlobCall {
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
	cv, ok := change.Value.(*storage.Entry)
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
