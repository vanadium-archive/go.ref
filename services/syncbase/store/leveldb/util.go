// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package leveldb

// #include "leveldb/c.h"
import "C"
import (
	"errors"
	"reflect"
	"unsafe"
)

// goError copies C error into Go heap and frees C buffer.
func goError(cError *C.char) error {
	if cError == nil {
		return nil
	}
	err := errors.New(C.GoString(cError))
	C.leveldb_free(unsafe.Pointer(cError))
	return err
}

// cSlice converts Go []byte to C string without copying the data.
//
// This function behaves similarly to standard Go slice copying or sub-slicing;
// the caller need not worry about ownership or garbage collection.
func cSlice(str []byte) (*C.char, C.size_t) {
	if len(str) == 0 {
		return nil, 0
	}
	data := unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&str)).Data)
	return (*C.char)(data), C.size_t(len(str))
}

// goBytes converts C string to Go []byte without copying the data.
// This function behaves similarly to cSlice.
func goBytes(str *C.char, size C.size_t) []byte {
	ptr := unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(str)),
		Len:  int(size),
		Cap:  int(size),
	})
	return *(*[]byte)(ptr)
}

// copyAll copies elements from a source slice into a destination slice.
// The returned slice may be a sub-slice of dst if dst was large enough to hold
// src. Otherwise, a newly allocated slice will be returned.
func copyAll(dst, src []byte) []byte {
	if cap(dst) < len(src) {
		newlen := cap(dst)*2 + 2
		if newlen < len(src) {
			newlen = len(src)
		}
		dst = make([]byte, newlen)
	}
	copy(dst, src)
	return dst[:len(src)]
}
