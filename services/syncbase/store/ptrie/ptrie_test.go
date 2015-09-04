// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO(rogulenko): Add a benchmark to compare a ptrie with
// a map[[]byte]interface{}.
package ptrie

import (
	"testing"
)

// TestPutGetDelete verifies basic functionality of Put/Get/Delete.
// More Put/Get/Delete/Scan tests can be found in store/memstore.
// TODO(rogulenko): Add more tests, don't rely on tests in store/memstore.
func TestPutGetDelete(t *testing.T) {
	data := New(true)
	data.Put([]byte("a"), "a")
	data.Put([]byte("ab"), "ab")
	if got, want := data.Get([]byte("a")).(string), "a"; got != want {
		t.Fatalf("unexpected Get result: got %q, want %q", got, want)
	}
	if got, want := data.Get([]byte("ab")).(string), "ab"; got != want {
		t.Fatalf("unexpected Get result: got %q, want %q", got, want)
	}
	// Verify that copy-on-write works.
	newData := data.Copy()
	newData.Delete([]byte("a"))
	if got, want := data.Get([]byte("a")).(string), "a"; got != want {
		t.Fatalf("unexpected Get result: got %q, want %q", got, want)
	}
	if value := newData.Get([]byte("a")); value != nil {
		t.Fatalf("Get returned a non-nil value %v", value)
	}
	// Verify path contraction after Delete().
	if newData.root.child[0].bitlen != 16 {
		t.Fatal("path was not contracted after Delete()")
	}
	// Verify path contraction after Put("ac") and Delete("ac").
	data = newData.Copy()
	data.Put([]byte("ac"), "ac")
	if got, want := data.Get([]byte("ac")).(string), "ac"; got != want {
		t.Fatalf("unexpected Get result: got %q, want %q", got, want)
	}
	data.Delete([]byte("ab"))
	if data.root.child[0].bitlen != 16 {
		t.Fatal("path was not contracted after Delete()")
	}
}
