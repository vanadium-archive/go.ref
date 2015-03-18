// A simple command-line tool to run the benchmark client.
package main

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/profiles/internal/rpc/benchmark/internal"
	tbm "v.io/x/ref/test/benchmark"

	"v.io/v23"
	"v.io/x/lib/vlog"
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
	ctx, shutdown := v23.Init()
	defer shutdown()

	if *chunkCntMux > 0 && *payloadSizeMux > 0 {
		dummyB := testing.B{}
		_, stop := internal.StartEchoStream(&dummyB, ctx, *server, 0, *chunkCntMux, *payloadSizeMux, nil)
		defer stop()
		vlog.Infof("Started background streaming (chunk_size=%d, payload_size=%d)", *chunkCntMux, *payloadSizeMux)
	}

	dummyB := testing.B{}
	stats := tbm.NewStats(16)

	now := time.Now()
	if *chunkCnt == 0 {
		internal.CallEcho(&dummyB, ctx, *server, *iterations, *payloadSize, stats)
	} else {
		internal.CallEchoStream(&dummyB, ctx, *server, *iterations, *chunkCnt, *payloadSize, stats)
	}
	elapsed := time.Since(now)

	fmt.Printf("iterations: %d  chunk_count: %d  payload_size: %d\n", *iterations, *chunkCnt, *payloadSize)
	fmt.Printf("elapsed time: %v\n", elapsed)
	stats.Print(os.Stdout)
}
