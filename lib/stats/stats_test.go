package stats_test

import (
	"reflect"
	"testing"
	"time"

	libstats "veyron/lib/stats"
	"veyron/lib/stats/counter"
	"veyron/lib/stats/histogram"
	istats "veyron/services/mgmt/stats"

	"veyron2/rt"
)

func doGlob(root, pattern string, since time.Time, includeValues bool) ([]libstats.KeyValue, error) {
	it := libstats.Glob(root, pattern, since, includeValues)
	out := []libstats.KeyValue{}
	for it.Advance() {
		v := it.Value()
		out = append(out, v)
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func TestStats(t *testing.T) {
	rt.Init()

	now := time.Unix(1, 0)
	counter.Now = func() time.Time { return now }

	a := libstats.NewInteger("ipc/test/aaa")
	b := libstats.NewFloat("ipc/test/bbb")
	c := libstats.NewString("ipc/test/ccc")
	d := libstats.NewCounter("ipc/test/ddd")

	a.Set(1)
	b.Set(2)
	c.Set("Hello")
	d.Set(4)

	got, err := libstats.Value("ipc/test/aaa")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(1); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	if _, err := libstats.Value(""); err != libstats.ErrNotFound {
		t.Errorf("expected error, got err=%v", err)
	}
	if _, err := libstats.Value("does/not/exist"); err != libstats.ErrNotFound {
		t.Errorf("expected error, got err=%v", err)
	}

	root := libstats.NewInteger("")
	root.Set(42)
	got, err = libstats.Value("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(42); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	foo := libstats.NewInteger("foo")
	foo.Set(55)
	got, err = libstats.Value("foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(55); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	bar := libstats.NewInteger("foo/bar")
	bar.Set(44)
	got, err = libstats.Value("foo/bar")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(44); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	// Glob showing only nodes with a value.
	result, err := doGlob("", "...", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := []libstats.KeyValue{
		libstats.KeyValue{Key: "", Value: int64(42)},
		libstats.KeyValue{Key: "foo", Value: int64(55)},
		libstats.KeyValue{Key: "foo/bar", Value: int64(44)},
		libstats.KeyValue{Key: "ipc/test/aaa", Value: int64(1)},
		libstats.KeyValue{Key: "ipc/test/bbb", Value: float64(2)},
		libstats.KeyValue{Key: "ipc/test/ccc", Value: string("Hello")},
		libstats.KeyValue{Key: "ipc/test/ddd", Value: int64(4)},
		libstats.KeyValue{Key: "ipc/test/ddd/delta10m", Value: int64(4)},
		libstats.KeyValue{Key: "ipc/test/ddd/delta1h", Value: int64(4)},
		libstats.KeyValue{Key: "ipc/test/ddd/delta1m", Value: int64(4)},
		libstats.KeyValue{Key: "ipc/test/ddd/rate10m", Value: float64(0)},
		libstats.KeyValue{Key: "ipc/test/ddd/rate1h", Value: float64(0)},
		libstats.KeyValue{Key: "ipc/test/ddd/rate1m", Value: float64(0)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("", "ipc/test/*", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "ipc/test/aaa", Value: int64(1)},
		libstats.KeyValue{Key: "ipc/test/bbb", Value: float64(2)},
		libstats.KeyValue{Key: "ipc/test/ccc", Value: string("Hello")},
		libstats.KeyValue{Key: "ipc/test/ddd", Value: int64(4)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Glob showing all nodes without values
	result, err = doGlob("", "ipc/...", time.Time{}, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "ipc"},
		libstats.KeyValue{Key: "ipc/test"},
		libstats.KeyValue{Key: "ipc/test/aaa"},
		libstats.KeyValue{Key: "ipc/test/bbb"},
		libstats.KeyValue{Key: "ipc/test/ccc"},
		libstats.KeyValue{Key: "ipc/test/ddd"},
		libstats.KeyValue{Key: "ipc/test/ddd/delta10m"},
		libstats.KeyValue{Key: "ipc/test/ddd/delta1h"},
		libstats.KeyValue{Key: "ipc/test/ddd/delta1m"},
		libstats.KeyValue{Key: "ipc/test/ddd/rate10m"},
		libstats.KeyValue{Key: "ipc/test/ddd/rate1h"},
		libstats.KeyValue{Key: "ipc/test/ddd/rate1m"},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test the rate counter.
	now = now.Add(10 * time.Second)
	d.Incr(100)
	result, err = doGlob("", "ipc/test/ddd/*", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "ipc/test/ddd/delta10m", Value: int64(104)},
		libstats.KeyValue{Key: "ipc/test/ddd/delta1h", Value: int64(104)},
		libstats.KeyValue{Key: "ipc/test/ddd/delta1m", Value: int64(104)},
		libstats.KeyValue{Key: "ipc/test/ddd/rate10m", Value: float64(10.4)},
		libstats.KeyValue{Key: "ipc/test/ddd/rate1h", Value: float64(0)},
		libstats.KeyValue{Key: "ipc/test/ddd/rate1m", Value: float64(10.4)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test Glob on non-root object.
	result, err = doGlob("ipc/test", "*", time.Time{}, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "aaa", Value: int64(1)},
		libstats.KeyValue{Key: "bbb", Value: float64(2)},
		libstats.KeyValue{Key: "ccc", Value: string("Hello")},
		libstats.KeyValue{Key: "ddd", Value: int64(104)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("ipc/test/aaa", "", time.Time{}, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "", Value: int64(1)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test LastUpdate. The test only works on Counters.
	result, err = doGlob("ipc/test", "ddd", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "ddd", Value: int64(104)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("ipc/test", "ddd", now.Add(time.Second), true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test histogram
	h := libstats.NewHistogram("ipc/test/hhh", histogram.Options{NumBuckets: 5, GrowthFactor: 0})
	h.Add(1)
	h.Add(2)

	result, err = doGlob("", "ipc/test/hhh", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{
			Key: "ipc/test/hhh",
			Value: istats.HistogramValue{
				Count: 2,
				Sum:   3,
				Buckets: []istats.HistogramBucket{
					istats.HistogramBucket{LowBound: 0, Count: 0},
					istats.HistogramBucket{LowBound: 1, Count: 1},
					istats.HistogramBucket{LowBound: 2, Count: 1},
					istats.HistogramBucket{LowBound: 3, Count: 0},
					istats.HistogramBucket{LowBound: 4, Count: 0},
				},
			},
		},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	now = now.Add(30 * time.Second)
	h.Add(2)
	now = now.Add(30 * time.Second)
	h.Add(3)

	result, err = doGlob("", "ipc/test/hhh/delta1m", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{
			Key: "ipc/test/hhh/delta1m",
			Value: istats.HistogramValue{
				Count: 2,
				Sum:   5,
				Buckets: []istats.HistogramBucket{
					istats.HistogramBucket{LowBound: 0, Count: 0},
					istats.HistogramBucket{LowBound: 1, Count: 0},
					istats.HistogramBucket{LowBound: 2, Count: 1},
					istats.HistogramBucket{LowBound: 3, Count: 1},
					istats.HistogramBucket{LowBound: 4, Count: 0},
				},
			},
		},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}
}
