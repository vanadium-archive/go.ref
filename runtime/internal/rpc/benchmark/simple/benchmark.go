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
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/ibe"
	"v.io/x/ref/lib/security/bcrypter"
	"v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/runtime/internal/flow/flowtest"
	fmanager "v.io/x/ref/runtime/internal/flow/manager"
	"v.io/x/ref/runtime/internal/rpc/benchmark/internal"
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

var ctx *context.T

type benchFun func(b *testing.B)

func newServer(ctx *context.T, opts ...rpc.ServerOpt) (*context.T, naming.Endpoint) {
	ctx, server, err := v23.WithNewServer(ctx, "", internal.NewService(), securityflag.NewAuthorizerOrDie(), opts...)
	if err != nil {
		ctx.Fatalf("NewServer failed: %v", err)
	}
	return ctx, server.Status().Endpoints[0]
}

// Benchmark for measuring RPC connection time including authentication.
//
// rpc.Client doesn't export an interface for closing connection. So we
// use the stream manager directly here.
func benchmarkRPCConnection(b *testing.B) {
	mp := runtime.GOMAXPROCS(numCPUs)
	defer runtime.GOMAXPROCS(mp)

	ctx, serverEP := newServer(ctx)

	principal := testutil.NewPrincipal("test")
	nctx, _ := v23.WithPrincipal(ctx, principal)

	b.StopTimer()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mctx, cancel := context.WithCancel(nctx)
		m := fmanager.New(mctx, naming.FixedRoutingID(0xc), nil, 0, 0)
		b.StartTimer()
		_, err := m.Dial(mctx, serverEP, flowtest.AllowAllPeersAuthorizer{}, 0)
		if err != nil {
			ctx.Fatalf("Dial failed: %v", err)
		}
		b.StopTimer()
		cancel()
		<-m.Closed()
	}
}

// Benchmark for measuring RPC connection time when using private mutual
// authentication. 'serverAuth' is the authorization policy used by the
// server while revealing its blessings, and 'clientBlessing' is the blessing
// used by the client.
//
// The specific protocol being benchmarked is Protocol 3 from the doc:
// https://docs.google.com/document/d/1FpLJSiKy4sXxRUSZh1BQrhUEn7io-dGW7y-DMszI21Q/edit
func benchmarkPrivateRPCConnection(ctx *context.T, serverAuth []security.BlessingPattern, clientBlessing string) benchFun {
	return func(b *testing.B) {
		mp := runtime.GOMAXPROCS(numCPUs)
		defer runtime.GOMAXPROCS(mp)

		ctx, privateServerEP := newServer(ctx, options.ServerPeers(serverAuth))

		principal := testutil.NewPrincipal(clientBlessing)
		nctx, _ := v23.WithPrincipal(ctx, principal)

		b.StopTimer()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mctx, cancel := context.WithCancel(nctx)
			m := fmanager.New(mctx, naming.FixedRoutingID(0xc), nil, 0, 0)
			b.StartTimer()
			_, err := m.Dial(nctx, privateServerEP, flowtest.AllowAllPeersAuthorizer{}, 0)
			if err != nil {
				ctx.Fatalf("Dial failed: %v", err)
			}
			b.StopTimer()
			cancel()
			<-m.Closed()
		}
	}
}

// Benchmark for non-streaming RPC.
func benchmarkRPC(b *testing.B) {
	ctx, serverEP := newServer(ctx)
	mp := runtime.GOMAXPROCS(numCPUs)
	defer runtime.GOMAXPROCS(mp)
	internal.CallEcho(b, ctx, serverEP.Name(), b.N, payloadSize, benchmark.NewStats(1))
}

// Benchmark for streaming RPC.
func benchmarkStreamingRPC(b *testing.B) {
	ctx, serverEP := newServer(ctx)
	mp := runtime.GOMAXPROCS(numCPUs)
	defer runtime.GOMAXPROCS(mp)
	internal.CallEchoStream(b, ctx, serverEP.Name(), b.N, chunkCnt, payloadSize, benchmark.NewStats(1))
}

// Benchmark for measuring throughput in streaming RPC.
func benchmarkStreamingRPCThroughput(b *testing.B) {
	ctx, serverEP := newServer(ctx)
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

	master, err := ibe.SetupBB1()
	if err != nil {
		ctx.Fatalf("ibe.SetupBB1 failed: %v", err)
	}
	root := bcrypter.NewRoot("root", master)
	clientBlessing := "root:alice:client"

	// Attach a crypter to the context, and add a blessing private
	// key to the for 'clientBlesing'.
	crypter := bcrypter.NewCrypter()
	cctx := bcrypter.WithCrypter(ctx, crypter)
	key, err := root.Extract(ctx, clientBlessing)
	if err != nil {
		ctx.Fatalf("could not extract private key: %v", err)
	}
	if err := crypter.AddKey(cctx, key); err != nil {
		ctx.Fatalf("could not add key to crypter: %v", err)
	}

	serverAuthPatterns := [][]security.BlessingPattern{
		[]security.BlessingPattern{"root:alice"},
		[]security.BlessingPattern{"root:bob:friend", "root:carol:friend", "root:alice:client"},
		[]security.BlessingPattern{"root:bob:spouse", "root:bob:enemy", "root:carol:spouse", "root:carol:enemy", "root:alice:client:$"},
	}
	for _, serverAuth := range serverAuthPatterns {
		r = testing.Benchmark(benchmarkPrivateRPCConnection(cctx, serverAuth, clientBlessing))
		fmt.Printf("Private RPC Connection with server authorization policy %v and client blessing %v \t%.2f ms/rpc\n", serverAuth, clientBlessing, msPerRPC(r))
	}

	// Create a connection to exclude the setup time from the following benchmarks.
	ctx, serverEP := newServer(ctx)
	internal.CallEcho(&testing.B{}, ctx, serverEP.Name(), 1, 0, benchmark.NewStats(1))

	r = testing.Benchmark(benchmarkRPC)
	fmt.Printf("RPC (echo %vB)\t%.2f ms/rpc (%.2f qps)\n", payloadSize, msPerRPC(r), rpcPerSec(r))

	r = testing.Benchmark(benchmarkStreamingRPC)
	fmt.Printf("RPC Streaming (echo %vB)\t%.2f ms/rpc\n", payloadSize, msPerRPC(r)/chunkCnt)

	r = testing.Benchmark(benchmarkStreamingRPCThroughput)
	fmt.Printf("RPC Streaming Throughput (echo %vMB)\t%.2f MB/s\n", bulkPayloadSize/1e6, mbPerSec(r))
}

func realMain() {
	// Set the default benchmark time.
	flag.Set("test.benchtime", defaultBenchTime.String())

	var shutdown v23.Shutdown
	ctx, shutdown = test.V23Init()
	defer shutdown()

	runBenchmarks()
}
