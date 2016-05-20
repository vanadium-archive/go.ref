// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

import (
	"testing"
)

var (
	// Put these in package scope to prevent the optimizer optimizing away
	// any of the calls being tested.
	accumulate int
	err        error
)

func BenchmarkStats_FileSystemBytes_NoData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	b.ResetTimer()      // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		accumulate += int(bs.filesystemBytes())
	}

}

func BenchmarkStats_FileSystemBytes_SomeData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	write10kB(bs)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		accumulate += int(bs.filesystemBytes())
	}

}

func BenchmarkStats_FileSystemBytes_LotsOfData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	write100MB(bs)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		accumulate += int(bs.filesystemBytes())
	}

}

func BenchmarkStats_FileCount_NoData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	b.ResetTimer()      // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		accumulate += bs.fileCount()
	}

}

func BenchmarkStats_FileCount_SomeData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	write10kB(bs)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		accumulate += bs.fileCount()
	}

}

func BenchmarkStats_FileCount_LotsOfData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	write100MB(bs)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		accumulate += bs.fileCount()
	}

}

func BenchmarkStats_Stats_NoData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	b.ResetTimer()      // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		var c, s, r, w []int
		c, s, r, w, err = bs.stats()
		accumulate += len(c) + len(s) + len(r) + len(w)
	}

}

func BenchmarkStats_Stats_SomeData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	write10kB(bs)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		var c, s, r, w []int
		c, s, r, w, err = bs.stats()
		accumulate += len(c) + len(s) + len(r) + len(w)
	}

}

func BenchmarkStats_Stats_LotsOfData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	write100MB(bs)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		var c, s, r, w []int
		c, s, r, w, err = bs.stats()
		accumulate += c[0] + s[0] + r[0] + w[0]
	}

}

// For comparison with stats calls, benchmark single Get call on small DB.
func BenchmarkStats_Get_SomeData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	keys := write10kB(bs)
	n := len(keys)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		var val []byte
		val, err = bs.Get(keys[i%n], nil)
		accumulate += int(val[0])
	}

}

// For comparison with stats calls, benchmark single Get call on large DB.
func BenchmarkStats_Get_LotsOfData(b *testing.B) {
	bs, cleanup := newBatchStore()
	defer cleanup()
	defer b.StopTimer() // Called before cleanup() to avoid timing it.
	keys := write100MB(bs)
	n := len(keys)
	b.ResetTimer() // Ignore timing of init stuff above.

	for i := 0; i < b.N; i++ {
		var val []byte
		val, err = bs.Get(keys[i%n], nil)
		accumulate += int(val[0])
	}

}
