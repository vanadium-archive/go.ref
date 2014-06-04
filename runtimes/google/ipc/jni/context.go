// +build android

package main

import (
	"fmt"
	"runtime"
)

// #include "jni_wrapper.h"
import "C"

func newContext(env *C.JNIEnv, jContext C.jobject) (*context, error) {
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		return nil, fmt.Errorf("couldn't get Java VM from the (Java) environment")
	}
	// Reference Java context; it will be de-referenced when the go context
	// created below is garbage-collected (through the finalizer callback we
	// setup just below).
	jContext = C.NewGlobalRef(env, jContext)
	c := &context{
		jVM:      jVM,
		jContext: jContext,
	}
	runtime.SetFinalizer(c, func(c *context) {
		var env *C.JNIEnv
		C.AttachCurrentThread(c.jVM, &env, nil)
		defer C.DetachCurrentThread(c.jVM)
		C.DeleteGlobalRef(env, c.jContext)
	})
	return c, nil
}

type context struct {
	jVM      *C.JavaVM
	jContext C.jobject
}
