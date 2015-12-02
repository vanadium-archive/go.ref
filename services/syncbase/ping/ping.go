// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ping provides a mechanism for pinging a set of servers in parallel to
// identify responsive ones.
//
// Note that it will be straightforward to adapt PingInParallel to work with
// two-stage RPCs (with an "establish connection" stage and a "invoke procedure"
// stage) should Vanadium introduce this concept.
package ping

import (
	"sort"
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/x/ref/services/syncbase/server/interfaces"
)

// PingResult is the result of a single ping.
type PingResult struct {
	Name string        // name that was pinged
	Err  error         // either an RPC error or a timeout/canceled error
	Dur  time.Duration // ping latency
}

// PingInParallel sends a Ping RPC in parallel to all specified names, waits for
// some time, then returns a map of name to PingResult.
// - 'slack' is the duration that PingInParallel will wait for additional ping
//   replies after receiving the first (lowest latency) ping reply before
//   returning.
// - 'timeout' is the maximum duration that PingInParallel will wait before
//   cancelling any outstanding RPCs and returning.
// Notes:
// - 'timeout' takes precedence over 'slack', i.e. any outstanding RPCs will
//   always be cancelled immediately when the timeout triggers.
// - PingInParallel may report erroneous ping latencies if the system clock
//   changes during the ping RPC. Clients must be resilient to such errors.
func PingInParallel(ctx *context.T, names []string, slack, timeout time.Duration) (map[string]PingResult, error) {
	var mu sync.Mutex // guards results
	results := map[string]PingResult{}

	ctx, cancel := context.WithTimeout(ctx, timeout)

	// Spawn goroutines.
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			// Note, ctx is thread-safe and is not mutated by the spawned goroutines.
			result := ping(ctx, name)
			mu.Lock()
			results[name] = result
			startSlackTimer := len(results) == 1
			mu.Unlock()
			if startSlackTimer {
				// First ping completed; start the 'slack' timer.
				time.AfterFunc(slack, cancel)
			}
		}(name)
	}

	// Wait for all pings to complete (or get cancelled).
	wg.Wait()

	// At this point, all spawned goroutines have completed, so we can safely
	// access 'results' without acquiring mu.
	return results, nil
}

// SortByDur takes a map[string]PingResult produced by PingInParallel and
// returns a []PingResult sorted by latency from low to high, with failed pings
// omitted.
func SortByDur(m map[string]PingResult) []PingResult {
	res := make([]PingResult, 0, len(m))
	for _, v := range m {
		if v.Err == nil {
			res = append(res, v)
		}
	}
	sort.Sort(byDur(res))
	return res
}

////////////////////////////////////////
// Internal helpers

// ping pings a single name and returns the PingResult.
func ping(ctx *context.T, name string) PingResult {
	start := time.Now()
	err := interfaces.PingableClient(name).Ping(ctx)
	return PingResult{Name: name, Err: err, Dur: time.Now().Sub(start)}
}

// byDur implements sort.Interface for []PingResult based on the Dur field.
type byDur []PingResult

func (a byDur) Len() int      { return len(a) }
func (a byDur) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byDur) Less(i, j int) bool {
	if a[i].Dur != a[j].Dur {
		return a[i].Dur < a[j].Dur
	}
	return a[i].Name < a[j].Name // tiebreak, for stability
}
