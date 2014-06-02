// +build android

package main

import (
	"flag"
	"fmt"
	"unsafe"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/verror"
)

// #include <stdlib.h>
// #include "jni_wrapper.h"
import "C"

var (
	// Global reference for com.veyron2.ipc.VeyronException class.
	jVeyronExceptionClass C.jclass
	// Global reference for com.veyron.runtimes.google.ipc.IDLInvoker class.
	jIDLInvokerClass C.jclass
	// Global reference for java.lang.Throwable class.
	jThrowableClass C.jclass
	// Global reference for java.lang.String class.
	jStringClass C.jclass
)

// refs stores references to instances of various Go types, namely instances
// that are referenced only by the Java code.  The only purpose of this store
// is to prevent Go runtime from garbage collecting those instances.
var refs = newSafeSet()

// serverPtr returns the pointer to the provided server instance, as a Java's
// C.jlong type.
func serverPtr(s ipc.Server) C.jlong {
	return C.jlong(uintptr(unsafe.Pointer(&s)))
}

// getServer returns the server referenced by the provided pointer, or nil if
// the pointer is 0.
func getServer(env *C.JNIEnv, ptr C.jlong) ipc.Server {
	if ptr == C.jlong(0) {
		jThrow(env, "Go server pointer is nil")
		return nil
	}
	return *(*ipc.Server)(unsafe.Pointer(uintptr(ptr)))
}

// serverPtr returns the pointer to the provided client instance, as a Java's
// C.jlong type.
func clientPtr(c *client) C.jlong {
	return C.jlong(uintptr(unsafe.Pointer(c)))
}

// getClient returns the client referenced by the provided pointer, or nil if
// the pointer is 0.
func getClient(env *C.JNIEnv, ptr C.jlong) *client {
	if ptr == C.jlong(0) {
		jThrow(env, "Go client pointer is nil")
		return nil
	}
	return (*client)(unsafe.Pointer(uintptr(ptr)))
}

// serverPtr returns the pointer to the provided clientCall instance, as a
// Java's C.jlong type.
func clientCallPtr(c *clientCall) C.jlong {
	return C.jlong(uintptr(unsafe.Pointer(c)))
}

// getCall returns the clientCall referenced by the provided pointer,
// or nil if the pointer is 0.
func getCall(env *C.JNIEnv, ptr C.jlong) *clientCall {
	if ptr == C.jlong(0) {
		jThrow(env, "Go client call pointer is nil")
		return nil
	}
	return (*clientCall)(unsafe.Pointer(uintptr(ptr)))
}

//export JNI_OnLoad
func JNI_OnLoad(jVM *C.JavaVM, reserved unsafe.Pointer) C.jint {
	return C.JNI_VERSION_1_6
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_nativeInit
func Java_com_veyron_runtimes_google_ipc_Runtime_nativeInit(env *C.JNIEnv, jRuntime C.jclass) {
	// Cache global references to all Java classes used by the package.  This is
	// necessary because JNI gets access to the class loader only in the system
	// thread, so we aren't able to invoke FindClass in other threads.
	jVeyronExceptionClass = jFindClassOrDie(env, "com/veyron2/ipc/VeyronException")
	jIDLInvokerClass = jFindClassOrDie(env, "com/veyron/runtimes/google/ipc/IDLInvoker")
	jThrowableClass = jFindClassOrDie(env, "java/lang/Throwable")
	jStringClass = jFindClassOrDie(env, "java/lang/String")
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativeInit
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativeInit(env *C.JNIEnv, jServer C.jobject) C.jlong {
	s, err := rt.R().NewServer()
	if err != nil {
		jThrow(env, fmt.Sprintf("Couldn't get new server from go runtime: %v", err))
		return C.jlong(0)
	}

	// Ref.
	refs.insert(s)
	return serverPtr(s)
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativeRegister
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativeRegister(env *C.JNIEnv, jServer C.jobject, goServerPtr C.jlong, prefix C.jstring, dispatcher C.jobject) {
	s := getServer(env, goServerPtr)
	if s == nil {
		jThrow(env, fmt.Sprintf("Couldn't find Go server with pointer: %d", int(goServerPtr)))
		return
	}
	// Create a new Dispatcher
	d, err := newJNIDispatcher(env, dispatcher)
	if err != nil {
		jThrow(env, err.Error())
		return
	}
	s.Register(goString(env, prefix), d)
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativeListen
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativeListen(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong, protocol C.jstring, address C.jstring) C.jstring {
	s := getServer(env, goServerPtr)
	if s == nil {
		jThrow(env, fmt.Sprintf("Couldn't find Go server with pointer: %d", int(goServerPtr)))
		return nil
	}
	ep, err := s.Listen(goString(env, protocol), goString(env, address))
	if err != nil {
		jThrow(env, err.Error())
		return nil
	}
	return jString(env, ep.String())
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativePublish
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Server_nativePublish(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong, name C.jstring) {
	s := getServer(env, goServerPtr)
	if s == nil {
		jThrow(env, fmt.Sprintf("Couldn't find Go server with pointer: %d", int(goServerPtr)))
		return
	}
	if err := s.Publish(goString(env, name)); err != nil {
		jThrow(env, err.Error())
		return
	}
}

//export Java_com_veyron_runtimes_google_ipc_jni_Runtime_00024Server_nativeStop
func Java_com_veyron_runtimes_google_ipc_jni_Runtime_00024Server_nativeStop(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong) {
	s := getServer(env, goServerPtr)
	if s == nil {
		jThrow(env, fmt.Sprintf("Couldn't find Go server with pointer: %d", int(goServerPtr)))
		return
	}
	if err := s.Stop(); err != nil {
		jThrow(env, err.Error())
		return
	}
}

//export Java_com_veyron_runtimes_google_ipc_jni_Runtime_00024Server_nativeFinalize
func Java_com_veyron_runtimes_google_ipc_jni_Runtime_00024Server_nativeFinalize(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong) {
	s := getServer(env, goServerPtr)
	if s != nil {
		// Unref.
		refs.delete(s)
	}
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeInit
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeInit(env *C.JNIEnv, jClient C.jobject) C.jlong {
	c, err := newClient()
	if err != nil {
		jThrow(env, fmt.Sprintf("Couldn't get new client from go runtime: %v", err))
		return C.jlong(0)
	}
	// Ref.
	refs.insert(c)
	return clientPtr(c)
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeStartCall
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeStartCall(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong, name C.jstring, method C.jstring, jsonArgs C.jobjectArray, jPath C.jstring, timeoutMillis C.jlong) C.jlong {
	c := getClient(env, goClientPtr)
	if c == nil {
		jThrow(env, fmt.Sprintf("Couldn't find Go client with pointer: %d", int(goClientPtr)))
		return C.jlong(0)
	}
	call, err := c.StartCall(env, goString(env, name), goString(env, method), jsonArgs, jPath, timeoutMillis)
	if err != nil {
		jThrow(env, fmt.Sprintf("Couldn't start Go call: %v", err))
		return C.jlong(0)
	}
	return clientCallPtr(call)
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeClose
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeClose(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong) {
	c := getClient(env, goClientPtr)
	if c != nil {
		c.Close()
	}
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeFinalize
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Client_nativeFinalize(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong) {
	c := getClient(env, goClientPtr)
	if c != nil {
		// Unref.
		refs.delete(c)
	}
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Call_nativeFinish
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Call_nativeFinish(env *C.JNIEnv, jClient C.jobject, goCallPtr C.jlong) C.jobjectArray {
	c := getCall(env, goCallPtr)
	if c == nil {
		jThrow(env, fmt.Sprintf("Couldn't find Go client with pointer: %d", int(goCallPtr)))
		return nil
	}
	ret, err := c.Finish(env)
	if err != nil {
		// Could be an application error, so we throw it with jThrowV.
		jThrowV(env, verror.Convert(err))
		return nil
	}
	// Unref.
	refs.delete(c)
	return ret
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Call_nativeCancel
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Call_nativeCancel(env *C.JNIEnv, jClient C.jobject, goCallPtr C.jlong) {
	c := getCall(env, goCallPtr)
	if c != nil {
		c.Cancel()
	}
}

//export Java_com_veyron_runtimes_google_ipc_Runtime_00024Call_nativeFinalize
func Java_com_veyron_runtimes_google_ipc_Runtime_00024Call_nativeFinalize(env *C.JNIEnv, jClient C.jobject, goCallPtr C.jlong) {
	c := getCall(env, goCallPtr)
	if c != nil {
		refs.delete(c)
	}
}

func main() {
	// Send all logging to stderr, so that the output is visible in Android.  Note that if this
	// flag is removed, the process will likely crash as android requires that all logs are written
	// into a specific directory.
	flag.Set("logtostderr", "true")
	rt.Init()
}
