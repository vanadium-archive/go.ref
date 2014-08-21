// +build android

package main

import (
	"flag"
	"unsafe"

	"veyron/jni/runtimes/google/ipc"
	"veyron/jni/runtimes/google/security"
	"veyron/jni/runtimes/google/util"
)

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
import "C"

//export JNI_OnLoad
func JNI_OnLoad(jVM *C.JavaVM, reserved unsafe.Pointer) C.jint {
	envPtr, freeFunc := util.GetEnv(jVM)
	env := (*C.JNIEnv)(envPtr)
	defer freeFunc()

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
