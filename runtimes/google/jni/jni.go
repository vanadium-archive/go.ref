// +build android

package main

import (
	"flag"
	"log"
	"unsafe"

	ipc "veyron/runtimes/google/ipc/jni"
	"veyron/runtimes/google/jni/util"
	security "veyron/runtimes/google/security/jni"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
import "C"

//export JNI_OnLoad
func JNI_OnLoad(jVM *C.JavaVM, reserved unsafe.Pointer) C.jint {
	log.Println("On_Load")
	var env *C.JNIEnv
	if C.GetEnv(jVM, &env, C.JNI_VERSION_1_6) != C.JNI_OK {
		// This should never happen as OnLoad is invoked from the main Java thread.
		C.AttachCurrentThread(jVM, &env, nil)
		defer C.DetachCurrentThread(jVM)
	}
	util.Init(env)
	ipc.Init(env)
	security.Init(env)
	return C.JNI_VERSION_1_6
}

func main() {
	// Send all logging to stderr, so that the output is visible in Android.  Note that if this
	// flag is removed, the process will likely crash as android requires that all logs are written
	// into a specific directory.
	flag.Set("logtostderr", "true")
}
