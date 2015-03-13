package impl_test

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/stats"
	"v.io/v23/services/watch/types"
	"v.io/v23/vdl"

	libstats "v.io/x/ref/lib/stats"
	"v.io/x/ref/lib/stats/histogram"
	_ "v.io/x/ref/profiles"
	istats "v.io/x/ref/services/mgmt/stats"
	"v.io/x/ref/services/mgmt/stats/impl"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type statsDispatcher struct {
}

func (d *statsDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return impl.NewStatsService(suffix, 100*time.Millisecond), nil, nil
}

func startServer(t *testing.T, ctx *context.T) (string, func()) {
	disp := &statsDispatcher{}
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
		return "", nil
	}
	endpoints, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
		return "", nil
	}
	if err := server.ServeDispatcher("", disp); err != nil {
		t.Fatalf("Serve failed: %v", err)
		return "", nil
	}
	return endpoints[0].String(), func() { server.Stop() }
}

func TestStatsImpl(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	endpoint, stop := startServer(t, ctx)
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

	name := naming.JoinAddressName(endpoint, "")
	c := stats.StatsClient(name)

	// Test Glob()
	{
		results, _, err := testutil.GlobName(ctx, name, "testing/foo/...")
		if err != nil {
			t.Fatalf("testutil.GlobName failed: %v", err)
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
		var noRM types.ResumeMarker
		ctx, cancel := context.WithCancel(ctx)
		stream, err := c.WatchGlob(ctx, types.GlobRequest{Pattern: "testing/foo/bar"})
		if err != nil {
			t.Fatalf("c.WatchGlob failed: %v", err)
		}
		iterator := stream.RecvStream()
		if !iterator.Advance() {
			t.Fatalf("expected more stream values")
		}
		got := iterator.Value()
		expected := types.Change{Name: "testing/foo/bar", Value: vdl.Int64Value(10), ResumeMarker: noRM}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}

		counter.Incr(5)

		if !iterator.Advance() {
			t.Fatalf("expected more stream values")
		}
		got = iterator.Value()
		expected = types.Change{Name: "testing/foo/bar", Value: vdl.Int64Value(15), ResumeMarker: noRM}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}

		counter.Incr(2)

		if !iterator.Advance() {
			t.Fatalf("expected more stream values")
		}
		got = iterator.Value()
		expected = types.Change{Name: "testing/foo/bar", Value: vdl.Int64Value(17), ResumeMarker: noRM}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unexpected result. Got %#v, want %#v", got, expected)
		}
		cancel()

		if iterator.Advance() {
			t.Errorf("expected no more stream values, got: %v", iterator.Value())
		}
	}

	// Test Value()
	{
		c := stats.StatsClient(naming.JoinAddressName(endpoint, "testing/foo/bar"))
		value, err := c.Value(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if want := vdl.Int64Value(17); !vdl.EqualValue(value, want) {
			t.Errorf("unexpected result. Got %v, want %v", value, want)
		}
	}

	// Test Value() with Histogram
	{
		c := stats.StatsClient(naming.JoinAddressName(endpoint, "testing/hist/foo"))
		value, err := c.Value(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		want := vdl.ValueOf(istats.HistogramValue{
			Count: 10,
			Sum:   45,
			Min:   0,
			Max:   9,
			Buckets: []istats.HistogramBucket{
				istats.HistogramBucket{LowBound: 0, Count: 1},
				istats.HistogramBucket{LowBound: 1, Count: 2},
				istats.HistogramBucket{LowBound: 3, Count: 4},
				istats.HistogramBucket{LowBound: 7, Count: 3},
				istats.HistogramBucket{LowBound: 15, Count: 0},
			},
		})
		if !vdl.EqualValue(value, want) {
			t.Errorf("unexpected result. Got %v, want %v", value, want)
		}
	}
}
