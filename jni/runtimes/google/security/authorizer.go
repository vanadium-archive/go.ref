// +build android

package security

import (
	"runtime"
	"unsafe"

	"veyron/jni/runtimes/google/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
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
		envPtr, freeFunc := util.GetEnv(a.jVM)
		env := (*C.JNIEnv)(envPtr)
		defer freeFunc()
		C.DeleteGlobalRef(env, a.jAuth)
	})
	return a
}

type authorizer struct {
	jVM   *C.JavaVM
	jAuth C.jobject
}

func (a *authorizer) Authorize(context security.Context) error {
	envPtr, freeFunc := util.GetEnv(a.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	// Create a Java context.
	jContext := newJavaContext(env, context)
	// Run Java Authorizer.
	contextSign := util.ClassSign("com.veyron2.security.Context")
	return util.CallVoidMethod(env, a.jAuth, "authorize", []util.Sign{contextSign}, util.VoidSign, jContext)
}
