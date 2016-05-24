// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build java android
// +build cgo

package main

import (
	"fmt"
	"unsafe"
)

// #include <stdlib.h>
// #include "jni_wrapper.h"
// #include "lib.h"
import "C"

func JFindClass(env *C.JNIEnv, name string) (C.jclass, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	class := C.FindClass(env, cName)
	if C.ExceptionOccurred(env) != nil {
		return nil, fmt.Errorf("couldn't find class %s", name)
	}

	globalRef := C.jclass(C.NewGlobalRef(env, class))
	if globalRef == nil {
		return nil, fmt.Errorf("couldn't allocate a global reference for class %s", name)
	}
	return globalRef, nil
}

func JGetMethodID(env *C.JNIEnv, cls C.jclass, name, sig string) (C.jmethodID, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	cSig := C.CString(sig)
	defer C.free(unsafe.Pointer(cSig))

	method := C.GetMethodID(env, cls, cName, cSig)
	if method == nil {
		return nil, fmt.Errorf("couldn't get method %q with signature %s", name, sig)
	}

	// Note: the validity of the method is bounded by the lifetime of the
	// ClassLoader that did the loading of the class.
	return method, nil
}

func JGetFieldID(env *C.JNIEnv, cls C.jclass, name, sig string) (C.jfieldID, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	cSig := C.CString(sig)
	defer C.free(unsafe.Pointer(cSig))

	field := C.GetFieldID(env, cls, cName, cSig)
	if field == nil {
		return nil, fmt.Errorf("couldn't get field %q with signature ", name, sig)
	}

	// Note: the validity of the field is bounded by the lifetime of the
	// ClassLoader that did the loading of the class.
	return field, nil
}

// V23SStringToJString constructs a C.jstring from a C.v23_syncbase_String. The
// code is somewhat complicated and inefficient because the NewStringUTF from
// JNI only works with modified UTF-8 strings (inner nulls are encoded as 0xC0,
// 0x80 and the string is terminated with a null).
func V23SStringToJString(env *C.JNIEnv, src C.v23_syncbase_String) (C.jstring, error) {
	n := int(src.n)
	srcPtr := uintptr(unsafe.Pointer(src.p))
	numNulls := 0
	for i := 0; i < n; i++ {
		if *(*byte)(unsafe.Pointer(srcPtr + uintptr(i))) == 0 {
			numNulls++
		}
	}
	tmp := C.malloc(C.size_t(n + numNulls + 1))
	defer C.free(tmp)
	tmpPtr := uintptr(tmp)
	j := 0
	for i := 0; i < n; i, j = i+1, j+1 {
		if *(*byte)(unsafe.Pointer(srcPtr + uintptr(i))) != 0 {
			*(*byte)(unsafe.Pointer(tmpPtr + uintptr(j))) = *(*byte)(unsafe.Pointer(srcPtr + uintptr(i)))
			continue
		}
		*(*byte)(unsafe.Pointer(tmpPtr + uintptr(j))) = 0xC0
		j++
		*(*byte)(unsafe.Pointer(tmpPtr + uintptr(j))) = 0x80
	}
	*(*byte)(unsafe.Pointer(tmpPtr + uintptr(j))) = 0
	r := C.NewStringUTF(env, (*C.char)(tmp))
	if C.ExceptionOccurred(env) != nil {
		panic("NewStringUTF OutOfMemoryError exception")
	}
	return r, nil
}

// JStringToV23SString creates a v23_syncbase_String from a jstring. This is the
// revert of the V23SStringToJString.
func JStringToV23SString(env *C.JNIEnv, s C.jstring) (C.v23_syncbase_String, error) {
	r := C.v23_syncbase_String{}
	r.n = C.int(C.GetStringUTFLength(env, s))
	r.p = (*C.char)(C.malloc(C.size_t(r.n)))
	p := uintptr(unsafe.Pointer(r.p))
	// Note that GetStringUTFLength does not include a trailing zero.
	n := int(C.GetStringUTFLength(env, s))
	C.GetStringUTFRegion(env, s, 0, C.GetStringLength(env, s), r.p)
	// We don't have to check for exceptions because GetStringUTFRegion can
	// only throw StringIndexOutOfBoundsException and we know the requested
	// amount of characters is valid.
	j := 0
	for i := 0; i < n; i, j = i+1, j+1 {
		if i+1 < n && *(*byte)(unsafe.Pointer(p + uintptr(i))) == 0xC0 && *(*byte)(unsafe.Pointer(p + uintptr(i+1))) == 0x80 {
			*(*byte)(unsafe.Pointer(p + uintptr(j))) = 0
			i++
			continue
		}

		if j == i {
			continue
		}

		*(*byte)(unsafe.Pointer(p + uintptr(j))) = *(*byte)(unsafe.Pointer(p + uintptr(i)))
	}
	r.p = (*C.char)(C.realloc(unsafe.Pointer(r.p), (C.size_t)(j)))
	return r, nil
}
