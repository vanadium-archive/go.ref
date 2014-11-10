package vtrace

import (
	"sync"

	"veyron.io/veyron/veyron2/uniqueid"
	"veyron.io/veyron/veyron2/vtrace"
)

// Store implements a store for traces.  The idea is to keep all the
// information we have about some subset of traces that pass through
// the server.  For now we just implement an LRU cache, so the least
// recently started/finished/annotated traces expire after some
// maximum trace count is reached.
// TODO(mattr): LRU is the wrong policy in the long term, we should
// try to keep some diverse set of traces and allow users to
// specifically tell us to capture a specific trace.  LRU will work OK
// for many testing scenarios and low volume applications.
type Store struct {
	size int

	// traces and head together implement a linked-hash-map.
	// head points to the head and tail of the doubly-linked-list
	// of recently used items (the tail is the LRU traceSet).
	mu     sync.Mutex
	traces map[uniqueid.ID]*traceSet // GUARDED_BY(mu)
	head   *traceSet                 // GUARDED_BY(mu)
}

// NewStore creates a new store that will keep a maximum of size
// traces in memory.
// TODO(mattr): Traces can be of widely varying size, we should have
// some better measurement then just number of traces.
func NewStore(size int) *Store {
	head := &traceSet{}
	head.next, head.prev = head, head

	return &Store{
		size:   size,
		traces: make(map[uniqueid.ID]*traceSet),
		head:   head,
	}
}

// Consider should be called whenever an interesting change happens to
// a trace the store will decide whether to keep it or not.
func (s *Store) Consider(trace vtrace.Trace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.traces[trace.ID()]
	if set == nil {
		set = newTraceSet()
		s.traces[trace.ID()] = set
	}
	set.add(trace)
	set.moveAfter(s.head)
	s.trimLocked()
}

// TraceRecords returns TraceRecords for all traces saved in the store.
func (s *Store) TraceRecords() []*vtrace.TraceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*vtrace.TraceRecord, s.size)
	i := 0
	for _, ts := range s.traces {
		out[i] = ts.traceRecord()
		i++
	}
	return out
}

// TraceRecord returns a TraceRecord for a given uniqueid.  Returns
// nil if the given id is not present.
func (s *Store) TraceRecord(id uniqueid.ID) *vtrace.TraceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.traces[id]
	if ts == nil {
		return nil
	}
	return ts.traceRecord()
}

// trimLocked removes elements from the store LRU first until we are
// below Store.size.
func (s *Store) trimLocked() {
	for len(s.traces) > s.size {
		el := s.head.prev
		el.removeFromList()
		delete(s.traces, el.id())
	}
}

// We need to capture traceSets because a single trace can reach this
// server along multiple paths.  Consider a client that calls this
// server twice in the same operation.
type traceSet struct {
	elts       map[vtrace.Trace]bool
	prev, next *traceSet
}

func newTraceSet() *traceSet {
	return &traceSet{elts: make(map[vtrace.Trace]bool)}
}

func (ts *traceSet) add(trace vtrace.Trace) {
	ts.elts[trace] = true
}

func (ts *traceSet) removeFromList() {
	if ts.prev != nil {
		ts.prev.next = ts.next
	}
	if ts.next != nil {
		ts.next.prev = ts.prev
	}
	ts.next = nil
	ts.prev = nil
}

func (ts *traceSet) moveAfter(prev *traceSet) {
	ts.removeFromList()
	ts.prev = prev
	ts.next = prev.next
	prev.next.prev = ts
	prev.next = ts
}

func (ts *traceSet) id() uniqueid.ID {
	for e := range ts.elts {
		return e.ID()
	}
	panic("unreachable")
}

func (ts *traceSet) traceRecord() *vtrace.TraceRecord {
	var out vtrace.TraceRecord

	// It is possible to have duplicate copies of spans.  Consider the
	// case where a server calls itself (even indirectly) we'll have one
	// Trace in the set for the parent call and one Trace in the set for
	// the decendant.  The two records will be exactly the same we
	// therefore de-dup here.
	spans := make(map[uniqueid.ID]bool)

	for e, _ := range ts.elts {
		record := e.Record()
		out.ID = record.ID
		for _, span := range record.Spans {
			if spans[span.ID] {
				continue
			}
			spans[span.ID] = true
			out.Spans = append(out.Spans, span)
		}
	}
	return &out
}
