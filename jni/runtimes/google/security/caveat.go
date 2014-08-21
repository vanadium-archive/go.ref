// +build android

package security

import (
	"runtime"

	"veyron/jni/runtimes/google/util"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
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
		envPtr, freeFunc := util.GetEnv(c.jVM)
		env := (*C.JNIEnv)(envPtr)
		defer freeFunc()
		C.DeleteGlobalRef(env, c.jCaveat)
	})
	return c
}

type caveat struct {
	jVM     *C.JavaVM
	jCaveat C.jobject
}

func (c *caveat) Validate(context security.Context) error {
	envPtr, freeFunc := util.GetEnv(c.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	jContext := newJavaContext(env, context)
	contextSign := util.ClassSign("com.veyron2.security.Context")
	return util.CallVoidMethod(env, c.jCaveat, "validate", []util.Sign{contextSign}, jContext)
}
