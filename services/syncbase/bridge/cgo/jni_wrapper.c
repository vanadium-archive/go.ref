// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build java android

#include "jni_wrapper.h"

jint GetEnv(JavaVM* jvm, JNIEnv** env, jint version) {
    return (*jvm)->GetEnv(jvm, (void**)env, version);
}