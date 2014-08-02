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
	case reflect.Slice, reflect.Array:
		switch rv.Type().Elem().Kind() {
		case reflect.Uint8:
			bs := rv.Interface().([]byte)
			jv := JByteArrayPtr(env, bs)
			ptr = unsafe.Pointer(&jv)
		case reflect.String:
			// TODO(bprosnitz) We should handle objects by calling jValue recursively. We need a way to get the sign of the target type or treat it as an Object for non-string types.
			strs := rv.Interface().([]string)
			jv := JStringArrayPtr(env, strs)
			ptr = unsafe.Pointer(&jv)
		default:
			panic(fmt.Sprintf("support for converting slice of kind %v not yet implemented", rv.Type().Elem().Kind()))
		}
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
func NewObject(env interface{}, class interface{}, argSigns []Sign, args ...interface{}) (C.jobject, error) {
	if class == nil {
		panic("cannot call constructor of nil class")
	}
	jenv := getEnv(env)
	jclass := getClass(class)

	jcid := C.jmethodID(JMethodIDPtrOrDie(jenv, jclass, "<init>", FuncSign(argSigns, VoidSign)))

	valArray, freeFunc := jValueArray(jenv, args)
	defer freeFunc()

	ret := C.NewObjectA(jenv, jclass, jcid, valArray)
	err := JExceptionMsg(env)
	return ret, err
}

// NewObjectOrCatch is a helper method that calls NewObject and panics if a Java exception occurred.
func NewObjectOrCatch(env interface{}, class interface{}, argSigns []Sign, args ...interface{}) C.jobject {
	obj, err := NewObject(env, class, argSigns, args...)
	if err != nil {
		panic(fmt.Sprintf("exception while creating jni object: %v", err))
	}
	return obj
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
func CallObjectMethod(env interface{}, object interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) (C.jobject, error) {
	switch retSign {
	case ByteSign, CharSign, ShortSign, LongSign, FloatSign, DoubleSign, BoolSign, IntSign, VoidSign:
		panic(fmt.Sprintf("Illegal call to CallObjectMethod on method with return sign %s", retSign))
	}
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, retSign, args)
	defer freeFunc()
	ret := C.CallObjectMethodA(jenv, jobject, jmid, valArray)
	return ret, JExceptionMsg(env)
}

// CallStringMethod calls a java method over JNI that returns a string.
func CallStringMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) (string, error) {
	jstr, err := CallObjectMethod(env, object, name, argSigns, StringSign, args...)
	return GoString(env, jstr), err
}

// CallByteArrayMethod calls a java method over JNI that returns a byte array.
func CallByteArrayMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) ([]byte, error) {
	jArr, err := CallObjectMethod(env, object, name, argSigns, ArraySign(ByteSign), args...)
	if err != nil {
		return nil, err
	}
	return GoByteArray(env, jArr), nil
}

// CallObjectArrayMethod calls a java method over JNI that returns an object array.
func CallObjectArrayMethod(env interface{}, object interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) ([]C.jobject, error) {
	if retSign == "" || retSign[0] != '[' {
		panic(fmt.Sprintf("Expected object array, got: %v", retSign))
	}
	jenv := getEnv(env)
	jarr, err := CallObjectMethod(env, object, name, argSigns, retSign, args...)
	garr := make([]C.jobject, int(C.GetArrayLength(jenv, C.jarray(jarr))))
	for i, _ := range garr {
		garr[i] = C.jobject(C.GetObjectArrayElement(jenv, C.jobjectArray(jarr), C.jsize(i)))
	}
	return garr, err
}

// CallStringArrayMethod calls a java method over JNI that returns an string array.
func CallStringArrayMethod(env interface{}, object interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) ([]string, error) {
	objarr, err := CallObjectArrayMethod(env, object, name, argSigns, retSign, args...)
	strs := make([]string, len(objarr))
	for i, obj := range objarr {
		strs[i] = GoString(env, obj)
	}
	return strs, err
}

// CallBooleanMethod calls a java method over JNI that returns a boolean.
func CallBooleanMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) (bool, error) {
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, BoolSign, args)
	defer freeFunc()
	ret := C.CallBooleanMethodA(jenv, jobject, jmid, valArray) != C.JNI_OK
	return ret, JExceptionMsg(env)
}

// CallIntMethod calls a java method over JNI that returns an int.
func CallIntMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) (int, error) {
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, IntSign, args)
	defer freeFunc()
	ret := int(C.CallIntMethodA(jenv, jobject, jmid, valArray))
	return ret, JExceptionMsg(env)
}

// CallLongMethod calls a java method over JNI that returns an int64.
func CallLongMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) (int64, error) {
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, LongSign, args)
	defer freeFunc()
	ret := int64(C.CallLongMethodA(jenv, jobject, jmid, valArray))
	return ret, JExceptionMsg(env)
}

// CallVoidMethod calls a java method over JNI that "returns" void.
func CallVoidMethod(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) error {
	jenv, jobject, jmid, valArray, freeFunc := setupMethodCall(env, object, name, argSigns, VoidSign, args)
	C.CallVoidMethodA(jenv, jobject, jmid, valArray)
	freeFunc()
	return JExceptionMsg(env)
}

// CallObjectMethodOrCatch is a helper method that calls CallObjectMethod and panics if a Java exception occurred.
func CallObjectMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) C.jobject {
	obj, err := CallObjectMethod(env, object, name, argSigns, retSign, args...)
	handleException(name, err)
	return obj
}

// CallStringMethodOrCatch is a helper method that calls CallStringMethod and panics if a Java exception occurred.
func CallStringMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) string {
	str, err := CallStringMethod(env, object, name, argSigns, args...)
	handleException(name, err)
	return str
}

// CallByteArrayMethodOrCatch is a helper method that calls CallByteArrayMethod and panics if a Java exception occurred.
func CallByteArrayMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) []byte {
	arr, err := CallByteArrayMethod(env, object, name, argSigns, args...)
	handleException(name, err)
	return arr
}

// CallObjectArrayMethodOrCatch is a helper method that calls CallObjectArrayMethod and panics if a Java exception occurred.
func CallObjectArrayMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) []C.jobject {
	objs, err := CallObjectArrayMethod(env, object, name, argSigns, retSign, args...)
	handleException(name, err)
	return objs
}

// CallStringArrayMethodOrCatch is a helper method that calls CallStringArrayMethod and panics if a Java exception occurred.
func CallStringArrayMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) []string {
	strs, err := CallStringArrayMethod(env, object, name, argSigns, ArraySign(StringSign), args...)
	handleException(name, err)
	return strs
}

// CallBooleanMethodOrCatch is a helper method that calls CallBooleanMethod and panics if a Java exception occurred.
func CallBooleanMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) bool {
	b, err := CallBooleanMethod(env, object, name, argSigns, args...)
	handleException(name, err)
	return b
}

// CallIntMethodOrCatch is a helper method that calls CallIntMethod and panics if a Java exception occurred.
func CallIntMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) int {
	i, err := CallIntMethod(env, object, name, argSigns, args...)
	handleException(name, err)
	return i
}

// CallLongMethodOrCatch is a helper method that calls CallLongMethod and panics if a Java exception occurred.
func CallLongMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) int64 {
	l, err := CallLongMethod(env, object, name, argSigns, args...)
	handleException(name, err)
	return l
}

// CallVoidMethodOrCatch is a helper method that calls CallVoidMethod and panics if a Java exception occurred.
func CallVoidMethodOrCatch(env interface{}, object interface{}, name string, argSigns []Sign, args ...interface{}) {
	err := CallVoidMethod(env, object, name, argSigns, args...)
	handleException(name, err)
}

// CallStaticObjectMethod calls a static java method over JNI that returns a java object.
func CallStaticObjectMethod(env interface{}, class interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) (C.jobject, error) {
	switch retSign {
	case ByteSign, CharSign, ShortSign, LongSign, FloatSign, DoubleSign, BoolSign, IntSign, VoidSign:
		panic(fmt.Sprintf("Illegal call to CallObjectMethod on method with return sign %s", retSign))
	}
	jenv := getEnv(env)
	jclass := getClass(class)

	jmid := C.jmethodID(JMethodIDPtrOrDie(jenv, jclass, name, FuncSign(argSigns, retSign)))

	jvalArray, freeFunc := jValueArray(jenv, args)
	ret := C.CallStaticObjectMethodA(jenv, jclass, jmid, jvalArray)
	freeFunc()
	return ret, JExceptionMsg(env)
}

// CallStaticObjectMethodOrCatch is a helper method that calls CallStaticObjectMethod and panics if a Java exception occurred.
func CallStaticObjectMethodOrCatch(env interface{}, class interface{}, name string, argSigns []Sign, retSign Sign, args ...interface{}) C.jobject {
	obj, err := CallStaticObjectMethod(env, class, name, argSigns, retSign, args...)
	handleException(name, err)
	return obj
}

func handleException(name string, err error) {
	if err != nil {
		panic(fmt.Sprintf("exception while calling jni method %q: %v", name, err))
	}
}
