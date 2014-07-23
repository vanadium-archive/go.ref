// +build android

package jni

import (
	"fmt"
	"runtime"

	"veyron/runtimes/google/jni/util"
	isecurity "veyron/runtimes/google/security/jni"
	"veyron2/ipc"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jobject CallDispatcherLookupMethod(JNIEnv* env, jobject obj, jmethodID id, jstring str) {
//   return (*env)->CallObjectMethod(env, obj, id, str);
// }
// static jobject CallDispatcherGetObjectMethod(JNIEnv* env, jobject obj, jmethodID id) {
//   return (*env)->CallObjectMethod(env, obj, id);
// }
// static jobject CallDispatcherGetAuthorizerMethod(JNIEnv* env, jobject obj, jmethodID id) {
//   return (*env)->CallObjectMethod(env, obj, id);
// }
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
		var env *C.JNIEnv
		C.AttachCurrentThread(d.jVM, &env, nil)
		defer C.DetachCurrentThread(d.jVM)
		C.DeleteGlobalRef(env, d.jDispatcher)
	})

	return d, nil
}

type dispatcher struct {
	jVM         *C.JavaVM
	jDispatcher C.jobject
}

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	// Get Java environment.
	var env *C.JNIEnv
	C.AttachCurrentThread(d.jVM, &env, nil)
	defer C.DetachCurrentThread(d.jVM)

	// Call Java dispatcher's lookup() method.
	serviceObjectWithAuthorizerSign := util.ClassSign("com.veyron2.ipc.ServiceObjectWithAuthorizer")
	lid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, d.jDispatcher), "lookup", util.FuncSign([]util.Sign{util.StringSign}, serviceObjectWithAuthorizerSign)))
	jServiceObjectWithAuthorizer := C.CallDispatcherLookupMethod(env, d.jDispatcher, lid, C.jstring(util.JStringPtr(env, suffix)))
	if err := util.JExceptionMsg(env); err != nil {
		return nil, nil, fmt.Errorf("error invoking Java dispatcher's lookup() method: %v", err)
	}
	if jServiceObjectWithAuthorizer == nil {
		// Lookup returned null, which means that the dispatcher isn't handling the object -
		// this is not an error.
		return nil, nil, nil
	}

	// Extract the Java service object and Authorizer.
	oid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, jServiceObjectWithAuthorizer), "getServiceObject", util.FuncSign(nil, util.ObjectSign)))
	jObj := C.CallDispatcherGetObjectMethod(env, jServiceObjectWithAuthorizer, oid)
	if jObj == nil {
		return nil, nil, fmt.Errorf("null service object returned by Java's ServiceObjectWithAuthorizer")
	}
	authSign := util.ClassSign("com.veyron2.security.Authorizer")
	aid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, jServiceObjectWithAuthorizer), "getAuthorizer", util.FuncSign(nil, authSign)))
	jAuth := C.CallDispatcherGetAuthorizerMethod(env, jServiceObjectWithAuthorizer, aid)

	// Create Go Invoker and Authorizer.
	i, err := newInvoker(env, d.jVM, jObj)
	if err != nil {
		return nil, nil, err
	}
	var a security.Authorizer
	if jAuth != nil {
		a = isecurity.NewAuthorizer(env, jAuth)
	}
	return i, a, nil
}
