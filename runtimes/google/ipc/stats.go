package ipc

import (
	"sync"
	"time"

	"veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/lib/stats/histogram"

	"veyron.io/veyron/veyron2/naming"
)

type ipcStats struct {
	mu      sync.RWMutex
	prefix  string
	methods map[string]*perMethodStats
}

func newIPCStats(prefix string) *ipcStats {
	return &ipcStats{prefix: prefix, methods: make(map[string]*perMethodStats)}
}

type perMethodStats struct {
	latency *histogram.Histogram
}

func (s *ipcStats) stop() {
	stats.Delete(s.prefix)
}

func (s *ipcStats) record(method string, latency time.Duration) {
	// Try first with a read lock. This will succeed in the most common
	// case. If it fails, try again with a write lock and create the stats
	// objects if they are still not there.
	s.mu.RLock()
	m, ok := s.methods[method]
	s.mu.RUnlock()
	if !ok {
		m = s.newPerMethodStats(method)
	}
	m.latency.Add(int64(latency / time.Millisecond))
}

// newPerMethodStats creates a new perMethodStats object if one doesn't exist
// already. It returns the newly created object, or the already existing one.
func (s *ipcStats) newPerMethodStats(method string) *perMethodStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.methods[method]
	if !ok {
		name := naming.Join(s.prefix, method, "latency-ms")
		s.methods[method] = &perMethodStats{
			latency: stats.NewHistogram(name, histogram.Options{
				NumBuckets:         25,
				GrowthFactor:       1,
				SmallestBucketSize: 1,
				MinValue:           0,
			}),
		}
		m = s.methods[method]
	}
	return m
}
