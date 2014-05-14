package memstore

import (
	"io"
	"runtime"
	"testing"
	"time"

	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/storage"
)

// cancellableContext implements ipc.Context.
type cancellableContext struct {
	cancelled chan struct{}
}

func newCancellableContext() *cancellableContext {
	return &cancellableContext{cancelled: make(chan struct{})}
}

func (*cancellableContext) Server() ipc.Server {
	return nil
}

func (*cancellableContext) Method() string {
	return ""
}

func (*cancellableContext) Name() string {
	return ""
}

func (*cancellableContext) Suffix() string {
	return ""
}

func (*cancellableContext) Label() (l security.Label) {
	return
}

func (*cancellableContext) CaveatDischarges() security.CaveatDischargeMap {
	return nil
}

func (*cancellableContext) LocalID() security.PublicID {
	return rootPublicID
}

func (*cancellableContext) RemoteID() security.PublicID {
	return rootPublicID
}

func (*cancellableContext) Deadline() (t time.Time) {
	return
}

func (ctx *cancellableContext) IsClosed() bool {
	select {
	case <-ctx.cancelled:
		return true
	default:
		return false
	}
}

func (ctx *cancellableContext) Closed() <-chan struct{} {
	return ctx.cancelled
}

// cancel synchronously closes the context. After cancel returns, calls to
// IsClosed will return true and the stream returned by Closed will be closed.
func (ctx *cancellableContext) cancel() {
	close(ctx.cancelled)
}

// serverStream implements raw.StoreServicePutMutationsStream
type serverStream struct {
	mus <-chan raw.Mutation
}

func (s *serverStream) Recv() (raw.Mutation, error) {
	mu, ok := <-s.mus
	if !ok {
		return mu, io.EOF
	}
	return mu, nil
}

// clientStream implements raw.StorePutMutationsStream
type clientStream struct {
	ctx    ipc.Context
	closed bool
	mus    chan<- raw.Mutation
	err    <-chan error
}

func (s *clientStream) Send(mu raw.Mutation) error {
	s.mus <- mu
	return nil
}

func (s *clientStream) CloseSend() error {
	if !s.closed {
		s.closed = true
		close(s.mus)
	}
	return nil
}

func (s *clientStream) Finish() error {
	s.CloseSend()
	return <-s.err
}

func (s *clientStream) Cancel() {
	s.ctx.(*cancellableContext).cancel()
	s.CloseSend()
}

func putMutations(st *Store) raw.StorePutMutationsStream {
	ctx := newCancellableContext()
	mus := make(chan raw.Mutation)
	err := make(chan error)
	go func() {
		err <- st.PutMutations(ctx, &serverStream{mus})
		close(err)
	}()
	return &clientStream{
		ctx: ctx,
		mus: mus,
		err: err,
	}
}

func putMutationsBatch(t *testing.T, st *Store, mus []raw.Mutation) {
	clientStream := putMutations(st)
	for _, mu := range mus {
		clientStream.Send(mu)
	}
	if err := clientStream.Finish(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't put mutations %s: %s", file, line, mus, err)
	}
}

func mkdir(t *testing.T, st *Store, tr storage.Transaction, path string) (storage.ID, interface{}) {
	_, file, line, _ := runtime.Caller(1)
	dir := &Dir{}
	stat, err := st.Bind(path).Put(rootPublicID, tr, dir)
	if err != nil || stat == nil {
		t.Errorf("%s(%d): mkdir %s: %s", file, line, path, err)
	}
	return stat.ID, dir
}

func get(t *testing.T, st *Store, tr storage.Transaction, path string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e.Value
}

func put(t *testing.T, st *Store, tr storage.Transaction, path string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	stat, err := st.Bind(path).Put(rootPublicID, tr, v)
	if err != nil {
		t.Errorf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	if _, err := st.Bind(path).Get(rootPublicID, tr); err != nil {
		t.Errorf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	if stat != nil {
		return stat.ID
	}
	return storage.ID{}
}

func remove(t *testing.T, st *Store, tr storage.Transaction, path string) {
	if err := st.Bind(path).Remove(rootPublicID, tr); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func commit(t *testing.T, tr storage.Transaction) {
	if err := tr.Commit(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s(%d): Transaction aborted: %s", file, line, err)
	}
}

func expectExists(t *testing.T, st *Store, tr storage.Transaction, path string) {
	_, file, line, _ := runtime.Caller(1)
	if ok, _ := st.Bind(path).Exists(rootPublicID, tr); !ok {
		t.Errorf("%s(%d): does not exist: %s", file, line, path)
	}
}

func expectNotExists(t *testing.T, st *Store, tr storage.Transaction, path string) {
	if e, err := st.Bind(path).Get(rootPublicID, tr); err == nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): should not exist: %s: got %+v", file, line, path, e.Value)
	}
}

func expectValue(t *testing.T, st *Store, tr storage.Transaction, path string, v interface{}) {
	_, file, line, _ := runtime.Caller(1)
	e, err := st.Bind(path).Get(rootPublicID, tr)
	if err != nil {
		t.Errorf("%s(%d): does not exist: %s", file, line, path)
		return
	}
	if e.Value != v {
		t.Errorf("%s(%d): expected %+v, got %+v", file, line, e.Value, v)
	}

}
