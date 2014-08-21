// +build android

package ipc

import (
	"fmt"
	"runtime"

	isecurity "veyron/jni/runtimes/google/security"
	"veyron/jni/runtimes/google/util"
	"veyron2/ipc"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
import "C"

func newDispatcher(env *C.JNIEnv, jDispatcher C.jobject) (*dispatcher, error) {
	// We cannot cache Java environments as they are only valid in the current
	// thread.  We can, however, cache the Java VM and obtain an environment
	// from it in whatever thread happens to be running at the time.
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		return nil, fmt.Errorf("couldn't get Java VM from the (Java) environment")
	}
	// Reference Java dispatcher; it will be de-referenced when the go
	// dispatcher created below is garbage-collected (through the finalizer
	// callback we setup below).
	jDispatcher = C.NewGlobalRef(env, jDispatcher)
	d := &dispatcher{
		jVM:         jVM,
		jDispatcher: jDispatcher,
	}
	runtime.SetFinalizer(d, func(d *dispatcher) {
		envPtr, freeFunc := util.GetEnv(d.jVM)
		env := (*C.JNIEnv)(envPtr)
		defer freeFunc()
		C.DeleteGlobalRef(env, d.jDispatcher)
	})

	return d, nil
}

type dispatcher struct {
	jVM         *C.JavaVM
	jDispatcher C.jobject
}

func (d *dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	// Get Java environment.
	envPtr, freeFunc := util.GetEnv(d.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()

	// Call Java dispatcher's lookup() method.
	serviceObjectWithAuthorizerSign := util.ClassSign("com.veyron2.ipc.ServiceObjectWithAuthorizer")
	tempJObj, err := util.CallObjectMethod(env, d.jDispatcher, "lookup", []util.Sign{util.StringSign}, serviceObjectWithAuthorizerSign, suffix)
	jObj := C.jobject(tempJObj)
	if err != nil {
		return nil, nil, fmt.Errorf("error invoking Java dispatcher's lookup() method: %v", err)
	}
	if jObj == nil {
		// Lookup returned null, which means that the dispatcher isn't handling the object -
		// this is not an error.
		return nil, nil, nil
	}

	// Extract the Java service object and Authorizer.
	jServiceObj := C.jobject(util.CallObjectMethodOrCatch(env, jObj, "getServiceObject", nil, util.ObjectSign))
	if jServiceObj == nil {
		return nil, nil, fmt.Errorf("null service object returned by Java's ServiceObjectWithAuthorizer")
	}
	authSign := util.ClassSign("com.veyron2.security.Authorizer")
	jAuth := C.jobject(util.CallObjectMethodOrCatch(env, jObj, "getAuthorizer", nil, authSign))

	// Create Go Invoker and Authorizer.
	i, err := newInvoker(env, d.jVM, jServiceObj)
	if err != nil {
		return nil, nil, err
	}
	var a security.Authorizer
	if jAuth != nil {
		a = isecurity.NewAuthorizer(env, jAuth)
	}
	return i, a, nil
}
