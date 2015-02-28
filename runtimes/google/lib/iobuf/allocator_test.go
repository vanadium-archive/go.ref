package iobuf

import (
	"fmt"
	"testing"
)

func TestAllocatorSmall(t *testing.T) {
	pool := NewPool(iobufSize)
	salloc := NewAllocator(pool, 0)
	const count = 100
	var slices [count]*Slice
	for i := 0; i != count; i++ {
		slices[i] = salloc.Copy([]byte(fmt.Sprintf("slice[%d]", i)))
	}
	for i := 0; i != count; i++ {
		expectEq(t, fmt.Sprintf("slice[%d]", i), string(slices[i].Contents))
		slices[i].Release()
	}
	salloc.Release()
}

func TestAllocatorLarge(t *testing.T) {
	pool := NewPool(iobufSize)
	salloc := NewAllocator(pool, 0)
	const count = 100
	var slices [count]*Slice
	for i := 0; i != count; i++ {
		slices[i] = salloc.Alloc(10000)
		copy(slices[i].Contents, []byte(fmt.Sprintf("slice[%d]", i)))
	}
	for i := 0; i != count; i++ {
		expected := fmt.Sprintf("slice[%d]", i)
		expectEq(t, expected, string(slices[i].Contents[0:len(expected)]))
		slices[i].Release()
	}
	salloc.Release()
}

// Check that the Allocator is unusable after it is closed.
func TestAllocatorClose(t *testing.T) {
	pool := NewPool(iobufSize)
	alloc := NewAllocator(pool, 0)
	slice := alloc.Alloc(10)
	if slice == nil {
		t.Fatalf("slice should not be nil")
	}
	slice.Release()
	alloc.Release()
	slice = alloc.Alloc(10)
	if slice != nil {
		t.Errorf("slice should be nil")
	}
	pool.Close()
}
