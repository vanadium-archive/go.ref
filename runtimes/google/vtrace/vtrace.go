// Package vtrace implements the Trace and Span interfaces in veyron2/vtrace.
// We also provide internal utilities for migrating trace information across
// IPC calls.
package vtrace

import (
	"fmt"
	"time"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/uniqueid"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"
)

// A span represents an annotated period of time.
type span struct {
	id        uniqueid.ID
	parent    uniqueid.ID
	name      string
	collector *collector
	start     time.Time
}

func newSpan(parent uniqueid.ID, name string, collector *collector) *span {
	id, err := uniqueid.Random()
	if err != nil {
		vlog.Errorf("vtrace: Couldn't generate Span ID, debug data may be lost: %v", err)
	}
	s := &span{
		id:        id,
		parent:    parent,
		name:      name,
		collector: collector,
		start:     time.Now(),
	}
	collector.start(s)
	return s
}

func (c *span) ID() uniqueid.ID     { return c.id }
func (c *span) Parent() uniqueid.ID { return c.parent }
func (c *span) Name() string        { return c.name }
func (c *span) Trace() vtrace.Trace { return c.collector }
func (c *span) Annotate(s string) {
	c.collector.annotate(c, s)
}
func (c *span) Annotatef(format string, a ...interface{}) {
	c.collector.annotate(c, fmt.Sprintf(format, a...))
}
func (c *span) Finish() { c.collector.finish(c) }

// Request generates a vtrace.Request from the active Span.
func Request(ctx context.T) vtrace.Request {
	if span := getSpan(ctx); span != nil {
		return vtrace.Request{
			SpanID:  span.id,
			TraceID: span.collector.traceID,
			Method:  span.collector.method,
		}
	}
	return vtrace.Request{}
}

// Response captures the vtrace.Response for the active Span.
func Response(ctx context.T) vtrace.Response {
	if span := getSpan(ctx); span != nil {
		return span.collector.response()
	}
	return vtrace.Response{}
}

// spanKey is uses to store and retrieve spans inside a context.T objects.
type spanKey struct{}

// ContinuedSpan creates a span that represents a continuation of a trace from
// a remote server.  name is a user readable string that describes the context
// and req contains the parameters needed to connect this span with it's trace.
func WithContinuedSpan(ctx context.T, name string, req vtrace.Request, store *Store) (context.T, vtrace.Span) {
	newSpan := newSpan(req.SpanID, name, newCollector(req.TraceID, store))
	if req.Method == vtrace.InMemory {
		newSpan.collector.ForceCollect()
	}
	return ctx.WithValue(spanKey{}, newSpan), newSpan
}

func WithNewRootSpan(ctx context.T, store *Store, forceCollect bool) (context.T, vtrace.Span) {
	id, err := uniqueid.Random()
	if err != nil {
		vlog.Errorf("vtrace: Couldn't generate Trace ID, debug data may be lost: %v", err)
	}
	col := newCollector(id, store)
	if forceCollect {
		col.ForceCollect()
	}
	s := newSpan(id, "", col)

	return ctx.WithValue(spanKey{}, s), s
}

// NewSpan creates a new span.
func WithNewSpan(parent context.T, name string) (context.T, vtrace.Span) {
	if curSpan := getSpan(parent); curSpan != nil {
		s := newSpan(curSpan.ID(), name, curSpan.collector)
		return parent.WithValue(spanKey{}, s), s
	}

	vlog.Error("vtrace: Creating a new child span from context with no existing span.")
	return WithNewRootSpan(parent, nil, false)
}

func getSpan(ctx context.T) *span {
	span, _ := ctx.Value(spanKey{}).(*span)
	return span
}

// GetSpan returns the active span from the context.
func FromContext(ctx context.T) vtrace.Span {
	span, _ := ctx.Value(spanKey{}).(vtrace.Span)
	return span
}
