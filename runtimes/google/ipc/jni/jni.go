// +build android

package jni

import (
	"io"
	"time"
	"unsafe"

	"veyron/runtimes/google/jni/util"
	"veyron2"
	ctx "veyron2/context"
	"veyron2/ipc"
	"veyron2/rt"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
// #include <stdlib.h>
import "C"

var (
	// Global reference for com.veyron.runtimes.google.ipc.Runtime$ServerCall class.
	jServerCallClass C.jclass
	// Global reference for com.veyron.runtimes.google.ipc.VDLInvoker class.
	jVDLInvokerClass C.jclass
	// Global reference for java.io.EOFException class.
	jEOFExceptionClass C.jclass
	// Global reference for java.lang.String class.
	jStringClass C.jclass
)

// Init initializes the JNI code with the given Java environment. This method
// must be called from the main Java thread.
// NOTE: Because CGO creates package-local types and because this method may be
// invoked from a different package, Java environment is passed in an empty
// interface and then cast into the package-local environment type.
func Init(jEnv interface{}) {
	env := (*C.JNIEnv)(unsafe.Pointer(util.PtrValue(jEnv)))
	// Cache global references to all Java classes used by the package.  This is
	// necessary because JNI gets access to the class loader only in the system
	// thread, so we aren't able to invoke FindClass in other threads.
	jServerCallClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron/runtimes/google/Runtime$ServerCall"))
	jVDLInvokerClass = C.jclass(util.JFindClassPtrOrDie(env, "com/veyron/runtimes/google/VDLInvoker"))
	jEOFExceptionClass = C.jclass(util.JFindClassPtrOrDie(env, "java/io/EOFException"))
	jStringClass = C.jclass(util.JFindClassPtrOrDie(env, "java/lang/String"))
}

//export Java_com_veyron_runtimes_google_Runtime_nativeInit
func Java_com_veyron_runtimes_google_Runtime_nativeInit(env *C.JNIEnv, jRuntime C.jobject, create C.jboolean) C.jlong {
	r := rt.Init()
	if create == C.JNI_TRUE {
		var err error
		r, err = rt.New()
		if err != nil {
			util.JThrowV(env, err)
		}
	}
	util.GoRef(&r) // Un-refed when the Java Runtime object is finalized.
	return C.jlong(util.PtrValue(&r))
}

//export Java_com_veyron_runtimes_google_Runtime_nativeNewClient
func Java_com_veyron_runtimes_google_Runtime_nativeNewClient(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong, timeoutMillis C.jlong) C.jlong {
	r := (*veyron2.Runtime)(util.Ptr(goRuntimePtr))
	options := []ipc.ClientOpt{}
	if int(timeoutMillis) > 0 {
		options = append(options, veyron2.CallTimeout(time.Duration(timeoutMillis)*time.Millisecond))
	}
	rc, err := (*r).NewClient(options...)
	if err != nil {
		util.JThrowV(env, err)
		return C.jlong(0)
	}
	c := newClient(rc)
	util.GoRef(c) // Un-refed when the Java Client object is finalized.
	return C.jlong(util.PtrValue(c))
}

//export Java_com_veyron_runtimes_google_Runtime_nativeNewServer
func Java_com_veyron_runtimes_google_Runtime_nativeNewServer(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) C.jlong {
	r := (*veyron2.Runtime)(util.Ptr(goRuntimePtr))
	s, err := (*r).NewServer()
	if err != nil {
		util.JThrowV(env, err)
		return C.jlong(0)
	}
	util.GoRef(&s) // Un-refed when the Java Server object is finalized.
	return C.jlong(util.PtrValue(&s))
}

//export Java_com_veyron_runtimes_google_Runtime_nativeGetClient
func Java_com_veyron_runtimes_google_Runtime_nativeGetClient(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) C.jlong {
	r := (*veyron2.Runtime)(util.Ptr(goRuntimePtr))
	rc := (*r).Client()
	c := newClient(rc)
	util.GoRef(c) // Un-refed when the Java Client object is finalized.
	return C.jlong(util.PtrValue(c))
}

//export Java_com_veyron_runtimes_google_Runtime_nativeNewContext
func Java_com_veyron_runtimes_google_Runtime_nativeNewContext(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) C.jlong {
	r := (*veyron2.Runtime)(util.Ptr(goRuntimePtr))
	c := (*r).NewContext()
	util.GoRef(&c) // Un-refed when the Java context object is finalized.
	return C.jlong(util.PtrValue(&c))
}

//export Java_com_veyron_runtimes_google_Runtime_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_nativeFinalize(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) {
	util.GoUnref((*veyron2.Runtime)(util.Ptr(goRuntimePtr)))
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeListen
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeListen(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong, protocol C.jstring, address C.jstring) C.jstring {
	s := (*ipc.Server)(util.Ptr(goServerPtr))
	ep, err := (*s).Listen(util.GoString(env, protocol), util.GoString(env, address))
	if err != nil {
		util.JThrowV(env, err)
		return nil
	}
	return C.jstring(util.JStringPtr(env, ep.String()))
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeServe
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeServe(env *C.JNIEnv, jServer C.jobject, goServerPtr C.jlong, name C.jstring, jDispatcher C.jobject) {
	s := (*ipc.Server)(util.Ptr(goServerPtr))
	d, err := newDispatcher(env, jDispatcher)
	if err != nil {
		util.JThrowV(env, err)
		return
	}
	if err := (*s).Serve(util.GoString(env, name), d); err != nil {
		util.JThrowV(env, err)
		return
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeGetPublishedNames
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeGetPublishedNames(env *C.JNIEnv, jServer C.jobject, goServerPtr C.jlong) C.jobjectArray {
	names, err := (*(*ipc.Server)(util.Ptr(goServerPtr))).Published()
	if err != nil {
		util.JThrowV(env, err)
		return nil
	}
	ret := C.NewObjectArray(env, C.jsize(len(names)), jStringClass, nil)
	for i, name := range names {
		C.SetObjectArrayElement(env, ret, C.jsize(i), C.jobject(util.JStringPtr(env, string(name))))
	}
	return ret
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeStop
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeStop(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong) {
	s := (*ipc.Server)(util.Ptr(goServerPtr))
	if err := (*s).Stop(); err != nil {
		util.JThrowV(env, err)
		return
	}
}

//export Java_com_veyron_runtimes_google_jni_Runtime_00024Server_nativeFinalize
func Java_com_veyron_runtimes_google_jni_Runtime_00024Server_nativeFinalize(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong) {
	util.GoUnref((*ipc.Server)(util.Ptr(goServerPtr)))
}

//export Java_com_veyron_runtimes_google_Runtime_00024Client_nativeStartCall
func Java_com_veyron_runtimes_google_Runtime_00024Client_nativeStartCall(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong, jContext C.jobject, name C.jstring, method C.jstring, jsonArgs C.jobjectArray, jPath C.jstring, timeoutMillis C.jlong) C.jlong {
	c := (*client)(util.Ptr(goClientPtr))
	call, err := c.StartCall(env, jContext, util.GoString(env, name), util.GoString(env, method), jsonArgs, jPath, timeoutMillis)
	if err != nil {
		util.JThrowV(env, err)
		return C.jlong(0)
	}
	util.GoRef(call)
	return C.jlong(util.PtrValue(call))
}

//export Java_com_veyron_runtimes_google_Runtime_00024Client_nativeClose
func Java_com_veyron_runtimes_google_Runtime_00024Client_nativeClose(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong) {
	(*client)(util.Ptr(goClientPtr)).Close()
}

//export Java_com_veyron_runtimes_google_Runtime_00024Client_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024Client_nativeFinalize(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong) {
	util.GoUnref((*client)(util.Ptr(goClientPtr)))
}

//export Java_com_veyron_runtimes_google_Runtime_00024Context_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024Context_nativeFinalize(env *C.JNIEnv, jClient C.jobject, goContextPtr C.jlong) {
	util.GoUnref((*ctx.T)(util.Ptr(goContextPtr)))
}

//export Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeSend
func Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeSend(env *C.JNIEnv, jStream C.jobject, goStreamPtr C.jlong, jItem C.jstring) {
	(*stream)(util.Ptr(goStreamPtr)).Send(env, jItem)
}

//export Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeRecv
func Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeRecv(env *C.JNIEnv, jStream C.jobject, goStreamPtr C.jlong) C.jstring {
	ret, err := (*stream)(util.Ptr(goStreamPtr)).Recv(env)
	if err != nil {
		if err == io.EOF {
			util.JThrow(env, jEOFExceptionClass, err.Error())
			return nil
		}
		util.JThrowV(env, err)
		return nil
	}
	return ret
}

//export Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinish
func Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinish(env *C.JNIEnv, jClientCall C.jobject, goClientCallPtr C.jlong) C.jobjectArray {
	ret, err := (*clientCall)(util.Ptr(goClientCallPtr)).Finish(env)
	if err != nil {
		util.JThrowV(env, err)
		return nil
	}
	return ret
}

//export Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeCancel
func Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeCancel(env *C.JNIEnv, jClientCall C.jobject, goClientCallPtr C.jlong) {
	(*clientCall)(util.Ptr(goClientCallPtr)).Cancel()
}

//export Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinalize(env *C.JNIEnv, jClientCall C.jobject, goClientCallPtr C.jlong) {
	util.GoUnref((*clientCall)(util.Ptr(goClientCallPtr)))
}

//export Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeBlessing
func Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeBlessing(env *C.JNIEnv, jServerCall C.jobject, goServerCallPtr C.jlong) C.jlong {
	id := (*serverCall)(util.Ptr(goServerCallPtr)).Blessing()
	util.GoRef(&id) // Un-refed when the Java PublicID object is finalized.
	return C.jlong(util.PtrValue(&id))
}

//export Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeDeadline
func Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeDeadline(env *C.JNIEnv, jServerCall C.jobject, goServerCallPtr C.jlong) C.jlong {
	s := (*serverCall)(util.Ptr(goServerCallPtr))
	var d time.Time
	if s == nil {
		// Error, return current time as deadline.
		d = time.Now()
	} else {
		d = s.Deadline()
	}
	return C.jlong(d.UnixNano() / 1000)
}

//export Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeClosed
func Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeClosed(env *C.JNIEnv, jServerCall C.jobject, goServerCallPtr C.jlong) C.jboolean {
	s := (*serverCall)(util.Ptr(goServerCallPtr))
	if s.IsClosed() {
		return C.JNI_TRUE
	}
	return C.JNI_FALSE
}
