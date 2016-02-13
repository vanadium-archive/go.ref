// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark_test

import (
	"os"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	lsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/runtime/internal/rpc/benchmark/internal"
	"v.io/x/ref/services/xproxy/xproxy"
	"v.io/x/ref/test"
	"v.io/x/ref/test/benchmark"
)

var (
	serverAddr, tpcServerAddr, proxiedServerAddr string
	ctx, tpcCtx                                  *context.T
)

// Benchmarks for non-streaming RPC.
func runEcho(b *testing.B, payloadSize int) {
	internal.CallEcho(b, ctx, serverAddr, b.N, payloadSize, benchmark.AddStats(b, 16))
}

func Benchmark____1B(b *testing.B) { runEcho(b, 1) }
func Benchmark___10B(b *testing.B) { runEcho(b, 10) }
func Benchmark__100B(b *testing.B) { runEcho(b, 100) }
func Benchmark___1KB(b *testing.B) { runEcho(b, 1000) }
func Benchmark__10KB(b *testing.B) { runEcho(b, 10000) }
func Benchmark_100KB(b *testing.B) { runEcho(b, 100000) }

// Benchmark for non-streaming RPC with a Client ThirdPartyCaveat.
func runEchoClientTPC(b *testing.B, payloadSize int) {
	internal.CallEcho(b, tpcCtx, serverAddr, b.N, payloadSize, benchmark.AddStats(b, 16))
}

func Benchmark____1B_ClientTPCav(b *testing.B) { runEchoClientTPC(b, 1) }
func Benchmark___10B_ClientTPCav(b *testing.B) { runEchoClientTPC(b, 10) }
func Benchmark__100B_ClientTPCav(b *testing.B) { runEchoClientTPC(b, 100) }
func Benchmark___1KB_ClientTPCav(b *testing.B) { runEchoClientTPC(b, 1000) }
func Benchmark__10KB_ClientTPCav(b *testing.B) { runEchoClientTPC(b, 10000) }
func Benchmark_100KB_ClientTPCav(b *testing.B) { runEchoClientTPC(b, 100000) }

// Benchmark for non-streaming RPC with a Server ThirdPartyCaveat.
func runEchoServerTPC(b *testing.B, payloadSize int) {
	internal.CallEcho(b, ctx, tpcServerAddr, b.N, payloadSize, benchmark.AddStats(b, 16))
}

func Benchmark____1B_ServerTPCav(b *testing.B) { runEchoServerTPC(b, 1) }
func Benchmark___10B_ServerTPCav(b *testing.B) { runEchoServerTPC(b, 10) }
func Benchmark__100B_ServerTPCav(b *testing.B) { runEchoServerTPC(b, 100) }
func Benchmark___1KB_ServerTPCav(b *testing.B) { runEchoServerTPC(b, 1000) }
func Benchmark__10KB_ServerTPCav(b *testing.B) { runEchoServerTPC(b, 10000) }
func Benchmark_100KB_ServerTPCav(b *testing.B) { runEchoServerTPC(b, 100000) }

// Benchmark for non-streaming RPC with a proxied Server.
func runEchoProxiedServer(b *testing.B, payloadSize int) {
	internal.CallEcho(b, ctx, proxiedServerAddr, b.N, payloadSize, benchmark.AddStats(b, 16))
}

func Benchmark____1B_ProxiedServer(b *testing.B) { runEchoProxiedServer(b, 1) }
func Benchmark___10B_ProxiedServer(b *testing.B) { runEchoProxiedServer(b, 10) }
func Benchmark__100B_ProxiedServer(b *testing.B) { runEchoProxiedServer(b, 100) }
func Benchmark___1KB_ProxiedServer(b *testing.B) { runEchoProxiedServer(b, 1000) }
func Benchmark__10KB_ProxiedServer(b *testing.B) { runEchoProxiedServer(b, 10000) }
func Benchmark_100KB_ProxiedServer(b *testing.B) { runEchoProxiedServer(b, 100000) }

// Benchmarks for streaming RPC.
func runEchoStream(b *testing.B, chunkCnt, payloadSize int) {
	internal.CallEchoStream(b, ctx, serverAddr, b.N, chunkCnt, payloadSize, benchmark.AddStats(b, 16))
}

func Benchmark____1_chunk_____1B(b *testing.B) { runEchoStream(b, 1, 1) }
func Benchmark____1_chunk____10B(b *testing.B) { runEchoStream(b, 1, 10) }
func Benchmark____1_chunk___100B(b *testing.B) { runEchoStream(b, 1, 100) }
func Benchmark____1_chunk____1KB(b *testing.B) { runEchoStream(b, 1, 1000) }
func Benchmark____1_chunk___10KB(b *testing.B) { runEchoStream(b, 1, 10000) }
func Benchmark____1_chunk__100KB(b *testing.B) { runEchoStream(b, 1, 100000) }
func Benchmark___10_chunk_____1B(b *testing.B) { runEchoStream(b, 10, 1) }
func Benchmark___10_chunk____10B(b *testing.B) { runEchoStream(b, 10, 10) }
func Benchmark___10_chunk___100B(b *testing.B) { runEchoStream(b, 10, 100) }
func Benchmark___10_chunk____1KB(b *testing.B) { runEchoStream(b, 10, 1000) }
func Benchmark___10_chunk___10KB(b *testing.B) { runEchoStream(b, 10, 10000) }
func Benchmark___10_chunk__100KB(b *testing.B) { runEchoStream(b, 10, 100000) }
func Benchmark__100_chunk_____1B(b *testing.B) { runEchoStream(b, 100, 1) }
func Benchmark__100_chunk____10B(b *testing.B) { runEchoStream(b, 100, 10) }
func Benchmark__100_chunk___100B(b *testing.B) { runEchoStream(b, 100, 100) }
func Benchmark__100_chunk____1KB(b *testing.B) { runEchoStream(b, 100, 1000) }
func Benchmark__100_chunk___10KB(b *testing.B) { runEchoStream(b, 100, 10000) }
func Benchmark__100_chunk__100KB(b *testing.B) { runEchoStream(b, 100, 100000) }
func Benchmark___1K_chunk_____1B(b *testing.B) { runEchoStream(b, 1000, 1) }
func Benchmark___1K_chunk____10B(b *testing.B) { runEchoStream(b, 1000, 10) }
func Benchmark___1K_chunk___100B(b *testing.B) { runEchoStream(b, 1000, 100) }
func Benchmark___1K_chunk____1KB(b *testing.B) { runEchoStream(b, 1000, 1000) }
func Benchmark___1K_chunk___10KB(b *testing.B) { runEchoStream(b, 1000, 10000) }
func Benchmark___1K_chunk__100KB(b *testing.B) { runEchoStream(b, 1000, 100000) }

// Benchmarks for per-chunk throughput in streaming RPC.
func runPerChunk(b *testing.B, payloadSize int) {
	internal.CallEchoStream(b, ctx, serverAddr, 1, b.N, payloadSize, benchmark.NewStats(1))
}

func Benchmark__per_chunk____1B(b *testing.B) { runPerChunk(b, 1) }
func Benchmark__per_chunk___10B(b *testing.B) { runPerChunk(b, 10) }
func Benchmark__per_chunk__100B(b *testing.B) { runPerChunk(b, 100) }
func Benchmark__per_chunk___1KB(b *testing.B) { runPerChunk(b, 1000) }
func Benchmark__per_chunk__10KB(b *testing.B) { runPerChunk(b, 10000) }
func Benchmark__per_chunk_100KB(b *testing.B) { runPerChunk(b, 100000) }

// Benchmarks for non-streaming RPC while running streaming RPC in background.
func runMux(b *testing.B, payloadSize, chunkCntB, payloadSizeB int) {
	_, stop := internal.StartEchoStream(&testing.B{}, ctx, serverAddr, 0, chunkCntB, payloadSizeB, benchmark.NewStats(1))
	internal.CallEcho(b, ctx, serverAddr, b.N, payloadSize, benchmark.AddStats(b, 16))
	stop()
}

func Benchmark___10B_mux__100_chunks___10B(b *testing.B) { runMux(b, 10, 100, 10) }
func Benchmark___10B_mux__100_chunks__100B(b *testing.B) { runMux(b, 10, 100, 100) }
func Benchmark___10B_mux__100_chunks___1KB(b *testing.B) { runMux(b, 10, 100, 1000) }
func Benchmark___10B_mux__100_chunks__10KB(b *testing.B) { runMux(b, 10, 100, 10000) }
func Benchmark___10B_mux___1K_chunks___10B(b *testing.B) { runMux(b, 10, 1000, 10) }
func Benchmark___10B_mux___1K_chunks__100B(b *testing.B) { runMux(b, 10, 1000, 100) }
func Benchmark___10B_mux___1K_chunks___1KB(b *testing.B) { runMux(b, 10, 1000, 1000) }
func Benchmark___10B_mux___1K_chunks__10KB(b *testing.B) { runMux(b, 10, 1000, 10000) }
func Benchmark___1KB_mux__100_chunks___10B(b *testing.B) { runMux(b, 1000, 100, 10) }
func Benchmark___1KB_mux__100_chunks__100B(b *testing.B) { runMux(b, 1000, 100, 100) }
func Benchmark___1KB_mux__100_chunks___1KB(b *testing.B) { runMux(b, 1000, 100, 1000) }
func Benchmark___1KB_mux__100_chunks__10KB(b *testing.B) { runMux(b, 1000, 100, 10000) }
func Benchmark___1KB_mux___1K_chunks___10B(b *testing.B) { runMux(b, 1000, 1000, 10) }
func Benchmark___1KB_mux___1K_chunks__100B(b *testing.B) { runMux(b, 1000, 1000, 100) }
func Benchmark___1KB_mux___1K_chunks___1KB(b *testing.B) { runMux(b, 1000, 1000, 1000) }
func Benchmark___1KB_mux___1K_chunks__10KB(b *testing.B) { runMux(b, 1000, 1000, 10000) }

// A single empty test to avoid:
// testing: warning: no tests to run
// from showing up when running benchmarks in this package via "go test"
func TestNoOp(t *testing.T) {}

func TestMain(m *testing.M) {
	// We do not use defer here since this program will exit at the end of
	// this function through os.Exit().
	var shutdown v23.Shutdown
	ctx, shutdown = test.V23Init()

	setupServerClient(ctx)
	setupThirdPartyCaveatServerClient(ctx)
	setupProxiedServerClient(ctx)

	r := benchmark.RunTestMain(m)
	shutdown()
	os.Exit(r)
}

func setupServerClient(ctx *context.T) {
	// Server with no ThirdPartyCaveat.
	_, server, err := v23.WithNewServer(ctx, "", internal.NewService(), securityflag.NewAuthorizerOrDie())
	if err != nil {
		ctx.Fatalf("NewServer failed: %v", err)
	}
	serverAddr = server.Status().Endpoints[0].Name()
	// Create a Conn to exclude the Conn setup time from the benchmark.
	internal.CallEcho(&testing.B{}, ctx, serverAddr, 1, 0, benchmark.NewStats(1))
}

func setupThirdPartyCaveatServerClient(ctx *context.T) {
	// Create a DischargeServer and a principal with a corresponding ThirdPartyCaveat.
	tpcav := internal.NewDischargeServer(ctx)
	p := v23.GetPrincipal(ctx)
	b, _ := p.BlessingStore().Default()
	tpcavb, err := p.Bless(p.PublicKey(), b, "tpcav", tpcav)
	if err != nil {
		ctx.Fatal(err)
	}
	store := lsecurity.FixedBlessingsStore(tpcavb, nil)
	tpcavp, err := lsecurity.ForkPrincipal(p, store, p.Roots())
	if err != nil {
		ctx.Fatal(err)
	}
	tpcCtx, err = v23.WithPrincipal(ctx, tpcavp)
	if err != nil {
		ctx.Fatal(err)
	}

	// Create a server with the ThirdPartyCaveat principal.
	_, tpcServer, err := v23.WithNewServer(tpcCtx, "", internal.NewService(), securityflag.NewAuthorizerOrDie())
	if err != nil {
		ctx.Fatalf("NewServer failed: %v", err)
	}
	tpcServerAddr = tpcServer.Status().Endpoints[0].Name()

	// Create Conns to exclude the Conn setup time from the benchmark.
	internal.CallEcho(&testing.B{}, tpcCtx, serverAddr, 1, 0, benchmark.NewStats(1))
	internal.CallEcho(&testing.B{}, ctx, tpcServerAddr, 1, 0, benchmark.NewStats(1))
}

func setupProxiedServerClient(ctx *context.T) {
	// Create a proxy.
	pxy, err := xproxy.New(ctx, "", securityflag.NewAuthorizerOrDie())
	if err != nil {
		ctx.Fatal(err)
	}
	pname := pxy.ListeningEndpoints()[0].Name()
	// Create a server that listens on the proxy.
	pCtx := v23.WithListenSpec(ctx, rpc.ListenSpec{Proxy: pname})
	_, proxiedServer, err := v23.WithNewServer(pCtx, "", internal.NewService(), securityflag.NewAuthorizerOrDie())
	if err != nil {
		ctx.Fatal(err)
	}
	for {
		status := proxiedServer.Status()
		if status.Endpoints[0].Addr().Network() != "bidi" {
			proxiedServerAddr = status.Endpoints[0].Name()
			break
		}
		<-status.Valid
	}
	// Create Conns to exclude the Conn setup time from the benchmark.
	internal.CallEcho(&testing.B{}, ctx, proxiedServerAddr, 1, 0, benchmark.NewStats(1))
}
