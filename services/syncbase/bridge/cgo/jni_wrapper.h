// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build java android cgo

// All JNI functions are function pointers of a JNIEnv variable. Go language
// cannot call function pointers so we need use some wrapper functions to do
// that.

#include "jni.h"

void ExceptionClear(JNIEnv *env);
jthrowable ExceptionOccurred(JNIEnv* env);
jclass FindClass(JNIEnv* env, const char* name);
jint GetEnv(JavaVM* jvm, JNIEnv** env, jint version);
jfieldID GetFieldID(JNIEnv *env, jclass cls, const char *name, const char *sig);
jmethodID GetMethodID(JNIEnv* env, jclass cls, const char* name, const char* sig);
jsize GetStringLength(JNIEnv *env, jstring string);
jsize GetStringUTFLength(JNIEnv *env, jstring string);
void GetStringUTFRegion(JNIEnv *env, jstring str, jsize start, jsize len, char *buf);
jobject NewGlobalRef(JNIEnv* env, jobject obj);
jobject NewObjectA(JNIEnv *env, jclass cls, jmethodID methodID, jvalue *args);
jstring NewStringUTF(JNIEnv *env, const char *bytes);
void SetLongField(JNIEnv *env, jobject obj, jfieldID fieldID, jlong value);
void SetObjectField(JNIEnv *env, jobject obj, jfieldID fieldID, jobject value);
jint Throw(JNIEnv *env, jthrowable obj);
jint ThrowNew(JNIEnv *env, jclass cls, const char *message);