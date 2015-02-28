package benchmark

import (
	"os"
	"testing"

	"v.io/x/ref/lib/testutil/benchmark"
)

// A single empty test to avoid:
// testing: warning: no tests to run
// from showing up when running benchmarks in this package via "go test"
func TestNoOp(t *testing.T) {}

func TestMain(m *testing.M) {
	os.Exit(benchmark.RunTestMain(m))
}
