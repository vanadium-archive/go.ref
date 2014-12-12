// A simple command-line tool to run the benchmark client.
package main

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/testutil"
	_ "veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/runtimes/google/ipc/benchmarks"
)

var (
	server = flag.String("server", "", "address of the server to connect to")

	iterations = flag.Int("iterations", 100, "number of iterations to run")

	chunkCnt       = flag.Int("chunk_count", 0, "number of chunks to send per streaming RPC (if zero, use non-streaming RPC)")
	payloadSize    = flag.Int("payload_size", 0, "size of payload in bytes")
	chunkCntMux    = flag.Int("mux_chunk_count", 0, "number of chunks to send in background")
	payloadSizeMux = flag.Int("mux_payload_size", 0, "size of payload to send in background")
)

func main() {
	vrt, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %s", err)
	}
	defer vrt.Cleanup()

	if *chunkCntMux > 0 && *payloadSizeMux > 0 {
		dummyB := testing.B{}
		_, stop := benchmarks.StartEchoStream(&dummyB, vrt.NewContext(), *server, 0, *chunkCntMux, *payloadSizeMux, nil)
		defer stop()
		vlog.Infof("Started background streaming (chunk_size=%d, payload_size=%d)", *chunkCntMux, *payloadSizeMux)
	}

	dummyB := testing.B{}
	stats := testutil.NewBenchStats(16)

	now := time.Now()
	ctx := vrt.NewContext()
	if *chunkCnt == 0 {
		benchmarks.CallEcho(&dummyB, ctx, *server, *iterations, *payloadSize, stats)
	} else {
		benchmarks.CallEchoStream(&dummyB, ctx, *server, *iterations, *chunkCnt, *payloadSize, stats)
	}
	elapsed := time.Since(now)

	fmt.Printf("iterations: %d  chunk_count: %d  payload_size: %d\n", *iterations, *chunkCnt, *payloadSize)
	fmt.Printf("elapsed time: %v\n", elapsed)
	stats.Print(os.Stdout)
}
