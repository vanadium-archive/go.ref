// Package vtrace implements the Trace and Span interfaces in veyron2/vtrace.
// We also provide internal utilities for migrating trace information across
// IPC calls.
package vtrace

import (
	"veyron2/context"
	"veyron2/uniqueid"
	"veyron2/vlog"
	"veyron2/vtrace"
)

// A span represents an annotated period of time.
type span struct {
	id        uniqueid.ID
	parent    uniqueid.ID
	name      string
	collector *collector
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
	}
	s.Annotate("Started")
	return s
}

func (c *span) ID() uniqueid.ID     { return c.id }
func (c *span) Parent() uniqueid.ID { return c.parent }
func (c *span) Name() string        { return c.name }
func (c *span) Trace() vtrace.Trace { return c.collector }
func (c *span) Annotate(msg string) { c.collector.annotate(c, msg) }

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
func WithContinuedSpan(ctx context.T, name string, req vtrace.Request) (context.T, vtrace.Span) {
	newSpan := newSpan(req.SpanID, name, newCollector(req.TraceID))
	if req.Method == vtrace.InMemory {
		newSpan.collector.ForceCollect()
	}
	return ctx.WithValue(spanKey{}, newSpan), newSpan
}

// NewSpan creates a new span.
func WithNewSpan(parent context.T, name string) (context.T, vtrace.Span) {
	var s *span
	if curSpan := getSpan(parent); curSpan != nil {
		s = newSpan(curSpan.ID(), name, curSpan.collector)
	} else {
		id, err := uniqueid.Random()
		if err != nil {
			vlog.Errorf("vtrace: Couldn't generate Trace ID, debug data may be lost: %v", err)
		}
		s = newSpan(id, name, newCollector(id))
	}
	return parent.WithValue(spanKey{}, s), s
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
