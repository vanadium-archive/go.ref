package benchmarks_test

import (
	"testing"

	"veyron.io/veyron/veyron/runtimes/google/ipc/benchmarks"

	"veyron.io/veyron/veyron2/rt"
)

var runtime = rt.Init()

func RunBenchmark(b *testing.B, payloadSize int) {
	address, stop := benchmarks.StartServer(runtime, "tcp", "127.0.0.1:0")
	ctx := runtime.NewContext()
	defer stop()
	benchmarks.CallEcho(ctx, address, 1, 1, nil) // Create VC
	b.ResetTimer()
	benchmarks.CallEcho(ctx, address, b.N, payloadSize, nil)
}

func RunStreamBenchmark(b *testing.B, rpcCount, messageCount, payloadSize int) {
	address, stop := benchmarks.StartServer(runtime, "tcp", "127.0.0.1:0")
	defer stop()
	benchmarks.CallEchoStream(address, 1, 1, 1, nil) // Create VC
	b.ResetTimer()
	benchmarks.CallEchoStream(address, rpcCount, messageCount, payloadSize, nil)
}

func Benchmark____1B(b *testing.B) {
	RunBenchmark(b, 1)
}

func Benchmark___10B(b *testing.B) {
	RunBenchmark(b, 10)
}

func Benchmark__100B(b *testing.B) {
	RunBenchmark(b, 100)
}

func Benchmark___1KB(b *testing.B) {
	RunBenchmark(b, 1000)
}

func Benchmark__10KB(b *testing.B) {
	RunBenchmark(b, 10000)
}

func Benchmark_100KB(b *testing.B) {
	RunBenchmark(b, 100000)
}

func Benchmark_N_RPCs____1_chunk_____1B(b *testing.B) {
	RunStreamBenchmark(b, b.N, 1, 1)
}

func Benchmark_N_RPCs____1_chunk____10B(b *testing.B) {
	RunStreamBenchmark(b, b.N, 1, 10)
}

func Benchmark_N_RPCs____1_chunk___100B(b *testing.B) {
	RunStreamBenchmark(b, b.N, 1, 100)
}

func Benchmark_N_RPCs____1_chunk____1KB(b *testing.B) {
	RunStreamBenchmark(b, b.N, 1, 1000)
}

func Benchmark_N_RPCs____1_chunk___10KB(b *testing.B) {
	RunStreamBenchmark(b, b.N, 1, 10000)
}

func Benchmark_N_RPCs___10_chunks___1KB(b *testing.B) {
	RunStreamBenchmark(b, b.N, 10, 1000)
}

func Benchmark_N_RPCs__100_chunks___1KB(b *testing.B) {
	RunStreamBenchmark(b, b.N, 100, 1000)
}

func Benchmark_N_RPCs_1000_chunks___1KB(b *testing.B) {
	RunStreamBenchmark(b, b.N, 1000, 1000)
}

func Benchmark_1_RPC_N_chunks_____1B(b *testing.B) {
	RunStreamBenchmark(b, 1, b.N, 1)
}

func Benchmark_1_RPC_N_chunks____10B(b *testing.B) {
	RunStreamBenchmark(b, 1, b.N, 10)
}

func Benchmark_1_RPC_N_chunks___100B(b *testing.B) {
	RunStreamBenchmark(b, 1, b.N, 100)
}

func Benchmark_1_RPC_N_chunks____1KB(b *testing.B) {
	RunStreamBenchmark(b, 1, b.N, 1000)
}

func Benchmark_1_RPC_N_chunks___10KB(b *testing.B) {
	RunStreamBenchmark(b, 1, b.N, 10000)
}

func Benchmark_1_RPC_N_chunks__100KB(b *testing.B) {
	RunStreamBenchmark(b, 1, b.N, 100000)
}
