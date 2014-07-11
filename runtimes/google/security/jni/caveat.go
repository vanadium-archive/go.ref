// +build android

package jni

import (
	"fmt"
	"runtime"

	"veyron/runtimes/google/jni/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
//
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jobject CallCaveatNewContextObject(JNIEnv* env, jclass class, jmethodID id, jlong goContextPtr) {
// 	return (*env)->NewObject(env, class, id, goContextPtr);
// }
// static void CallCaveatValidateMethod(JNIEnv* env, jobject obj, jmethodID id, jobject context) {
// 	return (*env)->CallVoidMethod(env, obj, id, context);
// }
import "C"

func newCaveat(env *C.JNIEnv, jCaveat C.jobject) *caveat {
	// We cannot cache Java environments as they are only valid in the current
	// thread.  We can, however, cache the Java VM and obtain an environment
	// from it in whatever thread happens to be running at the time.
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		panic("couldn't get Java VM from the (Java) environment")
	}
	// Reference Java service caveat; it will be de-referenced when the go
	// service caveat created below is garbage-collected (through the finalizer
	// callback we setup just below).
	jCaveat = C.NewGlobalRef(env, jCaveat)
	c := &caveat{
		jVM:     jVM,
		jCaveat: jCaveat,
	}
	runtime.SetFinalizer(c, func(c *caveat) {
		var env *C.JNIEnv
		C.AttachCurrentThread(c.jVM, &env, nil)
		defer C.DetachCurrentThread(c.jVM)
		C.DeleteGlobalRef(env, c.jCaveat)
	})
	return c
}

type caveat struct {
	jVM     *C.JavaVM
	jCaveat C.jobject
}

func (c *caveat) Validate(context security.Context) error {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	util.GoRef(&context) // un-refed when the Java Context object is finalized.
	cid := C.jmethodID(util.JMethodIDPtr(env, jContextImplClass, "<init>", fmt.Sprintf("(%s)%s", util.LongSign, util.VoidSign)))
	jContext := C.CallCaveatNewContextObject(env, jContextClass, cid, C.jlong(util.PtrValue(&context)))
	contextSign := "Lcom/veyron2/security/Context;"
	mid := C.jmethodID(util.JMethodIDPtr(env, C.GetObjectClass(env, c.jCaveat), "validate", fmt.Sprintf("(%s)%s", contextSign, util.VoidSign)))
	C.CallCaveatValidateMethod(env, c.jCaveat, mid, jContext)
	return util.JExceptionMsg(env)
}
