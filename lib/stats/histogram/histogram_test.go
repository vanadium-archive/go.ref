package histogram_test

import (
	"testing"

	"veyron/lib/stats/histogram"
)

func TestHistogram(t *testing.T) {
	// This creates a histogram with the following buckets:
	//  [1, 2[
	//  [2, 4[
	//  [4, 8[
	//  [8, 16[
	//  [16, Inf
	opts := histogram.Options{
		NumBuckets:         5,
		GrowthFactor:       1.0,
		SmallestBucketSize: 1.0,
		MinValue:           1.0,
	}
	h := histogram.New(opts)
	// Trying to add a value that's less than MinValue, should return an error.
	if err := h.Add(0); err == nil {
		t.Errorf("unexpected return value for Add(0.0). Want != nil, Got nil")
	}
	// Adding good values. Expect no errors.
	for i := 1; i <= 50; i++ {
		if err := h.Add(int64(i)); err != nil {
			t.Errorf("unexpected return value for Add(%d). Want nil, Got %v", i, err)
		}
	}
	expectedCount := []int64{1, 2, 4, 8, 35}
	buckets := h.Value().Buckets
	for i := 0; i < opts.NumBuckets; i++ {
		if buckets[i].Count != expectedCount[i] {
			t.Errorf("unexpected count for bucket[%d]. Want %d, Got %v", i, expectedCount[i], buckets[i].Count)
		}
	}
}

func BenchmarkHistogram(b *testing.B) {
	opts := histogram.Options{
		NumBuckets:         30,
		GrowthFactor:       1.0,
		SmallestBucketSize: 1.0,
		MinValue:           1.0,
	}
	h := histogram.New(opts)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Add(int64(i))
	}
}
