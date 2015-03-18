// Package vtrace implements the Trace and Span interfaces in veyron2/vtrace.
// We also provide internal utilities for migrating trace information across
// RPC calls.
package vtrace

import (
	"fmt"
	"time"

	"v.io/v23/context"
	"v.io/v23/uniqueid"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/flags"
)

// A span represents an annotated period of time.
type span struct {
	id     uniqueid.Id
	parent uniqueid.Id
	name   string
	trace  uniqueid.Id
	start  time.Time
	store  *Store
}

func newSpan(parent uniqueid.Id, name string, trace uniqueid.Id, store *Store) *span {
	id, err := uniqueid.Random()
	if err != nil {
		vlog.Errorf("vtrace: Couldn't generate Span ID, debug data may be lost: %v", err)
	}
	s := &span{
		id:     id,
		parent: parent,
		name:   name,
		trace:  trace,
		start:  time.Now(),
		store:  store,
	}
	store.start(s)
	return s
}

func (s *span) ID() uniqueid.Id     { return s.id }
func (s *span) Parent() uniqueid.Id { return s.parent }
func (s *span) Name() string        { return s.name }
func (s *span) Trace() uniqueid.Id  { return s.trace }
func (s *span) Annotate(msg string) {
	s.store.annotate(s, msg)
}
func (s *span) Annotatef(format string, a ...interface{}) {
	s.store.annotate(s, fmt.Sprintf(format, a...))
}
func (s *span) Finish() {
	s.store.finish(s)
}
func (s *span) flags() vtrace.TraceFlags {
	return s.store.flags(s.trace)
}

type contextKey int

const (
	storeKey = contextKey(iota)
	spanKey
)

// Manager allows you to create new traces and spans and access the
// vtrace store.
type manager struct{}

// SetNewTrace creates a new vtrace context that is not the child of any
// other span.  This is useful when starting operations that are
// disconnected from the activity ctx is performing.  For example
// this might be used to start background tasks.
func (m manager) SetNewTrace(ctx *context.T) (*context.T, vtrace.Span) {
	id, err := uniqueid.Random()
	if err != nil {
		vlog.Errorf("vtrace: Couldn't generate Trace Id, debug data may be lost: %v", err)
	}
	s := newSpan(id, "", id, getStore(ctx))

	return context.WithValue(ctx, spanKey, s), s
}

// SetContinuedTrace creates a span that represents a continuation of
// a trace from a remote server.  name is the name of the new span and
// req contains the parameters needed to connect this span with it's
// trace.
func (m manager) SetContinuedTrace(ctx *context.T, name string, req vtrace.Request) (*context.T, vtrace.Span) {
	st := getStore(ctx)
	if req.Flags&vtrace.CollectInMemory != 0 {
		st.ForceCollect(req.TraceId)
	}
	newSpan := newSpan(req.SpanId, name, req.TraceId, st)
	return context.WithValue(ctx, spanKey, newSpan), newSpan
}

// SetNewSpan derives a context with a new Span that can be used to
// trace and annotate operations across process boundaries.
func (m manager) SetNewSpan(ctx *context.T, name string) (*context.T, vtrace.Span) {
	if curSpan := getSpan(ctx); curSpan != nil {
		if curSpan.store == nil {
			panic("nil store")
		}
		s := newSpan(curSpan.ID(), name, curSpan.trace, curSpan.store)
		return context.WithValue(ctx, spanKey, s), s
	}

	vlog.Error("vtrace: Creating a new child span from context with no existing span.")
	return m.SetNewTrace(ctx)
}

// Span finds the currently active span.
func (m manager) GetSpan(ctx *context.T) vtrace.Span {
	if span := getSpan(ctx); span != nil {
		return span
	}
	return nil
}

// Request generates a vtrace.Request from the active Span.
func (m manager) GetRequest(ctx *context.T) vtrace.Request {
	if span := getSpan(ctx); span != nil {
		return vtrace.Request{
			SpanId:  span.id,
			TraceId: span.trace,
			Flags:   span.flags(),
		}
	}
	return vtrace.Request{}
}

// Response captures the vtrace.Response for the active Span.
func (m manager) GetResponse(ctx *context.T) vtrace.Response {
	if span := getSpan(ctx); span != nil {
		return vtrace.Response{
			Flags: span.flags(),
			Trace: *span.store.TraceRecord(span.trace),
		}
	}
	return vtrace.Response{}
}

// Store returns the current vtrace.Store.
func (m manager) GetStore(ctx *context.T) vtrace.Store {
	if store := getStore(ctx); store != nil {
		return store
	}
	return nil
}

// getSpan returns the internal span type.
func getSpan(ctx *context.T) *span {
	span, _ := ctx.Value(spanKey).(*span)
	return span
}

// GetStore returns the *Store attached to the context.
func getStore(ctx *context.T) *Store {
	store, _ := ctx.Value(storeKey).(*Store)
	return store
}

// Init initializes vtrace and attaches some state to the context.
// This should be called by the runtimes initialization function.
func Init(ctx *context.T, opts flags.VtraceFlags) (*context.T, error) {
	nctx := vtrace.WithManager(ctx, manager{})
	store, err := NewStore(opts)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(nctx, storeKey, store), nil
}
