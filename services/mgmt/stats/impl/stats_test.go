package impl_test

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/watch/types"

	libstats "veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/lib/stats/histogram"
	"veyron.io/veyron/veyron/profiles"
	istats "veyron.io/veyron/veyron/services/mgmt/stats"
	"veyron.io/veyron/veyron/services/mgmt/stats/impl"
)

type statsDispatcher struct {
}

func (d *statsDispatcher) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	return impl.NewStatsServer(suffix, 100*time.Millisecond), nil, nil
}

func startServer(t *testing.T) (string, func()) {
	disp := &statsDispatcher{}
	server, err := rt.R().NewServer()
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
		return "", nil
	}
	endpoint, err := server.Listen(profiles.LocalListenSpec)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
		return "", nil
	}
	if err := server.ServeDispatcher("", disp); err != nil {
		t.Fatalf("Serve failed: %v", err)
		return "", nil
	}
	return endpoint.String(), func() { server.Stop() }
}

func TestStatsImpl(t *testing.T) {
	rt.Init()

	endpoint, stop := startServer(t)
	defer stop()

	counter := libstats.NewCounter("testing/foo/bar")
	counter.Incr(10)

	histogram := libstats.NewHistogram("testing/hist/foo", histogram.Options{
		NumBuckets:         5,
		GrowthFactor:       1,
		SmallestBucketSize: 1,
		MinValue:           0,
	})
	for i := 0; i < 10; i++ {
		histogram.Add(int64(i))
	}

	c := stats.StatsClient(naming.JoinAddressName(endpoint, ""))

	// Test Glob()
	{
		stream, err := c.Glob(rt.R().NewContext(), "testing/foo/...")
		if err != nil {
			t.Fatalf("c.Glob failed: %v", err)
		}
		iterator := stream.RecvStream()
		results := []string{}
		for iterator.Advance() {
			me := iterator.Value()
			if len(me.Servers) > 0 {
				t.Errorf("unexpected servers. Got %v, want none", me.Servers)
			}
			results = append(results, me.Name)
		}
		if err := iterator.Err(); err != nil {
			t.Errorf("unexpected stream error: %v", err)
		}
		err = stream.Finish()
		if err != nil {
			t.Errorf("gstream.Finish failed: %v", err)
		}
		expected := []string{
			"testing/foo",
			"testing/foo/bar",
			"testing/foo/bar/delta10m",
			"testing/foo/bar/delta1h",
			"testing/foo/bar/delta1m",
			"testing/foo/bar/rate10m",
			"testing/foo/bar/rate1h",
			"testing/foo/bar/rate1m",
		}
		sort.Strings(results)
		sort.Strings(expected)
		if !reflect.DeepEqual(results, expected) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
		}
	}

	// Test WatchGlob()
	{
		stream, err := c.WatchGlob(rt.R().NewContext(), types.GlobRequest{Pattern: "testing/foo/bar"})
		if err != nil {
			t.Fatalf("c.WatchGlob failed: %v", err)
		}
		iterator := stream.RecvStream()
		if !iterator.Advance() {
			t.Fatalf("expected more stream values")
		}
		got := iterator.Value()
		expected := types.Change{Name: "testing/foo/bar", Value: int64(10)}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}

		counter.Incr(5)

		if !iterator.Advance() {
			t.Fatalf("expected more stream values")
		}
		got = iterator.Value()
		expected = types.Change{Name: "testing/foo/bar", Value: int64(15)}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}

		counter.Incr(2)

		if !iterator.Advance() {
			t.Fatalf("expected more stream values")
		}
		got = iterator.Value()
		expected = types.Change{Name: "testing/foo/bar", Value: int64(17)}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}
		stream.Cancel()

		if iterator.Advance() {
			t.Errorf("expected no more stream values, got: %v", iterator.Value())
		}
	}

	// Test Value()
	{
		c := stats.StatsClient(naming.JoinAddressName(endpoint, "testing/foo/bar"))
		value, err := c.Value(rt.R().NewContext())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if expected := int64(17); value != expected {
			t.Errorf("unexpected result. Got %v, want %v", value, expected)
		}
	}

	// Test Value() with Histogram
	{
		c := stats.StatsClient(naming.JoinAddressName(endpoint, "testing/hist/foo"))
		value, err := c.Value(rt.R().NewContext())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		want := istats.HistogramValue{
			Count: 10,
			Sum:   45,
			Buckets: []istats.HistogramBucket{
				istats.HistogramBucket{LowBound: 0, Count: 1},
				istats.HistogramBucket{LowBound: 1, Count: 2},
				istats.HistogramBucket{LowBound: 3, Count: 4},
				istats.HistogramBucket{LowBound: 7, Count: 3},
				istats.HistogramBucket{LowBound: 15, Count: 0},
			},
		}
		if !reflect.DeepEqual(value, want) {
			t.Errorf("unexpected result. Got %#v, want %#v", value, want)
		}
	}
}
