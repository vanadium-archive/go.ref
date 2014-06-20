// +build android

package jni

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"veyron2/verror"
)

// #cgo LDFLAGS: -ljniwrapper
// #include <stdlib.h>
// #include "jni_wrapper.h"
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jobject CallNewVeyronExceptionObject(JNIEnv* env, jclass class, jmethodID mid, jstring msg, jstring id) {
//   return (*env)->NewObject(env, class, mid, msg, id);
// }
// static jstring CallGetMessage(JNIEnv* env, jobject obj, jmethodID id) {
//   return (jstring)(*env)->CallObjectMethod(env, obj, id);
// }
import "C"

const (
	voidSign   = "V"
	boolSign   = "Z"
	longSign   = "J"
	stringSign = "Ljava/lang/String;"
	objectSign = "Ljava/lang/Object;"
)

// refs stores references to instances of various Go types, namely instances
// that are referenced only by the Java code.  The only purpose of this store
// is to prevent Go runtime from garbage collecting those instances.
var refs = newSafeSet()

// goRef creates a new reference to the value addressed by the provided pointer.
// The value will remain referenced until it is explicitly unreferenced using
// goUnref().
func goRef(valptr interface{}) {
	if !isPointer(valptr) {
		panic("must pass pointer value to goRef")
	}
	refs.insert(valptr)
}

// goUnref removes a previously added reference to the value addressed by the
// provided pointer.  If the value hasn't been ref-ed (a bug?), this unref will
// be a no-op.
func goUnref(valptr interface{}) {
	if !isPointer(valptr) {
		panic("must pass pointer value to goUnref")
	}
	refs.delete(valptr)
}

// ptrValue returns the value of the pointer as a Java C.jlong type.
func ptrValue(ptr interface{}) C.jlong {
	v := reflect.ValueOf(ptr)
	if v.Kind() != reflect.Ptr {
		panic("must pass pointer value to ptrValue")
	}
	return C.jlong(v.Pointer())
}

// ptr returns the pointer represented by the provided (Java C.jlong) value.
func ptr(ptrValue C.jlong) unsafe.Pointer {
	return unsafe.Pointer(uintptr(ptrValue))
}

// isPointer returns true iff the provided value is a pointer.
func isPointer(val interface{}) bool {
	return reflect.ValueOf(val).Kind() == reflect.Ptr
}

// goString returns a Go string given the Java string.
func goString(env *C.JNIEnv, str C.jstring) string {
	if str == nil {
		return ""
	}
	cString := C.GetStringUTFChars(env, str, nil)
	defer C.ReleaseStringUTFChars(env, str, cString)
	return C.GoString(cString)
}

// jString returns a Java string given the Go string.
func jString(env *C.JNIEnv, str string) C.jstring {
	cString := C.CString(str)
	defer C.free(unsafe.Pointer(cString))
	return C.NewStringUTF(env, cString)
}

// jThrow throws a new Java exception of the provided type with the given message.
func jThrow(env *C.JNIEnv, class C.jclass, msg string) {
	s := C.CString(msg)
	defer C.free(unsafe.Pointer(s))
	C.ThrowNew(env, class, s)
}

// jThrowV throws a new Java VeyronException corresponding to the given error.
func jThrowV(env *C.JNIEnv, err error) {
	verr := verror.Convert(err)
	id := jMethodID(env, jVeyronExceptionClass, "<init>", fmt.Sprintf("(%s%s)%s", stringSign, stringSign, voidSign))
	obj := C.jthrowable(C.CallNewVeyronExceptionObject(env, jVeyronExceptionClass, id, jString(env, verr.Error()), jString(env, string(verr.ErrorID()))))
	C.Throw(env, obj)
}

// jExceptionMsg returns the exception message if an exception occurred, or
// nil otherwise.
func jExceptionMsg(env *C.JNIEnv) error {
	e := C.ExceptionOccurred(env)
	if e == nil { // no exception
		return nil
	}
	C.ExceptionClear(env)
	id := jMethodID(env, jThrowableClass, "getMessage", fmt.Sprintf("()%s", stringSign))
	jMsg := C.CallGetMessage(env, C.jobject(e), id)
	//return errors.New("blah")
	return errors.New(goString(env, jMsg))
}

// camelCase converts ThisString to thisString.
func camelCase(s string) string {
	if s == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

// jBoolField returns the value of the provided object's boolean field.
func jBoolField(env *C.JNIEnv, obj C.jobject, field string) bool {
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString(boolSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	return C.GetBooleanField(env, obj, fid) != C.JNI_FALSE
}

// jStringField returns the value of the provided object's String field,
// as a Go string.
func jStringField(env *C.JNIEnv, obj C.jobject, field string) string {
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString(stringSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	jStr := C.jstring(C.GetObjectField(env, obj, fid))
	return goString(env, jStr)
}

// jStringArrayField returns the value of the provided object's String[] field,
// as a slice of Go strings.
func jStringArrayField(env *C.JNIEnv, obj C.jobject, field string) []string {
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString("[" + stringSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	jStrArray := C.jobjectArray(C.GetObjectField(env, obj, fid))
	if jStrArray == nil {
		return nil
	}
	length := C.GetArrayLength(env, C.jarray(jStrArray))
	ret := make([]string, int(length))
	for i := 0; i < int(length); i++ {
		ret[i] = goString(env, C.jstring(C.GetObjectArrayElement(env, jStrArray, C.jsize(i))))
	}
	return ret
}

// jMethodID returns the Java method ID for the given method.
func jMethodID(env *C.JNIEnv, class C.jclass, name, signature string) C.jmethodID {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	cSignature := C.CString(signature)
	defer C.free(unsafe.Pointer(cSignature))
	return C.GetMethodID(env, class, cName, cSignature)
}

// jFindClassOrDie returns the global references to the Java class with the
// given pathname, or panic-s if the class cannot be found.
func jFindClassOrDie(env *C.JNIEnv, name string) C.jclass {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	class := C.FindClass(env, cName)
	if err := jExceptionMsg(env); err != nil || class == nil {
		panic(fmt.Sprintf("couldn't find class %s: %v", name, err))
	}
	return C.jclass(C.NewGlobalRef(env, C.jobject(class)))
}

// derefOrDie dereferences the provided (pointer) value, or panic-s if the value
// isn't of pointer type.
func derefOrDie(i interface{}) interface{} {
	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("want reflect.Ptr value for %v, have %v", i, v.Kind()))
	}
	return v.Elem().Interface()
}

func newSafeSet() *safeSet {
	return &safeSet{
		items: make(map[interface{}]bool),
	}
}

// safeSet is a thread-safe set.
type safeSet struct {
	lock  sync.Mutex
	items map[interface{}]bool
}

func (s *safeSet) insert(item interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.items[item] = true
}

func (s *safeSet) delete(item interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.items, item)
}
