// +build android

package jni

import (
	"runtime"

	"veyron/runtimes/google/jni/util"
	inaming "veyron/runtimes/google/naming"
	"veyron2/naming"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
//
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jstring CallContextStringMethod(JNIEnv* env, jobject obj, jmethodID id) {
// 	return (jstring)(*env)->CallObjectMethod(env, obj, id);
// }
// static jint CallContextIntMethod(JNIEnv* env, jobject obj, jmethodID id) {
// 	return (*env)->CallIntMethod(env, obj, id);
// }
// static jobject CallContextPublicIDMethod(JNIEnv* env, jobject obj, jmethodID id) {
// 	return (*env)->CallObjectMethod(env, obj, id);
// }
// static jobject CallContextLabelMethod(JNIEnv* env, jobject obj, jmethodID id) {
// 	return (*env)->CallObjectMethod(env, obj, id);
// }
import "C"

func newContext(env *C.JNIEnv, jContext C.jobject) *context {
	// We cannot cache Java environments as they are only valid in the current
	// thread.  We can, however, cache the Java VM and obtain an environment
	// from it in whatever thread happens to be running at the time.
	var jVM *C.JavaVM
	if status := C.GetJavaVM(env, &jVM); status != 0 {
		panic("couldn't get Java VM from the (Java) environment")
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
	return c
}

type context struct {
	jVM      *C.JavaVM
	jContext C.jobject
}

func (c *context) Method() string {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "method", util.FuncSign(nil, util.StringSign)))
	return util.GoString(env, C.CallContextStringMethod(env, c.jContext, mid))
}

func (c *context) Name() string {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "name", util.FuncSign(nil, util.StringSign)))
	return util.GoString(env, C.CallContextStringMethod(env, c.jContext, mid))
}

func (c *context) Suffix() string {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "suffix", util.FuncSign(nil, util.StringSign)))
	return util.GoString(env, C.CallContextStringMethod(env, c.jContext, mid))
}

func (c *context) Label() security.Label {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	labelSign := util.ClassSign("com.veyron2.security.Label")
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "label", util.FuncSign(nil, labelSign)))
	jLabel := C.CallContextLabelMethod(env, c.jContext, mid)
	return security.Label(util.JIntField(env, jLabel, "value"))
}

func (c *context) CaveatDischarges() security.CaveatDischargeMap {
	// TODO(spetrovic): implement this method.
	return nil
}

func (c *context) LocalID() security.PublicID {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	publicIDSign := util.ClassSign("com.veyron2.security.PublicID")
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "localID", util.FuncSign(nil, publicIDSign)))
	jID := C.CallContextPublicIDMethod(env, c.jContext, mid)
	return newPublicID(env, jID)
}

func (c *context) RemoteID() security.PublicID {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	publicIDSign := util.ClassSign("com.veyron2.security.PublicID")
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "remoteID", util.FuncSign(nil, publicIDSign)))
	jID := C.CallContextPublicIDMethod(env, c.jContext, mid)
	return newPublicID(env, jID)
}

func (c *context) LocalEndpoint() naming.Endpoint {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "localEndpoint", util.FuncSign(nil, util.StringSign)))
	// TODO(spetrovic): create a Java Endpoint interface.
	epStr := util.GoString(env, C.CallContextStringMethod(env, c.jContext, mid))
	ep, err := inaming.NewEndpoint(epStr)
	if err != nil {
		panic("Couldn't parse endpoint string: " + epStr)
	}
	return ep
}

func (c *context) RemoteEndpoint() naming.Endpoint {
	var env *C.JNIEnv
	C.AttachCurrentThread(c.jVM, &env, nil)
	defer C.DetachCurrentThread(c.jVM)
	mid := C.jmethodID(util.JMethodIDPtrOrDie(env, C.GetObjectClass(env, c.jContext), "remoteEndpoint", util.FuncSign(nil, util.StringSign)))
	// TODO(spetrovic): create a Java Endpoint interface.
	epStr := util.GoString(env, C.CallContextStringMethod(env, c.jContext, mid))
	ep, err := inaming.NewEndpoint(epStr)
	if err != nil {
		panic("Couldn't parse endpoint string: " + epStr)
	}
	return ep
}
