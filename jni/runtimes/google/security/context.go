// +build android

package security

import (
	"runtime"

	"veyron/jni/runtimes/google/util"
	inaming "veyron/runtimes/google/naming"
	"veyron2/naming"
	"veyron2/security"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
import "C"

// newJavaContext constructs a new context in java based on the passed go context.
func newJavaContext(env interface{}, context security.Context) C.jobject {
	util.GoRef(&context) // Un-refed when the Java Context object is finalized.
	return C.jobject(util.NewObjectOrCatch(env, jContextImplClass, []util.Sign{util.LongSign}, &context))
}

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
		envPtr, freeFunc := util.GetEnv(c.jVM)
		env := (*C.JNIEnv)(envPtr)
		defer freeFunc()
		C.DeleteGlobalRef(env, c.jContext)
	})
	return c
}

// context is the go interface to the java implementation of security.Context
type context struct {
	jVM      *C.JavaVM
	jContext C.jobject
}

func (c *context) Method() string {
	return c.callStringMethod("method")
}

func (c *context) Name() string {
	return c.callStringMethod("name")
}

func (c *context) Suffix() string {
	return c.callStringMethod("suffix")
}

func (c *context) Label() security.Label {
	envPtr, freeFunc := util.GetEnv(c.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	labelSign := util.ClassSign("com.veyron2.security.Label")
	jLabel := C.jobject(util.CallObjectMethodOrCatch(env, c.jContext, "label", nil, labelSign))
	return security.Label(util.JIntField(env, jLabel, "value"))
}

func (c *context) CaveatDischarges() security.CaveatDischargeMap {
	// TODO(spetrovic): implement this method.
	return nil
}

func (c *context) LocalID() security.PublicID {
	envPtr, freeFunc := util.GetEnv(c.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	publicIDSign := util.ClassSign("com.veyron2.security.PublicID")
	jID := C.jobject(util.CallObjectMethodOrCatch(env, c.jContext, "localID", nil, publicIDSign))
	idPtr := util.CallLongMethodOrCatch(env, jID, "getNativePtr", nil)
	return (*(*security.PublicID)(util.Ptr(idPtr)))
}

func (c *context) RemoteID() security.PublicID {
	envPtr, freeFunc := util.GetEnv(c.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	publicIDSign := util.ClassSign("com.veyron2.security.PublicID")
	jID := C.jobject(util.CallObjectMethodOrCatch(env, c.jContext, "remoteID", nil, publicIDSign))
	idPtr := util.CallLongMethodOrCatch(env, jID, "getNativePtr", nil)
	return (*(*security.PublicID)(util.Ptr(idPtr)))
}

func (c *context) LocalEndpoint() naming.Endpoint {
	envPtr, freeFunc := util.GetEnv(c.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	// TODO(spetrovic): create a Java Endpoint interface.
	epStr := util.CallStringMethodOrCatch(env, c.jContext, "localEndpoint", nil)
	ep, err := inaming.NewEndpoint(epStr)
	if err != nil {
		panic("Couldn't parse endpoint string: " + epStr)
	}
	return ep
}

func (c *context) RemoteEndpoint() naming.Endpoint {
	envPtr, freeFunc := util.GetEnv(c.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	// TODO(spetrovic): create a Java Endpoint interface.
	epStr := util.CallStringMethodOrCatch(env, c.jContext, "remoteEndpoint", nil)
	ep, err := inaming.NewEndpoint(epStr)
	if err != nil {
		panic("Couldn't parse endpoint string: " + epStr)
	}
	return ep
}

func (c *context) callStringMethod(methodName string) string {
	envPtr, freeFunc := util.GetEnv(c.jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()
	return util.CallStringMethodOrCatch(env, c.jContext, methodName, nil)
}
