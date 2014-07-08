// +build android

package jni

import (
	"fmt"
	"io"
	"time"

	"veyron2"
	ctx "veyron2/context"
	"veyron2/ipc"
	"veyron2/rt"
)

// #include <stdlib.h>
// #include <jni.h>
import "C"

var (
	// Global reference for com.veyron2.ipc.VeyronException class.
	jVeyronExceptionClass C.jclass
	// Global reference for com.veyron.runtimes.google.ipc.Runtime$ServerCall class.
	jServerCallClass C.jclass
	// Global reference for com.veyron.runtimes.google.ipc.VDLInvoker class.
	jVDLInvokerClass C.jclass
	// Global reference for java.lang.Throwable class.
	jThrowableClass C.jclass
	// Global reference for java.lang.String class.
	jStringClass C.jclass
	// Global reference for java.io.EOFException class.
	jEOFExceptionClass C.jclass
)

//export Java_com_veyron_runtimes_google_Runtime_nativeGlobalInit
func Java_com_veyron_runtimes_google_Runtime_nativeGlobalInit(env *C.JNIEnv, jRuntime C.jclass) {
	// Cache global references to all Java classes used by the package.  This is
	// necessary because JNI gets access to the class loader only in the system
	// thread, so we aren't able to invoke FindClass in other threads.
	jVeyronExceptionClass = jFindClassOrDie(env, "com/veyron2/ipc/VeyronException")
	jServerCallClass = jFindClassOrDie(env, "com/veyron/runtimes/google/Runtime$ServerCall")
	jVDLInvokerClass = jFindClassOrDie(env, "com/veyron/runtimes/google/VDLInvoker")
	jThrowableClass = jFindClassOrDie(env, "java/lang/Throwable")
	jStringClass = jFindClassOrDie(env, "java/lang/String")
	jEOFExceptionClass = jFindClassOrDie(env, "java/io/EOFException")
}

//export Java_com_veyron_runtimes_google_Runtime_nativeInit
func Java_com_veyron_runtimes_google_Runtime_nativeInit(env *C.JNIEnv, jRuntime C.jobject, create C.jboolean) C.jlong {
	r := rt.Init()
	if create == C.JNI_TRUE {
		var err error
		r, err = rt.New()
		if err != nil {
			jThrowV(env, err)
		}
	}
	goRef(&r)
	return ptrValue(&r)
}

//export Java_com_veyron_runtimes_google_Runtime_nativeNewClient
func Java_com_veyron_runtimes_google_Runtime_nativeNewClient(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong, timeoutMillis C.jlong) C.jlong {
	r := (*veyron2.Runtime)(ptr(goRuntimePtr))
	if r == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go runtime with pointer: %d", int(goRuntimePtr)))
		return C.jlong(0)
	}
	options := []ipc.ClientOpt{}
	if int(timeoutMillis) > 0 {
		options = append(options, veyron2.CallTimeout(time.Duration(timeoutMillis)*time.Millisecond))
	}
	rc, err := (*r).NewClient(options...)
	if err != nil {
		jThrowV(env, err)
		return C.jlong(0)
	}
	c := newClient(rc)
	goRef(c)
	return ptrValue(c)
}

//export Java_com_veyron_runtimes_google_Runtime_nativeNewServer
func Java_com_veyron_runtimes_google_Runtime_nativeNewServer(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) C.jlong {
	r := (*veyron2.Runtime)(ptr(goRuntimePtr))
	if r == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go runtime with pointer: %d", int(goRuntimePtr)))
		return C.jlong(0)
	}
	s, err := (*r).NewServer()
	if err != nil {
		jThrowV(env, err)
		return C.jlong(0)
	}
	goRef(&s)
	return ptrValue(&s)
}

//export Java_com_veyron_runtimes_google_Runtime_nativeGetClient
func Java_com_veyron_runtimes_google_Runtime_nativeGetClient(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) C.jlong {
	r := (*veyron2.Runtime)(ptr(goRuntimePtr))
	if r == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go runtime with pointer: %d", int(goRuntimePtr)))
		return C.jlong(0)
	}
	rc := (*r).Client()
	c := newClient(rc)
	goRef(c)
	return ptrValue(c)
}

//export Java_com_veyron_runtimes_google_Runtime_nativeNewContext
func Java_com_veyron_runtimes_google_Runtime_nativeNewContext(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) C.jlong {
	r := (*veyron2.Runtime)(ptr(goRuntimePtr))
	if r == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go runtime with pointer: %d", int(goRuntimePtr)))
		return C.jlong(0)
	}
	c := (*r).NewContext()
	goRef(&c)
	return ptrValue(&c)
}

//export Java_com_veyron_runtimes_google_Runtime_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_nativeFinalize(env *C.JNIEnv, jRuntime C.jobject, goRuntimePtr C.jlong) {
	r := (*veyron2.Runtime)(ptr(goRuntimePtr))
	if r != nil {
		goUnref(r)
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeServe
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeServe(env *C.JNIEnv, jServer C.jobject, goServerPtr C.jlong, name C.jstring, dispatcher C.jobject) {
	s := (*ipc.Server)(ptr(goServerPtr))
	if s == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go server with pointer: %d", int(goServerPtr)))
		return
	}
	// Create a new Dispatcher
	d, err := newJNIDispatcher(env, dispatcher)
	if err != nil {
		jThrowV(env, err)
		return
	}
	if err := (*s).Serve(goString(env, name), d); err != nil {
		jThrowV(env, err)
		return
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeListen
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeListen(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong, protocol C.jstring, address C.jstring) C.jstring {
	s := (*ipc.Server)(ptr(goServerPtr))
	if s == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go server with pointer: %d", int(goServerPtr)))
		return nil
	}
	ep, err := (*s).Listen(goString(env, protocol), goString(env, address))
	if err != nil {
		jThrowV(env, err)
		return nil
	}
	return jString(env, ep.String())
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeStop
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeStop(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong) {
	s := (*ipc.Server)(ptr(goServerPtr))
	if s == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go server with pointer: %d", int(goServerPtr)))
		return
	}
	if err := (*s).Stop(); err != nil {
		jThrowV(env, err)
		return
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024Server_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024Server_nativeFinalize(env *C.JNIEnv, server C.jobject, goServerPtr C.jlong) {
	s := (*ipc.Server)(ptr(goServerPtr))
	if s != nil {
		goUnref(s)
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024Client_nativeStartCall
func Java_com_veyron_runtimes_google_Runtime_00024Client_nativeStartCall(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong, jContext C.jobject, name C.jstring, method C.jstring, jsonArgs C.jobjectArray, jPath C.jstring, timeoutMillis C.jlong) C.jlong {
	c := (*client)(ptr(goClientPtr))
	if c == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go client with pointer: %d", int(goClientPtr)))
		return C.jlong(0)
	}
	call, err := c.StartCall(env, jContext, goString(env, name), goString(env, method), jsonArgs, jPath, timeoutMillis)
	if err != nil {
		jThrowV(env, err)
		return C.jlong(0)
	}
	goRef(call)
	return ptrValue(call)
}

//export Java_com_veyron_runtimes_google_Runtime_00024Client_nativeClose
func Java_com_veyron_runtimes_google_Runtime_00024Client_nativeClose(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong) {
	c := (*client)(ptr(goClientPtr))
	if c == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go client with pointer: %d", int(goClientPtr)))
		return
	}
	c.Close()
}

//export Java_com_veyron_runtimes_google_Runtime_00024Client_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024Client_nativeFinalize(env *C.JNIEnv, jClient C.jobject, goClientPtr C.jlong) {
	c := (*client)(ptr(goClientPtr))
	if c != nil {
		goUnref(c)
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024Context_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024Context_nativeFinalize(env *C.JNIEnv, jClient C.jobject, goContextPtr C.jlong) {
	c := (*ctx.T)(ptr(goContextPtr))
	if c != nil {
		goUnref(c)
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeSend
func Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeSend(env *C.JNIEnv, jStream C.jobject, goStreamPtr C.jlong, jItem C.jstring) {
	s := (*stream)(ptr(goStreamPtr))
	if s == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go stream with pointer: %d", int(goStreamPtr)))
		return
	}
	s.Send(env, jItem)
}

//export Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeRecv
func Java_com_veyron_runtimes_google_Runtime_00024Stream_nativeRecv(env *C.JNIEnv, jStream C.jobject, goStreamPtr C.jlong) C.jstring {
	s := (*stream)(ptr(goStreamPtr))
	if s == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go stream with pointer: %d", int(goStreamPtr)))
		return nil
	}
	ret, err := s.Recv(env)
	if err != nil {
		if err == io.EOF {
			jThrow(env, jEOFExceptionClass, err.Error())
			return nil
		}
		jThrowV(env, err)
		return nil
	}
	return ret
}

//export Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinish
func Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinish(env *C.JNIEnv, jClientCall C.jobject, goClientCallPtr C.jlong) C.jobjectArray {
	c := (*clientCall)(ptr(goClientCallPtr))
	if c == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go client call with pointer: %d", int(goClientCallPtr)))
		return nil
	}
	ret, err := c.Finish(env)
	if err != nil {
		jThrowV(env, err)
		return nil
	}
	return ret
}

//export Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeCancel
func Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeCancel(env *C.JNIEnv, jClientCall C.jobject, goClientCallPtr C.jlong) {
	c := (*clientCall)(ptr(goClientCallPtr))
	if c == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go client call with pointer: %d", int(goClientCallPtr)))
		return
	}
	c.Cancel()
}

//export Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024ClientCall_nativeFinalize(env *C.JNIEnv, jClientCall C.jobject, goClientCallPtr C.jlong) {
	c := (*clientCall)(ptr(goClientCallPtr))
	if c != nil {
		goUnref(c)
	}
}

//export Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeDeadline
func Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeDeadline(env *C.JNIEnv, jServerCall C.jobject, goServerCallPtr C.jlong) C.jlong {
	s := (*serverCall)(ptr(goServerCallPtr))
	if s == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go server call with pointer: %d", int(goServerCallPtr)))
		return C.jlong(0)
	}
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
	s := (*serverCall)(ptr(goServerCallPtr))
	if s == nil {
		jThrowV(env, fmt.Errorf("Couldn't find Go server call with pointer: %d", int(goServerCallPtr)))
		return C.JNI_FALSE
	}
	if s.IsClosed() {
		return C.JNI_TRUE
	}
	return C.JNI_FALSE
}

//export Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeFinalize
func Java_com_veyron_runtimes_google_Runtime_00024ServerCall_nativeFinalize(env *C.JNIEnv, jServerCall C.jobject, goServerCallPtr C.jlong) {
	s := (*serverCall)(ptr(goServerCallPtr))
	if s != nil {
		goUnref(s)
	}
}
