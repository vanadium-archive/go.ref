package iobuf

import (
	"testing"
)

func TestExpandFront(t *testing.T) {
	pool := NewPool(iobufSize)
	calloc := NewAllocator(pool, 8)
	slice := calloc.Alloc(10)
	if slice.Size() != 10 {
		t.Errorf("Expected length 10, got %d", slice.Size())
	}
	copy(slice.Contents, []byte("0123456789"))
	ok := slice.ExpandFront(8)
	if !ok {
		t.Errorf("Expected ExpandFront to succeed")
	}
	if slice.Size() != 18 {
		t.Errorf("Expected length 18, got %d", slice.Size())
	}
	slice.TruncateFront(11)
	if slice.Size() != 7 {
		t.Errorf("Expected length 9, got %d", slice.Size())
	}
	ok = slice.ExpandFront(3)
	if slice.Size() != 10 {
		t.Errorf("Expected length 10, got %d", slice.Size())
	}
	if string(slice.Contents) != "0123456789" {
		t.Errorf("Expected 0123456789, got %q", string(slice.Contents))
	}
	ok = slice.ExpandFront(10)
	if ok {
		t.Errorf("Expected expansion to fail")
	}
}

func TestCoalesce(t *testing.T) {
	pool := NewPool(iobufSize)
	salloc := NewAllocator(pool, 0)
	const count = 100
	const blocksize = 1024
	var slices [count]*Slice
	for i := 0; i != count; i++ {
		var block [blocksize]byte
		for j := 0; j != blocksize; j++ {
			block[j] = charAt(i*blocksize + j)
		}
		slices[i] = salloc.Copy(block[:])
	}
	coalesced := Coalesce(slices[:], blocksize*4)
	expectEq(t, count/4, len(coalesced))

	off := 0
	for _, buf := range coalesced {
		checkBuf(t, buf.Contents, off)
		off += len(buf.Contents)
		buf.Release()
	}

	salloc.Release()
}

func charAt(i int) byte {
	return "0123456789abcedf"[i%16]
}

func checkBuf(t *testing.T, buf []byte, off int) {
	for i, c := range buf {
		expectEq(t, charAt(off+i), c)
	}
}
