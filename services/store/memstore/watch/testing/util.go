package blackbox

import (
	"errors"
	"sync"
	"time"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/services/watch"
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

func Watch(id security.PublicID, watchFn func(ipc.ServerContext, watch.Request, watch.WatcherServiceWatchStream) error, req watch.Request) watch.WatcherWatchStream {
	mu := &sync.Mutex{}
	ctx := NewCancellableContext(id)
	c := make(chan watch.ChangeBatch, 1)
	errc := make(chan error, 1)
	go func() {
		err := watchFn(ctx, req, &watcherServiceWatchStream{
			mu:     mu,
			ctx:    ctx,
			output: c,
		})
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
