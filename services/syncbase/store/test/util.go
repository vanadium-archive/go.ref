// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"bytes"
	"reflect"
	"runtime/debug"
	"testing"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

// verifyGet verifies that st.Get(key) == value. If value is nil, verifies that
// the key is not found.
func verifyGet(t *testing.T, st store.StoreReader, key, value []byte) {
	valbuf := []byte("tmp")
	var err error
	if value != nil {
		if valbuf, err = st.Get(key, valbuf); err != nil {
			Fatalf(t, "can't get value of %q: %v", key, err)
		}
		if !bytes.Equal(valbuf, value) {
			Fatalf(t, "unexpected value: got %q, want %q", valbuf, value)
		}
	} else {
		valbuf, err = st.Get(key, valbuf)
		if !reflect.DeepEqual(&store.ErrUnknownKey{Key: string(key)}, err) {
			Fatalf(t, "unexpected get error for key %q: %v", key, err)
		}
		valcopy := []byte("tmp")
		// Verify that valbuf is not modified if the key is not found.
		if !bytes.Equal(valbuf, valcopy) {
			Fatalf(t, "unexpected value: got %q, want %q", valbuf, valcopy)
		}
	}
}

// verifyGet verifies the next key/value pair of the provided stream.
// If key is nil, verifies that next Advance call on the stream returns false.
func verifyAdvance(t *testing.T, s store.Stream, key, value []byte) {
	ok := s.Advance()
	if key == nil {
		if ok {
			Fatalf(t, "advance returned true unexpectedly")
		}
		return
	}
	if !ok {
		Fatalf(t, "can't advance the stream")
	}
	var k, v []byte
	for i := 0; i < 2; i++ {
		if k = s.Key(k); !bytes.Equal(k, key) {
			Fatalf(t, "unexpected key: got %q, want %q", k, key)
		}
		if v = s.Value(v); !bytes.Equal(v, value) {
			Fatalf(t, "unexpected value: got %q, want %q", v, value)
		}
	}
}

func Fatalf(t *testing.T, format string, args ...interface{}) {
	debug.PrintStack()
	t.Fatalf(format, args...)
}
