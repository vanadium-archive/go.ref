// +build android

package main

//TODO(bprosnitz) Move android code to a separate package so that we can make dependencies work

import "syscall"

// #cgo LDFLAGS: -ljniwrapper
// #include "jni_wrapper.h"
// #include <stdlib.h>
import "C"

//export Java_com_veyron_runtimes_google_android_RedirectStderr_nativeStart
func Java_com_veyron_runtimes_google_android_RedirectStderr_nativeStart(env *C.JNIEnv, jRuntime C.jclass, fileno C.jint) {
	syscall.Dup2(int(fileno), syscall.Stderr)
}
