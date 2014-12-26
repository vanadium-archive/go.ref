package benchmarks_test

import (
	"testing"

	"v.io/core/veyron/profiles"
	"v.io/core/veyron/runtimes/google/ipc/benchmarks"

	"v.io/core/veyron2"
	"v.io/core/veyron2/rt"
)

var vrt veyron2.Runtime
var address string

func init() {
	var err error
	vrt, err = rt.New()
	if err != nil {
		panic(err)
	}

	address, _ = benchmarks.StartServer(vrt, profiles.LocalListenSpec)
}

func runBenchmarkEcho(b *testing.B, payloadSize int) {
	benchmarks.CallEcho(&testing.B{}, vrt.NewContext(), address, 1, 0, nil) // Create VC.
	benchmarks.CallEcho(b, vrt.NewContext(), address, b.N, payloadSize, nil)
}

func runBenchmarkEchoStream(b *testing.B, iterations, chunkCnt, payloadSize int) {
	benchmarks.CallEcho(&testing.B{}, vrt.NewContext(), address, 1, 0, nil) // Create VC.
	benchmarks.CallEchoStream(b, vrt.NewContext(), address, iterations, chunkCnt, payloadSize, nil)
}

func runBenchmarkMux(b *testing.B, payloadSize, chunkCntB, payloadSizeB int) {
	benchmarks.CallEcho(&testing.B{}, vrt.NewContext(), address, 1, 0, nil) // Create VC.
	_, stop := benchmarks.StartEchoStream(&testing.B{}, vrt.NewContext(), address, 0, chunkCntB, payloadSizeB, nil)
	benchmarks.CallEcho(b, vrt.NewContext(), address, b.N, payloadSize, nil)
	stop()
}

// Benchmarks for non-streaming RPC.
func Benchmark____1B(b *testing.B) {
	runBenchmarkEcho(b, 1)
}

func Benchmark___10B(b *testing.B) {
	runBenchmarkEcho(b, 10)
}

func Benchmark___1KB(b *testing.B) {
	runBenchmarkEcho(b, 1000)
}

func Benchmark_100KB(b *testing.B) {
	runBenchmarkEcho(b, 100000)
}

// Benchmarks for streaming RPC.
func Benchmark____1_chunk_____1B(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 1, 1)
}

func Benchmark____1_chunk____10B(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 1, 10)
}

func Benchmark____1_chunk____1KB(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 1, 1000)
}

func Benchmark____1_chunk___10KB(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 1, 10000)
}

func Benchmark___10_chunks____1B(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 10, 1)
}

func Benchmark___10_chunks___10B(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 10, 10)
}

func Benchmark___10_chunks___1KB(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 10, 1000)
}

func Benchmark___10_chunks__10KB(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 10, 10000)
}

func Benchmark__100_chunks____1B(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 100, 1)
}

func Benchmark__100_chunks___10B(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 100, 10)
}

func Benchmark__100_chunks___1KB(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 100, 1000)
}

func Benchmark__100_chunks__10KB(b *testing.B) {
	runBenchmarkEchoStream(b, b.N, 100, 10000)
}

// Benchmarks for per-chunk throughput in streaming RPC.
func Benchmark__per_chunk____1B(b *testing.B) {
	runBenchmarkEchoStream(b, 1, b.N, 1)
}

func Benchmark__per_chunk___10B(b *testing.B) {
	runBenchmarkEchoStream(b, 1, b.N, 10)
}

func Benchmark__per_chunk___1KB(b *testing.B) {
	runBenchmarkEchoStream(b, 1, b.N, 1000)
}

func Benchmark__per_chunk__10KB(b *testing.B) {
	runBenchmarkEchoStream(b, 1, b.N, 10000)
}

// Benchmarks for non-streaming RPC while running streaming RPC in background.
func Benchmark____1B_mux___10_chunks___10B(b *testing.B) {
	runBenchmarkMux(b, 1, 10, 10)
}

func Benchmark____1B_mux___10_chunks___1KB(b *testing.B) {
	runBenchmarkMux(b, 1, 10, 1000)
}

func Benchmark____1B_mux__100_chunks___10B(b *testing.B) {
	runBenchmarkMux(b, 1, 100, 10)
}

func Benchmark____1B_mux__100_chunks___1KB(b *testing.B) {
	runBenchmarkMux(b, 1, 100, 1000)
}
