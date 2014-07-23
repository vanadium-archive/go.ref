// +build android

package jni

import (
	"runtime"
	"unsafe"

	"veyron/runtimes/google/jni/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jobject CallAuthorizerNewContextObject(JNIEnv* env, jclass class, jmethodID id, jlong goContextPtr) {
// 	return (*env)->NewObject(env, class, id, goContextPtr);
// }
// static void CallAuthorizerAuthorizeMethod(JNIEnv* env, jobject obj, jmethodID id, jobject context) {
// 	(*env)->CallObjectMethod(env, obj, id, context);
// }
import "C"

// NewAuthorizer returns a new security.Authorizer given the provided Java authorizer.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java types are passed in an empty interface
// and then cast into their package local types.
func NewAuthorizer(jEnv, jAuthPtr interface{}) security.Authorizer {
	env := (*C.JNIEnv)(unsafe.Pointer(util.PtrValue(jEnv)))
	jAuth := C.jobject(unsafe.Pointer(util.PtrValue(jAuthPtr)))
	// We cannot cache Java environments as they are only valid in the current
	// thread.  We can, however, cache the Java VM and obtain an environment
	// from it in whatever thread happens to be running at the time.
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		panic("couldn't get Java VM from the (Java) environment")
	}
	// Reference Java dispatcher; it will be de-referenced when the go
	// dispatcher created below is garbage-collected (through the finalizer
	// callback we setup below).
	jAuth = C.NewGlobalRef(env, jAuth)
	a := &authorizer{
		jVM:   jVM,
		jAuth: jAuth,
	}
	runtime.SetFinalizer(a, func(a *authorizer) {
		var env *C.JNIEnv
		C.AttachCurrentThread(a.jVM, &env, nil)
		defer C.DetachCurrentThread(a.jVM)
		C.DeleteGlobalRef(env, a.jAuth)
	})
	return a
}

type authorizer struct {
	jVM   *C.JavaVM
	jAuth C.jobject
}

func (a *authorizer) Authorize(context security.Context) error {
	var env *C.JNIEnv
	C.AttachCurrentThread(a.jVM, &env, nil)
	defer C.DetachCurrentThread(a.jVM)
	// Create a Java context.
	util.GoRef(&context) // Un-refed when the Java Context object is finalized.
	cid := C.jmethodID(util.JMethodIDPtrOrDie(env, jContextImplClass, "<init>", util.FuncSign([]util.Sign{util.LongSign}, util.VoidSign)))
	jContext := C.CallAuthorizerNewContextObject(env, jContextImplClass, cid, C.jlong(util.PtrValue(&context)))
	// Run Java Authorizer.
	contextSign := util.ClassSign("com.veyron2.security.Context")
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, a.jAuth), "authorize", util.FuncSign([]util.Sign{contextSign}, util.VoidSign)))
	C.CallAuthorizerAuthorizeMethod(env, a.jAuth, mid, jContext)
	if err := util.JExceptionMsg(env); err != nil {
		return err
	}
	return nil
}
