package vtrace

import (
	"encoding/binary"
	"reflect"
	"testing"

	"v.io/veyron/veyron2/uniqueid"
	"v.io/veyron/veyron2/vtrace"
)

var nextid = uint64(1)

func id() uniqueid.ID {
	var out uniqueid.ID
	binary.BigEndian.PutUint64(out[8:], nextid)
	nextid++
	return out
}

func makeTraces(n int, st *Store) []vtrace.Trace {
	traces := make([]vtrace.Trace, n)
	for i := range traces {
		traces[i] = newCollector(id(), st)
		traces[i].ForceCollect()
	}
	return traces
}

func recordids(records ...vtrace.TraceRecord) map[uniqueid.ID]bool {
	out := make(map[uniqueid.ID]bool)
	for _, trace := range records {
		out[trace.ID] = true
	}
	return out
}

func traceids(traces ...vtrace.Trace) map[uniqueid.ID]bool {
	out := make(map[uniqueid.ID]bool)
	for _, trace := range traces {
		out[trace.ID()] = true
	}
	return out
}

func TestConsiderAndTrim(t *testing.T) {
	st := NewStore(5)
	traces := makeTraces(10, st)
	records := st.TraceRecords()

	if want, got := traceids(traces[5:]...), recordids(records...); !reflect.DeepEqual(want, got) {
		t.Errorf("Got wrong traces.  Want %#v, got %#v", want, got)
	}

	// Starting a new span on one of the traces should bring it back into the stored set.
	traces[2].(*collector).start(&span{id: id()})
	records = st.TraceRecords()
	if want, got := traceids(traces[2], traces[6], traces[7], traces[8], traces[9]), recordids(records...); !reflect.DeepEqual(want, got) {
		t.Errorf("Got wrong traces.  Want %#v, got %#v", want, got)
	}

	// Starting a new span on one of the traces should bring it back into the stored set.
	traces[2].(*collector).start(&span{id: id()})
	records = st.TraceRecords()
	if want, got := traceids(traces[2], traces[6], traces[7], traces[8], traces[9]), recordids(records...); !reflect.DeepEqual(want, got) {
		t.Errorf("Got wrong traces.  Want %#v, got %#v", want, got)
	}

	// Finishing a span on one of the traces should bring it back into the stored set.
	traces[3].(*collector).finish(&span{id: id()})
	records = st.TraceRecords()
	if want, got := traceids(traces[3], traces[2], traces[7], traces[8], traces[9]), recordids(records...); !reflect.DeepEqual(want, got) {
		t.Errorf("Got wrong traces.  Want %#v, got %#v", want, got)
	}

	// Annotating a span on one of the traces should bring it back into the stored set.
	traces[4].(*collector).annotate(&span{id: id()}, "hello")
	records = st.TraceRecords()
	if want, got := traceids(traces[4], traces[3], traces[2], traces[8], traces[9]), recordids(records...); !reflect.DeepEqual(want, got) {
		t.Errorf("Got wrong traces.  Want %#v, got %#v", want, got)
	}
}
