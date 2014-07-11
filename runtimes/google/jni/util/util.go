// +build android

// Package util provides various JNI utilities shared across our JNI code.
package util

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
// static jstring CallGetExceptionMessage(JNIEnv* env, jobject obj, jmethodID id) {
//   return (jstring)(*env)->CallObjectMethod(env, obj, id);
// }
import "C"

const (
	// VoidSign denotes a signature of a Java void type.
	VoidSign = "V"
	// ByteSign denotes a signature of a Java byte type.
	ByteSign = "B"
	// BoolSign denotes a signature of a Java boolean type.
	BoolSign = "Z"
	// IntSign denotes a signature of a Java int type.
	IntSign = "I"
	// LongSign denotes a signature of a Java long type.
	LongSign = "J"
	// StringSign denotes a signature of a Java String type.
	StringSign = "Ljava/lang/String;"
	// ObjectSign denotes a signature of a Java Object type.
	ObjectSign = "Ljava/lang/Object;"
)

// ArraySign returns the array signature, given the underlying array type.
func ArraySign(sign string) string {
	return "[" + sign
}

var (
	// Global reference for com.veyron2.ipc.VeyronException class.
	jVeyronExceptionClass C.jclass
	// Global reference for java.lang.Throwable class.
	jThrowableClass C.jclass
	// Global reference for java.lang.String class.
	jStringClass C.jclass
)

// Init initializes the JNI code with the given Java environment.  This method
// must be invoked before any other method in this package and must be called
// from the main Java thread (e.g., On_Load()).
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func Init(jEnv interface{}) {
	env := getEnv(jEnv)
	jVeyronExceptionClass = C.jclass(JFindClassPtrOrDie(env, "com/veyron2/ipc/VeyronException"))
	jThrowableClass = C.jclass(JFindClassPtrOrDie(env, "java/lang/Throwable"))
	jStringClass = C.jclass(JFindClassPtrOrDie(env, "java/lang/String"))
}

// GoRef creates a new reference to the value addressed by the provided pointer.
// The value will remain referenced until it is explicitly unreferenced using
// goUnref().
func GoRef(valptr interface{}) {
	if !IsPointer(valptr) {
		panic("must pass pointer value to goRef")
	}
	refs.insert(valptr)
}

// GoUnref removes a previously added reference to the value addressed by the
// provided pointer.  If the value hasn't been ref-ed (a bug?), this unref will
// be a no-op.
func GoUnref(valptr interface{}) {
	if !IsPointer(valptr) {
		panic("must pass pointer value to goUnref")
	}
	refs.delete(valptr)
}

// IsPointer returns true iff the provided value is a pointer.
func IsPointer(val interface{}) bool {
	return reflect.ValueOf(val).Kind() == reflect.Ptr
}

// DerefOrDie dereferences the provided (pointer) value, or panic-s if the value
// isn't of pointer type.
func DerefOrDie(i interface{}) interface{} {
	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("want reflect.Ptr value for %v, have %v", i, v.Kind()))
	}
	return v.Elem().Interface()
}

// Ptr returns the value of the provided Java pointer (of type C.jlong) as an
// unsafe.Pointer.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func Ptr(jPtr interface{}) unsafe.Pointer {
	v := reflect.ValueOf(jPtr)
	return unsafe.Pointer(uintptr(v.Int()))
}

// PtrValue returns the value of the pointer as a uintptr.
func PtrValue(ptr interface{}) uintptr {
	v := reflect.ValueOf(ptr)
	if v.Kind() != reflect.Ptr && v.Kind() != reflect.UnsafePointer {
		panic("must pass pointer value to PtrValue")
	}
	return v.Pointer()
}

// CamelCase converts ThisString to thisString.
func CamelCase(s string) string {
	if s == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

// GoString returns a Go string given the Java string.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func GoString(jEnv, jStr interface{}) string {
	env := getEnv(jEnv)
	str := getString(jStr)
	if str == nil {
		return ""
	}
	cString := C.GetStringUTFChars(env, str, nil)
	defer C.ReleaseStringUTFChars(env, str, cString)
	return C.GoString(cString)
}

// JString returns a Java string given the Go string.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JStringPtr(jEnv interface{}, str string) unsafe.Pointer {
	env := getEnv(jEnv)
	cString := C.CString(str)
	defer C.free(unsafe.Pointer(cString))
	return unsafe.Pointer(C.NewStringUTF(env, cString))
}

// JThrow throws a new Java exception of the provided type with the given message.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JThrow(jEnv, jClass interface{}, msg string) {
	env := getEnv(jEnv)
	class := getClass(jClass)
	s := C.CString(msg)
	defer C.free(unsafe.Pointer(s))
	C.ThrowNew(env, class, s)
}

// JThrowV throws a new Java VeyronException corresponding to the given error.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JThrowV(jEnv interface{}, err error) {
	env := getEnv(jEnv)
	verr := verror.Convert(err)
	id := C.jmethodID(JMethodIDPtr(env, jVeyronExceptionClass, "<init>", fmt.Sprintf("(%s%s)%s", StringSign, StringSign, VoidSign)))
	obj := C.jthrowable(C.CallNewVeyronExceptionObject(env, jVeyronExceptionClass, id, C.jstring(JStringPtr(env, verr.Error())), C.jstring(JStringPtr(env, string(verr.ErrorID())))))
	C.Throw(env, obj)
}

// JExceptionMsg returns the exception message if an exception occurred, or
// nil otherwise.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JExceptionMsg(jEnv interface{}) error {
	env := getEnv(jEnv)
	e := C.ExceptionOccurred(env)
	if e == nil { // no exception
		return nil
	}
	C.ExceptionClear(env)
	id := C.jmethodID(JMethodIDPtr(env, jThrowableClass, "getMessage", fmt.Sprintf("()%s", StringSign)))
	jMsg := C.CallGetExceptionMessage(env, C.jobject(e), id)
	return errors.New(GoString(env, jMsg))
}

// JBoolField returns the value of the provided Java object's boolean field.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JBoolField(jEnv, jObj interface{}, field string) bool {
	env := getEnv(jEnv)
	obj := getObject(jObj)
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString(BoolSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	return C.GetBooleanField(env, obj, fid) != C.JNI_FALSE
}

// JIntField returns the value of the provided Java object's int field.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JIntField(jEnv, jObj interface{}, field string) int {
	env := getEnv(jEnv)
	obj := getObject(jObj)
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString(IntSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	return int(C.GetIntField(env, obj, fid))
}

// JStringField returns the value of the provided Java object's String field,
// as a Go string.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JStringField(jEnv, jObj interface{}, field string) string {
	env := getEnv(jEnv)
	obj := getObject(jObj)
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString(StringSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	jStr := C.jstring(C.GetObjectField(env, obj, fid))
	return GoString(env, jStr)
}

// JStringArrayField returns the value of the provided object's String[] field,
// as a slice of Go strings.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JStringArrayField(jEnv, jObj interface{}, field string) []string {
	env := getEnv(jEnv)
	obj := getObject(jObj)
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString("[" + StringSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	jStrArray := C.jobjectArray(C.GetObjectField(env, obj, fid))
	return GoStringArray(env, jStrArray)
}

// JByteArrayField returns the value of the provided object's byte[] field as a
// Go byte slice.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JByteArrayField(jEnv, jObj interface{}, field string) []byte {
	env := getEnv(jEnv)
	obj := getObject(jObj)
	cField := C.CString(field)
	defer C.free(unsafe.Pointer(cField))
	cSig := C.CString("[" + StringSign)
	defer C.free(unsafe.Pointer(cSig))
	fid := C.GetFieldID(env, C.GetObjectClass(env, obj), cField, cSig)
	arr := C.jbyteArray(C.GetObjectField(env, obj, fid))
	if arr == nil {
		return nil
	}
	return GoByteArray(env, arr)
}

// JStringArray converts the provided slice of Go strings into a Java array of strings.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JStringArrayPtr(jEnv interface{}, strs []string) unsafe.Pointer {
	env := getEnv(jEnv)
	ret := C.NewObjectArray(env, C.jsize(len(strs)), jStringClass, nil)
	for i, str := range strs {
		C.SetObjectArrayElement(env, ret, C.jsize(i), C.jobject(JStringPtr(env, str)))
	}
	return unsafe.Pointer(ret)
}

// GoStringArray converts a Java string array to a Go string array.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func GoStringArray(jEnv, jStrArray interface{}) []string {
	env := getEnv(jEnv)
	jArr := getObjectArray(jStrArray)
	if jArr == nil {
		return nil
	}
	length := C.GetArrayLength(env, C.jarray(jArr))
	ret := make([]string, int(length))
	for i := 0; i < int(length); i++ {
		ret[i] = GoString(env, C.jstring(C.GetObjectArrayElement(env, jArr, C.jsize(i))))
	}
	return ret
}

// JByteArray converts the provided Go byte slice into a Java byte array.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JByteArrayPtr(jEnv interface{}, bytes []byte) unsafe.Pointer {
	env := getEnv(jEnv)
	ret := C.NewByteArray(env, C.jsize(len(bytes)))
	C.SetByteArrayRegion(env, ret, 0, C.jsize(len(bytes)), (*C.jbyte)(unsafe.Pointer(&bytes[0])))
	return unsafe.Pointer(ret)
}

// GoByteArray converts the provided Java byte array into a Go byte slice.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func GoByteArray(jEnv, jArr interface{}) (ret []byte) {
	env := getEnv(jEnv)
	arr := getByteArray(jArr)
	length := int(C.GetArrayLength(env, C.jarray(arr)))
	ret = make([]byte, length)
	bytes := C.GetByteArrayElements(env, arr, nil)
	for i := 0; i < length; i++ {
		ret[i] = byte(*bytes)
		bytes = (*C.jbyte)(unsafe.Pointer(uintptr(unsafe.Pointer(bytes)) + unsafe.Sizeof(*bytes)))
	}
	return
}

// JMethodID returns the Java method ID for the given method.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JMethodIDPtr(jEnv, jClass interface{}, name, signature string) unsafe.Pointer {
	env := getEnv(jEnv)
	class := getClass(jClass)
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	cSignature := C.CString(signature)
	defer C.free(unsafe.Pointer(cSignature))
	return unsafe.Pointer(C.GetMethodID(env, class, cName, cSignature))
}

// JFindClassOrDie returns the global references to the Java class with the
// given pathname, or panic-s if the class cannot be found.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func JFindClassPtrOrDie(jEnv interface{}, name string) unsafe.Pointer {
	env := getEnv(jEnv)
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	class := C.FindClass(env, cName)
	if err := JExceptionMsg(env); err != nil || class == nil {
		panic(fmt.Sprintf("couldn't find class %s: %v", name, err))
	}
	return unsafe.Pointer(C.NewGlobalRef(env, C.jobject(class)))
}

// refs stores references to instances of various Go types, namely instances
// that are referenced only by the Java code.  The only purpose of this store
// is to prevent Go runtime from garbage collecting those instances.
var refs = newSafeSet()

// newSafeSet returns a new instance of a thread-safe set.
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

// Various functions that cast CGO types from various other packages into this
// package's types.
func getEnv(jEnv interface{}) *C.JNIEnv {
	return (*C.JNIEnv)(unsafe.Pointer(PtrValue(jEnv)))
}
func getByteArray(jByteArray interface{}) C.jbyteArray {
	return C.jbyteArray(unsafe.Pointer(PtrValue(jByteArray)))
}
func getObject(jObj interface{}) C.jobject {
	return C.jobject(unsafe.Pointer(PtrValue(jObj)))
}
func getClass(jClass interface{}) C.jclass {
	return C.jclass(unsafe.Pointer(PtrValue(jClass)))
}
func getString(jString interface{}) C.jstring {
	return C.jstring(unsafe.Pointer(PtrValue(jString)))
}
func getObjectArray(jArray interface{}) C.jobjectArray {
	return C.jobjectArray(unsafe.Pointer(PtrValue(jArray)))
}
