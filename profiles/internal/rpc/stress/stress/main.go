package main

import (
	"flag"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/vlog"

	"v.io/x/ref/profiles/internal/rpc/stress"
	"v.io/x/ref/profiles/internal/rpc/stress/internal"
	_ "v.io/x/ref/profiles/static"
)

var (
	servers  = flag.String("servers", "", "comma-seperated list of of the servers to connect to")
	workers  = flag.Int("workers", 1, "number of test workers to run; If zero, no test will be performed.")
	duration = flag.Duration("duration", 1*time.Minute, "duration of the stress test to run")

	maxChunkCnt    = flag.Int("max_chunk_count", 100, "maximum number of chunks to send per streaming RPC")
	maxPayloadSize = flag.Int("max_payload_size", 10000, "maximum size of payload in bytes")

	serverStats = flag.Bool("server_stats", false, "If true, print out the server stats")
	serverStop  = flag.Bool("server_stop", false, "If true, shutdown the servers")
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func runTest(ctx *context.T, servers []string) {
	fmt.Printf("starting stress test against %d servers...\n", len(servers))
	fmt.Printf("workers: %d, maxChunkCnt: %d, maxPayloadSize: %d\n", *workers, *maxChunkCnt, *maxPayloadSize)

	now := time.Now()
	done := make(chan stress.Stats, *workers)
	for i := 0; i < *workers; i++ {
		go func() {
			var sumCount, sumStreamCount uint64
			timeout := time.After(*duration)
		done:
			for {
				server := servers[rand.Intn(len(servers))]
				if rand.Intn(2) == 0 {
					internal.CallSum(ctx, server, *maxPayloadSize)
					sumCount++
				} else {
					internal.CallSumStream(ctx, server, *maxChunkCnt, *maxPayloadSize)
					sumStreamCount++
				}

				select {
				case <-timeout:
					break done
				default:
				}
			}
			done <- stress.Stats{sumCount, sumStreamCount}
		}()
	}

	var stats stress.Stats
	for i := 0; i < *workers; i++ {
		s := <-done
		stats.SumCount += s.SumCount
		stats.SumStreamCount += s.SumStreamCount
	}
	elapsed := time.Since(now)

	fmt.Printf("done after %v\n", elapsed)
	fmt.Printf("client stats: %+v, ", stats)
	fmt.Printf("qps: %.2f\n", float64(stats.SumCount+stats.SumStreamCount)/elapsed.Seconds())
}

func printServerStats(ctx *context.T, servers []string) {
	for _, server := range servers {
		stats, err := stress.StressClient(server).GetStats(ctx)
		if err != nil {
			vlog.Fatal("GetStats failed: %v\n", err)
		}
		fmt.Printf("server stats: %q:%+v\n", server, stats)
	}
}

func stopServers(ctx *context.T, servers []string) {
	for _, server := range servers {
		if err := stress.StressClient(server).Stop(ctx); err != nil {
			vlog.Fatal("Stop failed: %v\n", err)
		}
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	ctx, shutdown := v23.Init()
	defer shutdown()

	var addrs []string
	for _, a := range strings.Split(*servers, ",") {
		addrs = append(addrs, strings.TrimSpace(a))
	}
	if len(addrs) == 0 {
		vlog.Fatal("no server specified")
	}

	if *workers > 0 && *duration > 0 {
		runTest(ctx, addrs)
	}

	if *serverStats {
		printServerStats(ctx, addrs)
	}
	if *serverStop {
		stopServers(ctx, addrs)
	}
}
