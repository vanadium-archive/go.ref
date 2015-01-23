package benchmark

import (
	"os"
	"testing"

	"v.io/core/veyron/lib/testutil/benchmark"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
)

var (
	serverAddr string
	ctx        *context.T
)

// Benchmarks for non-streaming RPC.
func runEcho(b *testing.B, payloadSize int) {
	CallEcho(b, ctx, serverAddr, b.N, payloadSize, benchmark.AddStats(b, 16))
}

func Benchmark____1B(b *testing.B) { runEcho(b, 1) }
func Benchmark___10B(b *testing.B) { runEcho(b, 10) }
func Benchmark__100B(b *testing.B) { runEcho(b, 100) }
func Benchmark___1KB(b *testing.B) { runEcho(b, 1000) }
func Benchmark__10KB(b *testing.B) { runEcho(b, 10000) }
func Benchmark_100KB(b *testing.B) { runEcho(b, 100000) }

// Benchmarks for streaming RPC.
func runEchoStream(b *testing.B, chunkCnt, payloadSize int) {
	CallEchoStream(b, ctx, serverAddr, b.N, chunkCnt, payloadSize, benchmark.AddStats(b, 16))
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
	CallEchoStream(b, ctx, serverAddr, 1, b.N, payloadSize, benchmark.NewStats(1))
}

func Benchmark__per_chunk____1B(b *testing.B) { runPerChunk(b, 1) }
func Benchmark__per_chunk___10B(b *testing.B) { runPerChunk(b, 10) }
func Benchmark__per_chunk__100B(b *testing.B) { runPerChunk(b, 100) }
func Benchmark__per_chunk___1KB(b *testing.B) { runPerChunk(b, 1000) }
func Benchmark__per_chunk__10KB(b *testing.B) { runPerChunk(b, 10000) }
func Benchmark__per_chunk_100KB(b *testing.B) { runPerChunk(b, 100000) }

// Benchmarks for non-streaming RPC while running streaming RPC in background.
func runMux(b *testing.B, payloadSize, chunkCntB, payloadSizeB int) {
	_, stop := StartEchoStream(&testing.B{}, ctx, serverAddr, 0, chunkCntB, payloadSizeB, benchmark.NewStats(1))
	CallEcho(b, ctx, serverAddr, b.N, payloadSize, benchmark.AddStats(b, 16))
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
	var shutdown veyron2.Shutdown
	ctx, shutdown = veyron2.Init()

	var err error
	ctx, err = veyron2.SetPrincipal(ctx, tsecurity.NewPrincipal("test-blessing"))
	if err != nil {
		panic(err)
	}

	var serverStop func()
	serverAddr, serverStop = StartServer(ctx, veyron2.GetListenSpec(ctx))

	// Create a VC to exclude the VC setup time from the benchmark.
	CallEcho(&testing.B{}, ctx, serverAddr, 1, 0, benchmark.NewStats(1))

	r := benchmark.RunTestMain(m)

	serverStop()
	shutdown()

	os.Exit(r)
}
