// A simple command-line tool to run IPC benchmarks.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/runtimes/google/ipc/benchmarks"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/rt"
)

var (
	vrt     veyron2.Runtime
	address string

	cpuList []int

	histogram = flag.Bool("histogram", false, "If ture, output histogram of the benchmark results.")
)

func runBenchmarkEcho() {
	payloadSizes := []int{1, 10, 100, 1000, 10000, 100000}

	for _, size := range payloadSizes {
		name := fmt.Sprintf("%sB", formatNum(size))
		runBenchmark(name, func(b *testing.B, stats *testutil.BenchStats) {
			benchmarks.CallEcho(b, vrt.NewContext(), address, b.N, size, stats)
		})
	}
}

func runBenchmarkEchoStream() {
	chunkCnts := []int{1, 10, 100, 1000}
	payloadSizes := []int{1, 10, 100, 1000, 10000, 100000}

	for _, cnt := range chunkCnts {
		for _, size := range payloadSizes {
			name := formatNum(cnt)
			if cnt == 1 {
				name += "_chunk_"
			} else {
				name += "_chunks"
			}
			name += fmt.Sprintf("%sB", formatNum(size))
			runBenchmark(name, func(b *testing.B, stats *testutil.BenchStats) {
				benchmarks.CallEchoStream(b, vrt.NewContext(), address, b.N, cnt, size, stats)
			})
		}
	}
}

func runBenchmarkEchoStreamPerChunk() {
	payloadSizes := []int{1, 10, 100, 1000, 10000, 100000}

	for _, size := range payloadSizes {
		name := fmt.Sprintf("__per_chunk%sB", formatNum(size))
		runBenchmark(name, func(b *testing.B, stats *testutil.BenchStats) {
			benchmarks.CallEchoStream(b, vrt.NewContext(), address, 1, b.N, size, stats)
		})
	}
}

func runBenchmarkMux() {
	payloadSizes := []int{10, 100}

	chunkCntsB := []int{100, 1000}
	payloadSizesB := []int{10, 100, 1000}

	for _, size := range payloadSizes {
		for _, cntB := range chunkCntsB {
			for _, sizeB := range payloadSizesB {
				name := fmt.Sprintf("%sB_mux%s", formatNum(size), formatNum(cntB))
				if cntB == 1 {
					name += "_chunk_"
				} else {
					name += "_chunks"
				}
				name += fmt.Sprintf("%sB", formatNum(sizeB))
				runBenchmark(name, func(b *testing.B, stats *testutil.BenchStats) {
					dummyB := testing.B{}
					_, stop := benchmarks.StartEchoStream(&dummyB, vrt.NewContext(), address, 0, cntB, sizeB, nil)

					b.ResetTimer()
					benchmarks.CallEcho(b, vrt.NewContext(), address, b.N, size, stats)
					b.StopTimer()

					stop()
				})
			}
		}
	}
}

// runBenchmark runs a single bencmark function 'f'.
func runBenchmark(name string, f func(*testing.B, *testutil.BenchStats)) {
	for _, cpu := range cpuList {
		runtime.GOMAXPROCS(cpu)

		benchName := "Benchmark" + name
		if cpu != 1 {
			benchName += fmt.Sprintf("-%d", cpu)
		}

		var stats *testutil.BenchStats
		if *histogram {
			stats = testutil.NewBenchStats(16)
		}

		r := testing.Benchmark(func(b *testing.B) {
			f(b, stats)
		})

		fmt.Printf("%s\t%s\n", benchName, r)
		if stats != nil {
			stats.Print(os.Stdout)
		}
	}
}

// formatNum formats the given number n with an optional metric prefix
// and padding with underscores if necessary.
func formatNum(n int) string {
	value := float32(n)
	var prefix string
	for _, p := range []string{"", "K", "M", "G", "T"} {
		if value < 1000 {
			prefix = p
			break
		}
		value /= 1000
	}

	return strings.Replace(fmt.Sprintf("%*.0f%s", 5-len(prefix), value, prefix), " ", "_", -1)
}

// parseCpuList looks up the existing '-test.cpu' flag value and parses it into
// the list of cpus.
func parseCpuList() {
	for _, v := range strings.Split(flag.Lookup("test.cpu").Value.String(), ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		cpu, err := strconv.Atoi(v)
		if err != nil || cpu <= 0 {
			fmt.Fprintf(os.Stderr, "invalid -test.cpu %q\n", v)
			os.Exit(1)
		}
		cpuList = append(cpuList, cpu)
	}
	if cpuList == nil {
		cpuList = append(cpuList, runtime.GOMAXPROCS(-1))
	}
}

func main() {
	flag.Parse()
	parseCpuList()

	var err error
	vrt, err = rt.New()
	if err != nil {
		panic(err)
	}
	defer vrt.Cleanup()

	var stop func()
	address, stop = benchmarks.StartServer(vrt, profiles.LocalListenSpec)

	benchmarks.CallEcho(&testing.B{}, vrt.NewContext(), address, 1, 0, nil) // Create VC.

	runBenchmarkEcho()
	runBenchmarkEchoStream()
	runBenchmarkEchoStreamPerChunk()
	runBenchmarkMux()

	stop()
}
