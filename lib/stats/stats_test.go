// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stats_test

import (
	"reflect"
	"testing"
	"time"

	"v.io/v23/verror"
	libstats "v.io/x/ref/lib/stats"
	"v.io/x/ref/lib/stats/counter"
	"v.io/x/ref/lib/stats/histogram"
	istats "v.io/x/ref/services/mgmt/stats"
)

func doGlob(root, pattern string, since time.Time, includeValues bool) ([]libstats.KeyValue, error) {
	it := libstats.Glob(root, pattern, since, includeValues)
	out := []libstats.KeyValue{}
	for it.Advance() {
		out = append(out, it.Value())
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func TestStats(t *testing.T) {
	now := time.Unix(1, 0)
	counter.TimeNow = func() time.Time { return now }

	a := libstats.NewInteger("rpc/test/aaa")
	b := libstats.NewFloat("rpc/test/bbb")
	c := libstats.NewString("rpc/test/ccc")
	d := libstats.NewCounter("rpc/test/ddd")

	a.Set(1)
	b.Set(2)
	c.Set("Hello")
	d.Set(4)

	got, err := libstats.Value("rpc/test/aaa")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if expected := int64(1); got != expected {
		t.Errorf("unexpected result. Got %v, want %v", got, expected)
	}

	if _, err := libstats.Value(""); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Errorf("expected error %s, got err=%s", verror.ErrNoExist.ID, verror.ErrorID(err))
	}
	if _, err := libstats.Value("does/not/exist"); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Errorf("expected error %s, got err=%s", verror.ErrNoExist.ID, verror.ErrorID(err))
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
		libstats.KeyValue{Key: "rpc/test/aaa", Value: int64(1)},
		libstats.KeyValue{Key: "rpc/test/bbb", Value: float64(2)},
		libstats.KeyValue{Key: "rpc/test/ccc", Value: string("Hello")},
		libstats.KeyValue{Key: "rpc/test/ddd", Value: int64(4)},
		libstats.KeyValue{Key: "rpc/test/ddd/delta10m", Value: int64(4)},
		libstats.KeyValue{Key: "rpc/test/ddd/delta1h", Value: int64(4)},
		libstats.KeyValue{Key: "rpc/test/ddd/delta1m", Value: int64(4)},
		libstats.KeyValue{Key: "rpc/test/ddd/rate10m", Value: float64(0)},
		libstats.KeyValue{Key: "rpc/test/ddd/rate1h", Value: float64(0)},
		libstats.KeyValue{Key: "rpc/test/ddd/rate1m", Value: float64(0)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("", "rpc/test/*", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "rpc/test/aaa", Value: int64(1)},
		libstats.KeyValue{Key: "rpc/test/bbb", Value: float64(2)},
		libstats.KeyValue{Key: "rpc/test/ccc", Value: string("Hello")},
		libstats.KeyValue{Key: "rpc/test/ddd", Value: int64(4)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Glob showing all nodes without values
	result, err = doGlob("", "rpc/...", time.Time{}, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "rpc"},
		libstats.KeyValue{Key: "rpc/test"},
		libstats.KeyValue{Key: "rpc/test/aaa"},
		libstats.KeyValue{Key: "rpc/test/bbb"},
		libstats.KeyValue{Key: "rpc/test/ccc"},
		libstats.KeyValue{Key: "rpc/test/ddd"},
		libstats.KeyValue{Key: "rpc/test/ddd/delta10m"},
		libstats.KeyValue{Key: "rpc/test/ddd/delta1h"},
		libstats.KeyValue{Key: "rpc/test/ddd/delta1m"},
		libstats.KeyValue{Key: "rpc/test/ddd/rate10m"},
		libstats.KeyValue{Key: "rpc/test/ddd/rate1h"},
		libstats.KeyValue{Key: "rpc/test/ddd/rate1m"},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test the rate counter.
	now = now.Add(10 * time.Second)
	d.Incr(100)
	result, err = doGlob("", "rpc/test/ddd/*", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "rpc/test/ddd/delta10m", Value: int64(104)},
		libstats.KeyValue{Key: "rpc/test/ddd/delta1h", Value: int64(104)},
		libstats.KeyValue{Key: "rpc/test/ddd/delta1m", Value: int64(104)},
		libstats.KeyValue{Key: "rpc/test/ddd/rate10m", Value: float64(10.4)},
		libstats.KeyValue{Key: "rpc/test/ddd/rate1h", Value: float64(0)},
		libstats.KeyValue{Key: "rpc/test/ddd/rate1m", Value: float64(10.4)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test Glob on non-root object.
	result, err = doGlob("rpc/test", "*", time.Time{}, true)
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

	result, err = doGlob("rpc/test/aaa", "", time.Time{}, true)
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
	result, err = doGlob("rpc/test", "ddd", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{Key: "ddd", Value: int64(104)},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	result, err = doGlob("rpc/test", "ddd", now.Add(time.Second), true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result. Got %#v, want %#v", result, expected)
	}

	// Test histogram
	h := libstats.NewHistogram("rpc/test/hhh", histogram.Options{NumBuckets: 5, GrowthFactor: 0})
	h.Add(1)
	h.Add(2)

	result, err = doGlob("", "rpc/test/hhh", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{
			Key: "rpc/test/hhh",
			Value: istats.HistogramValue{
				Count: 2,
				Sum:   3,
				Min:   1,
				Max:   2,
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

	result, err = doGlob("", "rpc/test/hhh/delta1m", now, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected = []libstats.KeyValue{
		libstats.KeyValue{
			Key: "rpc/test/hhh/delta1m",
			Value: istats.HistogramValue{
				Count: 2,
				Sum:   5,
				Min:   2,
				Max:   3,
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

func TestMap(t *testing.T) {
	m := libstats.NewMap("testing/foo")
	m.Set([]libstats.KeyValue{{"a", 1}, {"b", 2}})

	// Test the Value of the map.
	{
		got, err := libstats.Value("testing/foo")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if expected := interface{}(nil); got != expected {
			t.Errorf("unexpected result. Got %v, want %v", got, expected)
		}
	}
	// Test Glob on the map object.
	{
		got, err := doGlob("testing", "foo/...", time.Time{}, true)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		expected := []libstats.KeyValue{
			libstats.KeyValue{Key: "foo", Value: nil},
			libstats.KeyValue{Key: "foo/a", Value: int64(1)},
			libstats.KeyValue{Key: "foo/b", Value: int64(2)},
		}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}
	}

	m.Delete([]string{"a"})

	// Test Glob on the map object.
	{
		got, err := doGlob("testing", "foo/...", time.Time{}, true)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		expected := []libstats.KeyValue{
			libstats.KeyValue{Key: "foo", Value: nil},
			libstats.KeyValue{Key: "foo/b", Value: int64(2)},
		}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}
	}
}

func TestFunc(t *testing.T) {
	libstats.NewIntegerFunc("testing/integer", func() int64 { return 123 })
	libstats.NewFloatFunc("testing/float", func() float64 { return 456.789 })
	libstats.NewStringFunc("testing/string", func() string { return "Hello World" })
	ch := make(chan int64, 5)
	libstats.NewIntegerFunc("testing/slowint", func() int64 {
		return <-ch
	})

	testcases := []struct {
		name     string
		expected interface{}
	}{
		{"testing/integer", int64(123)},
		{"testing/float", float64(456.789)},
		{"testing/string", "Hello World"},
		{"testing/slowint", nil}, // Times out
	}
	for _, tc := range testcases {
		checkVariable(t, tc.name, tc.expected)
	}
	checkVariable(t, "testing/slowint", nil) // Times out
	checkVariable(t, "testing/slowint", nil) // Times out
	ch <- int64(0)
	checkVariable(t, "testing/slowint", int64(0)) // New value
	checkVariable(t, "testing/slowint", int64(0)) // Times out
	for i := 1; i <= 5; i++ {
		ch <- int64(i)
	}
	for i := 1; i <= 5; i++ {
		checkVariable(t, "testing/slowint", int64(i)) // New value each time
	}
}

func checkVariable(t *testing.T, name string, expected interface{}) {
	got, err := libstats.Value(name)
	if err != nil {
		t.Errorf("unexpected error for %q: %v", name, err)
	}
	if got != expected {
		t.Errorf("unexpected result for %q. Got %v, want %v", name, got, expected)
	}
}

func TestDelete(t *testing.T) {
	_ = libstats.NewInteger("a/b/c/d")
	if _, err := libstats.GetStatsObject("a/b/c/d"); err != nil {
		t.Errorf("unexpected error value: %v", err)
	}
	if err := libstats.Delete("a/b/c/d"); err != nil {
		t.Errorf("unexpected error value: %v", err)
	}
	if _, err := libstats.GetStatsObject("a/b/c/d"); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Errorf("unexpected error value: Got %v, want %v", verror.ErrorID(err), verror.ErrNoExist.ID)
	}
	if err := libstats.Delete("a/b"); err != nil {
		t.Errorf("unexpected error value: %v", err)
	}
	if _, err := libstats.GetStatsObject("a/b"); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Errorf("unexpected error value: Got %v, want %v", verror.ErrorID(err), verror.ErrNoExist.ID)
	}
	if _, err := libstats.GetStatsObject("a/b/c"); verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Errorf("unexpected error value: Got %v, want %v", verror.ErrorID(err), verror.ErrNoExist.ID)
	}
}
