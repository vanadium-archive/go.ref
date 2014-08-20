package stats_test

import (
	"reflect"
	"testing"
	"time"

	"veyron/lib/stats"
	"veyron/lib/stats/counter"
	"veyron/lib/stats/histogram"
	"veyron2/rt"
)

func doGlob(root, pattern string, since time.Time) ([]stats.KeyValue, error) {
	it := stats.Glob(root, pattern, since, true)
	out := []stats.KeyValue{}
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

	a := stats.NewInteger("ipc/test/aaa")
	b := stats.NewFloat("ipc/test/bbb")
	c := stats.NewString("ipc/test/ccc")
	d := stats.NewCounter("ipc/test/ddd")

	a.Set(1)
	b.Set(2)
	c.Set("Hello")
	d.Set(4)

	got, err := stats.Value("ipc/test/aaa")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(1); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	if _, err := stats.Value(""); err != stats.ErrNotFound {
		t.Errorf("expected error, got err=%v", err)
	}
	if _, err := stats.Value("does/not/exist"); err != stats.ErrNotFound {
		t.Errorf("expected error, got err=%v", err)
	}

	root := stats.NewInteger("")
	root.Set(42)
	got, err = stats.Value("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(42); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	foo := stats.NewInteger("foo")
	foo.Set(55)
	got, err = stats.Value("foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(55); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	bar := stats.NewInteger("foo/bar")
	bar.Set(44)
	got, err = stats.Value("foo/bar")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(44); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	result, err := doGlob("", "...", now)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := []stats.KeyValue{
		stats.KeyValue{Key: "", Value: int64(42)},
		stats.KeyValue{Key: "foo", Value: int64(55)},
		stats.KeyValue{Key: "foo/bar", Value: int64(44)},
		stats.KeyValue{Key: "ipc/test/aaa", Value: int64(1)},
		stats.KeyValue{Key: "ipc/test/bbb", Value: float64(2)},
		stats.KeyValue{Key: "ipc/test/ccc", Value: string("Hello")},
		stats.KeyValue{Key: "ipc/test/ddd", Value: int64(4)},
		stats.KeyValue{Key: "ipc/test/ddd/delta10m", Value: int64(0)},
		stats.KeyValue{Key: "ipc/test/ddd/delta1h", Value: int64(0)},
		stats.KeyValue{Key: "ipc/test/ddd/delta1m", Value: int64(0)},
		stats.KeyValue{Key: "ipc/test/ddd/rate10m", Value: float64(0)},
		stats.KeyValue{Key: "ipc/test/ddd/rate1h", Value: float64(0)},
		stats.KeyValue{Key: "ipc/test/ddd/rate1m", Value: float64(0)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("", "ipc/test/*", now)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{
		stats.KeyValue{Key: "ipc/test/aaa", Value: int64(1)},
		stats.KeyValue{Key: "ipc/test/bbb", Value: float64(2)},
		stats.KeyValue{Key: "ipc/test/ccc", Value: string("Hello")},
		stats.KeyValue{Key: "ipc/test/ddd", Value: int64(4)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test the rate counter.
	now = now.Add(10 * time.Second)
	d.Incr(100)
	result, err = doGlob("", "ipc/test/ddd/*", now)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{
		stats.KeyValue{Key: "ipc/test/ddd/delta10m", Value: int64(100)},
		stats.KeyValue{Key: "ipc/test/ddd/delta1h", Value: int64(0)},
		stats.KeyValue{Key: "ipc/test/ddd/delta1m", Value: int64(100)},
		stats.KeyValue{Key: "ipc/test/ddd/rate10m", Value: float64(10)},
		stats.KeyValue{Key: "ipc/test/ddd/rate1h", Value: float64(0)},
		stats.KeyValue{Key: "ipc/test/ddd/rate1m", Value: float64(10)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test Glob on non-root object.
	result, err = doGlob("ipc/test", "*", time.Time{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{
		stats.KeyValue{Key: "aaa", Value: int64(1)},
		stats.KeyValue{Key: "bbb", Value: float64(2)},
		stats.KeyValue{Key: "ccc", Value: string("Hello")},
		stats.KeyValue{Key: "ddd", Value: int64(104)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("ipc/test/aaa", "", time.Time{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{
		stats.KeyValue{Key: "", Value: int64(1)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test LastUpdate. The test only works on Counters.
	result, err = doGlob("ipc/test", "ddd", now)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{
		stats.KeyValue{Key: "ddd", Value: int64(104)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("ipc/test", "ddd", now.Add(time.Second))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test histogram
	h := stats.NewHistogram("ipc/test/hhh", histogram.Options{NumBuckets: 5, GrowthFactor: 0})
	h.Add(1)
	h.Add(2)

	result, err = doGlob("", "ipc/test/hhh", now)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{
		stats.KeyValue{
			Key: "ipc/test/hhh",
			Value: histogram.HistogramValue{
				Count: 2,
				Sum:   3,
				Buckets: []histogram.Bucket{
					histogram.Bucket{LowBound: 0, Count: 0},
					histogram.Bucket{LowBound: 1, Count: 1},
					histogram.Bucket{LowBound: 2, Count: 1},
					histogram.Bucket{LowBound: 3, Count: 0},
					histogram.Bucket{LowBound: 4, Count: 0},
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

	result, err = doGlob("", "ipc/test/hhh/delta1m", now)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []stats.KeyValue{
		stats.KeyValue{
			Key: "ipc/test/hhh/delta1m",
			Value: histogram.HistogramValue{
				Count: 2,
				Sum:   5,
				Buckets: []histogram.Bucket{
					histogram.Bucket{LowBound: 0, Count: 0},
					histogram.Bucket{LowBound: 1, Count: 0},
					histogram.Bucket{LowBound: 2, Count: 1},
					histogram.Bucket{LowBound: 3, Count: 1},
					histogram.Bucket{LowBound: 4, Count: 0},
				},
			},
		},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}
}
