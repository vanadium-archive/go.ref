package vtrace

import (
	"sync"
	"time"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/uniqueid"
	"veyron.io/veyron/veyron2/vtrace"
)

func copySpanRecord(in *vtrace.SpanRecord) *vtrace.SpanRecord {
	return &vtrace.SpanRecord{
		ID:          in.ID,
		Parent:      in.Parent,
		Name:        in.Name,
		Start:       in.Start,
		End:         in.End,
		Annotations: append([]vtrace.Annotation{}, in.Annotations...),
	}
}

// collectors collect spans and annotations for output or analysis.
// collectors are safe to use from multiple goroutines simultaneously.
// TODO(mattr): collector should support log-based collection
// as well as in-memory collection.
type collector struct {
	traceID uniqueid.ID
	method  vtrace.TraceMethod
	spans   map[uniqueid.ID]*vtrace.SpanRecord
	mu      sync.Mutex
}

// newCollector returns a new collector for the given traceID.
func newCollector(traceID uniqueid.ID) *collector {
	return &collector{
		traceID: traceID,
		method:  vtrace.None,
	}
}

// ID returns the ID of the trace this collector is collecting for.
func (c *collector) ID() uniqueid.ID {
	return c.traceID
}

// ForceCollect turns on collection for this trace.  If collection
// is already active, this does nothing.
func (c *collector) ForceCollect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.method != vtrace.InMemory {
		c.method = vtrace.InMemory
		c.spans = make(map[uniqueid.ID]*vtrace.SpanRecord)
	}
}

func (c *collector) spanRecordLocked(s *span) *vtrace.SpanRecord {
	sid := s.ID()
	record, ok := c.spans[sid]
	if !ok {
		record = &vtrace.SpanRecord{
			ID:     sid,
			Parent: s.parent,
			Name:   s.name,
			Start:  s.start.UnixNano(),
		}
		c.spans[sid] = record
	}
	return record
}

// start records the fact that a given span has begun.
func (c *collector) start(s *span) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.method == vtrace.InMemory {
		// Note that simply fetching the record is enough since
		// if the record does not exist we will created it according
		// to the start time in s.
		c.spanRecordLocked(s)
	}
}

// finish records the time that a span finished.
func (c *collector) finish(s *span) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.method == vtrace.InMemory {
		record := c.spanRecordLocked(s)
		// TODO(mattr): Perhaps we should log an error if we have already been finished?
		record.End = time.Now().UnixNano()
	}
}

// annotate adds a span annotation to the collection.
func (c *collector) annotate(s *span, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.method == vtrace.InMemory {
		record := c.spanRecordLocked(s)
		record.Annotations = append(record.Annotations, vtrace.Annotation{
			When:    time.Now().UnixNano(),
			Message: msg,
		})
	}
}

// response computes a vtrace.Response for the current trace.
func (c *collector) response() vtrace.Response {
	c.mu.Lock()
	defer c.mu.Unlock()
	return vtrace.Response{
		Method: c.method,
		Trace:  c.traceRecordLocked(),
	}
}

// Record computes a vtrace.TraceRecord containing all annotations
// collected so far.
func (c *collector) Record() vtrace.TraceRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.traceRecordLocked()
}

func (c *collector) traceRecordLocked() vtrace.TraceRecord {
	spans := make([]vtrace.SpanRecord, 0, len(c.spans))
	for _, span := range c.spans {
		spans = append(spans, *copySpanRecord(span))
	}
	return vtrace.TraceRecord{
		ID:    c.traceID,
		Spans: spans,
	}
}

// merge merges a vtrace.Response into the current trace.
func (c *collector) merge(parent vtrace.Span, t *vtrace.Response) {
	if t.Method == vtrace.InMemory {
		c.ForceCollect()
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO(mattr): We need to carefully merge here to correct for
	// clock skew and ordering.  We should estimate the clock skew
	// by assuming that children of parent need to start after parent
	// and end before now.
	for _, span := range t.Trace.Spans {
		c.spans[span.ID] = copySpanRecord(&span)
	}
}

// MergeResponse merges a vtrace.Response into the current trace.
func MergeResponse(ctx context.T, response *vtrace.Response) {
	if span := getSpan(ctx); span != nil {
		span.collector.merge(span, response)
	}
}
