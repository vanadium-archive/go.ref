// package histogram implements a basic histogram to keep track of data
// distribution.
package histogram

import (
	"fmt"
	"time"

	"veyron/lib/stats/counter"
)

// A Histogram accumulates values in the form of a histogram. The type of the
// values is int64, which is suitable for keeping track of things like RPC
// latency in milliseconds. New histogram objects should be obtain via the New()
// function.
type Histogram struct {
	opts    Options
	buckets []bucketInternal
	sum     *counter.Counter
	count   *counter.Counter
}

// Options contains the parameters that define the histogram's buckets.
type Options struct {
	// NumBuckets is the number of buckets.
	NumBuckets int
	// GrowthFactor is the growth factor of the buckets. A value of 0.1
	// indicates that bucket N+1 will be 10% larger than bucket N.
	GrowthFactor float64
	// SmallestBucketSize is the size of the first bucket. Bucket sizes are
	// rounded down to the nearest integer.
	SmallestBucketSize float64
	// MinValue is the lower bound of the first bucket.
	MinValue int64
}

// HistogramValue is the value returned by Value(), and the Delta*() methods.
type HistogramValue struct {
	// Count is the total number of values added to the histogram.
	Count int64
	// Sum is the sum of all the values added to the histogram.
	Sum int64
	// Buckets contains all the buckets of the histogram.
	Buckets []Bucket
}

// Bucket is one histogram bucket.
type Bucket struct {
	// LowBound is the lower bound of the bucket.
	LowBound int64
	// Count is the number of values in the bucket.
	Count int64
}

// bucketInternal is the internal representation of a bucket, which includes a
// rate counter.
type bucketInternal struct {
	lowBound int64
	count    *counter.Counter
}

// New returns a pointer to a new Histogram object that was created with the
// provided options.
func New(opts Options) *Histogram {
	if opts.NumBuckets == 0 {
		opts.NumBuckets = 32
	}
	if opts.SmallestBucketSize == 0.0 {
		opts.SmallestBucketSize = 1.0
	}
	h := Histogram{
		opts:    opts,
		buckets: make([]bucketInternal, opts.NumBuckets),
		sum:     counter.New(),
		count:   counter.New(),
	}
	low := opts.MinValue
	delta := opts.SmallestBucketSize
	for i := 0; i < opts.NumBuckets; i++ {
		h.buckets[i].lowBound = low
		h.buckets[i].count = counter.New()
		low = low + int64(delta)
		delta = delta * (1.0 + opts.GrowthFactor)
	}
	return &h
}

// Opts returns a copy of the options used to create the Histogram.
func (h *Histogram) Opts() Options {
	return h.opts
}

// Add adds a value to the histogram.
func (h *Histogram) Add(value int64) error {
	bucket, err := h.findBucket(value)
	if err != nil {
		return err
	}
	h.buckets[bucket].count.Incr(1)
	h.count.Incr(1)
	h.sum.Incr(value)
	return nil
}

// LastUpdate returns the time at which the object was last updated.
func (h *Histogram) LastUpdate() time.Time {
	return h.count.LastUpdate()
}

// Value returns the accumulated state of the histogram since it was created.
func (h *Histogram) Value() HistogramValue {
	b := make([]Bucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = Bucket{
			LowBound: v.lowBound,
			Count:    v.count.Value(),
		}
	}

	v := HistogramValue{
		Count:   h.count.Value(),
		Sum:     h.sum.Value(),
		Buckets: b,
	}
	return v
}

// Delta1h returns the change in the last hour.
func (h *Histogram) Delta1h() HistogramValue {
	b := make([]Bucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = Bucket{
			LowBound: v.lowBound,
			Count:    v.count.Delta1h(),
		}
	}

	v := HistogramValue{
		Count:   h.count.Delta1h(),
		Sum:     h.sum.Delta1h(),
		Buckets: b,
	}
	return v
}

// Delta10m returns the change in the last 10 minutes.
func (h *Histogram) Delta10m() HistogramValue {
	b := make([]Bucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = Bucket{
			LowBound: v.lowBound,
			Count:    v.count.Delta10m(),
		}
	}

	v := HistogramValue{
		Count:   h.count.Delta10m(),
		Sum:     h.sum.Delta10m(),
		Buckets: b,
	}
	return v
}

// Delta1m returns the change in the last 10 minutes.
func (h *Histogram) Delta1m() HistogramValue {
	b := make([]Bucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = Bucket{
			LowBound: v.lowBound,
			Count:    v.count.Delta1m(),
		}
	}

	v := HistogramValue{
		Count:   h.count.Delta1m(),
		Sum:     h.sum.Delta1m(),
		Buckets: b,
	}
	return v
}

// findBucket does a binary search to find in which bucket the value goes.
func (h *Histogram) findBucket(value int64) (int, error) {
	lastBucket := len(h.buckets) - 1
	min, max := 0, lastBucket
	for max >= min {
		b := (min + max) / 2
		if value >= h.buckets[b].lowBound && (b == lastBucket || value < h.buckets[b+1].lowBound) {
			return b, nil
		}
		if value < h.buckets[b].lowBound {
			max = b - 1
			continue
		}
		min = b + 1
	}
	return 0, fmt.Errorf("no bucket for value: %f", value)
}
