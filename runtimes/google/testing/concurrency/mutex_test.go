// concurrency_test is a simple test of the framework for systematic
// testing of concurrency.
package concurrency_test

import (
	"testing"
	"time"

	"veyron/runtimes/google/testing/concurrency"
	"veyron/runtimes/google/testing/concurrency/sync"
)

var m sync.Mutex

// empty is used as the test setup and cleanup function.
func empty() {}

// factorial computes the factorial of the given integer.
func factorial(n int) int {
	if n == 0 {
		return 1
	}
	return n * factorial(n-1)
}

// threadClosure folds the input arguments inside of the function body
// as the testing framework only supports functions with no arguments.
func threadClosure(t *testing.T, n, max int) func() {
	return func() {
		defer concurrency.Exit()
		if n < max {
			child := threadClosure(t, n+1, max)
			concurrency.Start(child)
		}
		m.Lock()
		m.Unlock()
	}
}

// TestOriginal runs the test without systematic testing of
// concurrency.
func TestOriginal(t *testing.T) {
	for n := 2; n < 6; n++ {
		thread := threadClosure(t, 1, n)
		thread()
	}
}

// TestExplore runs the test using the framework for systematic
// testing of concurrency, checking that the exploration explores the
// correct number of interleavings.
func TestExplore(t *testing.T) {
	for n := 2; n < 6; n++ {
		thread := threadClosure(t, 1, n)
		tester := concurrency.Init(empty, thread, empty)
		defer concurrency.Finish()
		niterations, err := tester.Explore()
		if err != nil {
			t.Fatalf("Unexpected error encountered: %v", err)
		}
		if niterations != factorial(n) {
			t.Fatalf("Unexpected number of iterations: expected %v, got %v", 2, niterations)
		}
		t.Logf("Explored %v iterations.", niterations)
	}
}

// TestExploreN runs the test using the framework for systematic
// testing of concurrency, checking that the exploration explores at
// most the given number of interleavings.
func TestExploreN(t *testing.T) {
	for n := 2; n < 6; n++ {
		thread := threadClosure(t, 1, n)
		tester := concurrency.Init(empty, thread, empty)
		defer concurrency.Finish()
		niterations, err := tester.ExploreN(100)
		if err != nil {
			t.Fatalf("Unexpected error encountered: %v", err)
		}
		if niterations != factorial(n) && niterations > 100 {
			t.Fatalf("Unexpected number of iterations: expected at most %v, got %v", 100, niterations)
		}
		t.Logf("Explored %v iterations.", niterations)
	}
}

// TestExploreFor runs the test using the framework for systematic
// testing of concurrency, checking that the exploration respects the
// given "soft" deadline.
func TestExploreFor(t *testing.T) {
	for n := 2; n < 6; n++ {
		thread := threadClosure(t, 1, n)
		tester := concurrency.Init(empty, thread, empty)
		defer concurrency.Finish()
		start := time.Now()
		deadline := 10 * time.Millisecond
		niterations, err := tester.ExploreFor(deadline)
		end := time.Now()
		if err != nil {
			t.Fatalf("Unexpected error encountered: %v", err)
		}
		if niterations != factorial(n) && start.Add(deadline).After(end) {
			t.Fatalf("Unexpected early termination: expected termination after %v, got %v", start.Add(deadline), end)
		}
		t.Logf("Explored %v iterations.", niterations)
	}
}
