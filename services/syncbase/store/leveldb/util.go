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

// cSlice converts Go string to C string without copying the data.
//
// This function behaves similarly to standard Go slice copying or sub-slicing;
// the caller need not worry about ownership or garbage collection.
func cSlice(str string) (*C.char, C.size_t) {
	if len(str) == 0 {
		return nil, 0
	}
	data := unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&str)).Data)
	return (*C.char)(data), C.size_t(len(str))
}

// cSliceFromBytes converts Go []byte to C string without copying the data.
// This function behaves similarly to cSlice.
func cSliceFromBytes(bytes []byte) (*C.char, C.size_t) {
	if len(bytes) == 0 {
		return nil, 0
	}
	data := unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&bytes)).Data)
	return (*C.char)(data), C.size_t(len(bytes))
}

// goString converts C string to Go string without copying the data.
// This function behaves similarly to cSlice.
func goString(str *C.char, size C.size_t) string {
	ptr := unsafe.Pointer(&reflect.StringHeader{
		Data: uintptr(unsafe.Pointer(str)),
		Len:  int(size),
	})
	return *(*string)(ptr)
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
