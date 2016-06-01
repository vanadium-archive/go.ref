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

func jFindClass(env *C.JNIEnv, name string) (C.jclass, error) {
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

func jGetMethodID(env *C.JNIEnv, cls C.jclass, name, sig string) C.jmethodID {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	cSig := C.CString(sig)
	defer C.free(unsafe.Pointer(cSig))

	method := C.GetMethodID(env, cls, cName, cSig)
	if method == nil {
		panic(fmt.Sprintf("couldn't get method %q with signature %s", name, sig))
	}

	// Note: the validity of the method is bounded by the lifetime of the
	// ClassLoader that did the loading of the class.
	return method
}

func jGetFieldID(env *C.JNIEnv, cls C.jclass, name, sig string) C.jfieldID {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	cSig := C.CString(sig)
	defer C.free(unsafe.Pointer(cSig))

	field := C.GetFieldID(env, cls, cName, cSig)
	if field == nil {
		panic(fmt.Sprintf("couldn't get field %q with signature ", name, sig))
	}

	// Note: the validity of the field is bounded by the lifetime of the
	// ClassLoader that did the loading of the class.
	return field
}
