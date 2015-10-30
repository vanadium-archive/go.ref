// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"runtime"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/x/ref"
	"v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/runtime/factories/static"
	"v.io/x/ref/runtime/internal/flow/flowtest"
	fmanager "v.io/x/ref/runtime/internal/flow/manager"
	"v.io/x/ref/runtime/internal/rpc/benchmark/internal"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/test"
	"v.io/x/ref/test/benchmark"
	"v.io/x/ref/test/testutil"
)

const (
	payloadSize = 1000
	chunkCnt    = 10000

	bulkPayloadSize = 1000000

	numCPUs          = 2
	defaultBenchTime = 5 * time.Second
)

var (
	serverEP naming.Endpoint
	ctx      *context.T
)

// Benchmark for measuring RPC connection time including authentication.
//
// rpc.Client doesn't export an interface for closing connection. So we
// use the stream manager directly here.
func benchmarkRPCConnection(b *testing.B) {
	mp := runtime.GOMAXPROCS(numCPUs)
	defer runtime.GOMAXPROCS(mp)

	principal := testutil.NewPrincipal("test")
	nctx, _ := v23.WithPrincipal(ctx, principal)

	b.StopTimer()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if ref.RPCTransitionState() >= ref.XServers {
			mctx, cancel := context.WithCancel(nctx)
			m := fmanager.New(mctx, naming.FixedRoutingID(0xc), nil)
			b.StartTimer()
			_, err := m.Dial(mctx, serverEP, flowtest.AllowAllPeersAuthorizer{})
			if err != nil {
				ctx.Fatalf("Dial failed: %v", err)
			}
			b.StopTimer()
			cancel()
			<-m.Closed()
		} else {
			client := manager.InternalNew(ctx, naming.FixedRoutingID(0xc))
			b.StartTimer()
			_, err := client.Dial(nctx, serverEP)
			if err != nil {
				ctx.Fatalf("Dial failed: %v", err)
			}
			b.StopTimer()
			client.Shutdown()
		}
	}
}

// Benchmark for non-streaming RPC.
func benchmarkRPC(b *testing.B) {
	mp := runtime.GOMAXPROCS(numCPUs)
	defer runtime.GOMAXPROCS(mp)
	internal.CallEcho(b, ctx, serverEP.Name(), b.N, payloadSize, benchmark.NewStats(1))
}

// Benchmark for streaming RPC.
func benchmarkStreamingRPC(b *testing.B) {
	mp := runtime.GOMAXPROCS(numCPUs)
	defer runtime.GOMAXPROCS(mp)
	internal.CallEchoStream(b, ctx, serverEP.Name(), b.N, chunkCnt, payloadSize, benchmark.NewStats(1))
}

// Benchmark for measuring throughput in streaming RPC.
func benchmarkStreamingRPCThroughput(b *testing.B) {
	mp := runtime.GOMAXPROCS(numCPUs)
	defer runtime.GOMAXPROCS(mp)
	internal.CallEchoStream(b, ctx, serverEP.Name(), 1, b.N, bulkPayloadSize, benchmark.NewStats(1))
}

func msPerRPC(r testing.BenchmarkResult) float64 {
	return r.T.Seconds() / float64(r.N) * 1000
}

func rpcPerSec(r testing.BenchmarkResult) float64 {
	return float64(r.N) / r.T.Seconds()
}
func mbPerSec(r testing.BenchmarkResult) float64 {
	return (float64(r.Bytes) * float64(r.N) / 1e6) / r.T.Seconds()
}

func runBenchmarks() {
	r := testing.Benchmark(benchmarkRPCConnection)
	fmt.Printf("RPC Connection\t%.2f ms/rpc\n", msPerRPC(r))

	// Create a connection to exclude the setup time from the following benchmarks.
	internal.CallEcho(&testing.B{}, ctx, serverEP.Name(), 1, 0, benchmark.NewStats(1))

	r = testing.Benchmark(benchmarkRPC)
	fmt.Printf("RPC (echo %vB)\t%.2f ms/rpc (%.2f qps)\n", payloadSize, msPerRPC(r), rpcPerSec(r))

	r = testing.Benchmark(benchmarkStreamingRPC)
	fmt.Printf("RPC Streaming (echo %vB)\t%.2f ms/rpc\n", payloadSize, msPerRPC(r)/chunkCnt)

	r = testing.Benchmark(benchmarkStreamingRPCThroughput)
	fmt.Printf("RPC Streaming Throughput (echo %vMB)\t%.2f MB/s\n", bulkPayloadSize/1e6, mbPerSec(r))
}

func main() {
	// Set the default benchmark time.
	flag.Set("test.benchtime", defaultBenchTime.String())

	var shutdown v23.Shutdown
	ctx, shutdown = test.V23Init()
	defer shutdown()

	ctx, server, err := v23.WithNewServer(ctx, "", internal.NewService(), securityflag.NewAuthorizerOrDie())
	if err != nil {
		ctx.Fatalf("NewServer failed: %v", err)
	}
	serverEP = server.Status().Endpoints[0]

	runBenchmarks()
}
