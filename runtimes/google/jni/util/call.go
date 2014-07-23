// +build android

package util

import (
	"fmt"
	"reflect"
	"unsafe"
)

// #cgo LDFLAGS: -ljniwrapper
// #include <stdlib.h>
// #include "jni_wrapper.h"
//
// static jvalue* allocJValueArray(int elements) {
//   return malloc(sizeof(jvalue) * elements);
// }
//
// static setJValueArrayElement(jvalue* arr, int index, jvalue val) {
//   arr[index] = val;
// }
//
import "C"

// jValue converts a reflect value to a JNI jvalue (to use as an argument).
func jValue(env *C.JNIEnv, rv reflect.Value) C.jvalue {
	if rv.Kind() == reflect.Ptr || rv.Kind() == reflect.UnsafePointer {
		rv = reflect.ValueOf(rv.Pointer()) // Convert the pointer's address to a uintptr
	}
	var ptr unsafe.Pointer
	switch rv.Kind() {
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		jv := C.jint(rv.Int())
		ptr = unsafe.Pointer(&jv)
	case reflect.Uintptr, reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		jv := C.jlong(rv.Uint())
		ptr = unsafe.Pointer(&jv)
	case reflect.String:
		// JStringPtr allocates the strings locally, so they are freed automatically when we return to Java.
		jv := JStringPtr(env, rv.String())
		if jv == nil {
			panic("JStringPtr failed to convert string.")
		}
		ptr = unsafe.Pointer(&jv)
	default:
		panic(fmt.Sprintf("support for converting go value of kind %v not yet implemented", rv.Kind()))
	}
	if ptr == nil {
		panic(fmt.Sprintf("unexpected nil ptr when converting to java value (kind was %v)", rv.Kind()))
	}
	return *(*C.jvalue)(ptr)
}

// jValueArray converts a slice of go values to JNI jvalues (the calling args).
func jValueArray(env *C.JNIEnv, args []interface{}) (value *C.jvalue, free func()) {
	jvalueArr := C.allocJValueArray(C.int(len(args)))
	for i, item := range args {
		jval := jValue(env, reflect.ValueOf(item))
		C.setJValueArrayElement(jvalueArr, C.int(i), jval)
	}

	freeFunc := func() {
		C.free(unsafe.Pointer(jvalueArr))
	}
	return jvalueArr, freeFunc
}

// NewObject calls a java constructor through JNI, passing the specified args.
func NewObject(env interface{}, class interface{}, argSigns []Sign, args ...interface{}) C.jobject {
	if class == nil {
		panic("cannot call constructor of nil class")
	}
	jenv := getEnv(env)
	jclass := getClass(class)

	jcid := C.jmethodID(JMethodIDPtrOrDie(jenv, jclass, "<init>", FuncSign(argSigns, VoidSign)))

	valArray, freeFunc := jValueArray(jenv, args)
	defer freeFunc()

	return C.NewObjectA(jenv, jclass, jcid, valArray)
}

// setupMethodCall performs the shared preparation operations between various java method invocation functions.
func setupMethodCall(env interface{}, object interface{}, name string, argSigns []Sign, retSign Sign, args []interface{}) (jenv *C.JNIEnv, jobject C.jobject, jmid C.jmethodID, jvalArray *C.jvalue, freeFunc func()) {
	jenv = getEnv(env)
	jobject = getObject(object)
	jclass := C.GetObjectClass(jenv, jobject)

	jmid = C.jmethodID(JMethodIDPtrOrDie(jenv, jclass, name, FuncSign(argSigns, retSign)))

	jvalArray, freeFunc = jValueArray(jenv, args)
	return
}

// CallObjectMethod calls a java method over JNI that returns a java object.
func CallObjectMethod(env interface{}, object interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) C.jobject {
	switch retSign {
	case ByteSign, CharSign, ShortSign, LongSign, FloatSign, DoubleSign, BoolSign, IntSign, VoidSign:
		panic(fmt.Sprintf("Illegal call to CallObjectMethod on method with return sign %s", retSign))
	}
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, retSign, args)
	defer freeFunc()
	return C.CallObjectMethodA(jenv, jobject, jmid, valArray)
}

// CallStringMethod calls a java method over JNI that returns a string.
func CallStringMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) string {
	return GoString(env, CallObjectMethod(env, object, name, argSigns, StringSign, args))
}

// CallBooleanMethod calls a java method over JNI that returns a boolean.
func CallBooleanMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) bool {
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, BoolSign, args)
	defer freeFunc()
	return C.CallBooleanMethodA(jenv, jobject, jmid, valArray) != C.JNI_OK
}

// CallIntMethod calls a java method over JNI that returns an int.
func CallIntMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) int {
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, IntSign, args)
	defer freeFunc()
	return int(C.CallIntMethodA(jenv, jobject, jmid, valArray))
}

// CallVoidMethod calls a java method over JNI that "returns" void.
func CallVoidMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) {
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, VoidSign, args)
	C.CallVoidMethodA(jenv, jobject, jmid, valArray)
	freeFunc()
}
