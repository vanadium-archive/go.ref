package ipc

import (
	"sync"
	"testing"
	"time"

	"veyron.io/veyron/veyron/runtimes/google/testing/mocks/runtime"
	"veyron.io/veyron/veyron/runtimes/google/vtrace"

	"veyron.io/veyron/veyron2/context"
)

// We need a special way to create contexts for tests.  We
// can't create a real runtime in the runtime implementation
// so we use a fake one that panics if used.  The runtime
// implementation should not ever use the Runtime from a context.
func testContext() context.T {
	ctx := InternalNewContext(&runtime.PanicRuntime{})
	ctx, _ = vtrace.WithNewSpan(ctx, "Root")
	ctx, _ = ctx.WithDeadline(time.Now().Add(20 * time.Second))
	return ctx
}

func testCancel(t *testing.T, ctx context.T, cancel context.CancelFunc) {
	select {
	case <-ctx.Done():
		t.Errorf("Done closed when deadline not yet passed")
	default:
	}
	ch := make(chan bool, 0)
	go func() {
		cancel()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out witing for cancel.")
	}

	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("timed out witing for cancellation.")
	}
	if err := ctx.Err(); err != context.Canceled {
		t.Errorf("Unexpected error want %v, got %v", context.Canceled, err)
	}
}

func TestCancelContext(t *testing.T) {
	ctx, cancel := testContext().WithCancel()
	testCancel(t, ctx, cancel)
}

func TestMultiLevelCancelContext(t *testing.T) {
	c0, c0Cancel := testContext().WithCancel()
	c1, _ := c0.WithCancel()
	c2, _ := c1.WithCancel()
	c3, _ := c2.WithCancel()
	testCancel(t, c3, c0Cancel)
}

type nonStandardContext struct {
	context.T
}

func (n *nonStandardContext) WithCancel() (ctx context.T, cancel context.CancelFunc) {
	return newCancelContext(n)
}
func (n *nonStandardContext) WithDeadline(deadline time.Time) (context.T, context.CancelFunc) {
	return newDeadlineContext(n, deadline)
}
func (n *nonStandardContext) WithTimeout(timeout time.Duration) (context.T, context.CancelFunc) {
	return newDeadlineContext(n, time.Now().Add(timeout))
}
func (n *nonStandardContext) WithValue(key interface{}, val interface{}) context.T {
	return newValueContext(n, key, val)
}

func TestCancelContextWithNonStandard(t *testing.T) {
	c0, c0Cancel := testContext().WithCancel()
	c1 := &nonStandardContext{c0}
	c2 := &nonStandardContext{c1}
	c3, _ := c2.WithCancel()
	testCancel(t, c3, c0Cancel)
}

func testDeadline(t *testing.T, ctx context.T, start time.Time, desiredTimeout time.Duration) {
	<-ctx.Done()
	if delta := time.Now().Sub(start); delta < desiredTimeout {
		t.Errorf("Deadline too short want %s got %s", desiredTimeout, delta)
	}
	if err := ctx.Err(); err != context.DeadlineExceeded {
		t.Errorf("Unexpected error want %s, got %s", context.DeadlineExceeded, err)
	}
}

func TestDeadlineContext(t *testing.T) {
	cases := []time.Duration{
		10 * time.Millisecond,
		0,
	}
	for _, desiredTimeout := range cases {
		start := time.Now()
		ctx, _ := testContext().WithDeadline(start.Add(desiredTimeout))
		testDeadline(t, ctx, start, desiredTimeout)
	}

	ctx, cancel := testContext().WithDeadline(time.Now().Add(100 * time.Hour))
	testCancel(t, ctx, cancel)
}

func TestDeadlineContextWithRace(t *testing.T) {
	ctx, cancel := testContext().WithDeadline(time.Now().Add(100 * time.Hour))
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			cancel()
			wg.Done()
		}()
	}
	wg.Wait()
	<-ctx.Done()
	if err := ctx.Err(); err != context.Canceled {
		t.Errorf("Unexpected error want %v, got %v", context.Canceled, err)
	}
}

func TestValueContext(t *testing.T) {
	type testContextKey int
	const (
		key1 = testContextKey(iota)
		key2
		key3
		key4
	)
	const (
		val1 = iota
		val2
		val3
	)
	ctx1 := testContext().WithValue(key1, val1)
	ctx2 := ctx1.WithValue(key2, val2)
	ctx3 := ctx2.WithValue(key3, val3)

	expected := map[interface{}]interface{}{
		key1: val1,
		key2: val2,
		key3: val3,
		key4: nil,
	}
	for k, v := range expected {
		if got := ctx3.Value(k); got != v {
			t.Errorf("Got wrong value for %v: want %v got %v", k, v, got)
		}
	}

}
