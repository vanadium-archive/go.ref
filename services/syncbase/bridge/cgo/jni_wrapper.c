// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build java android

#include "jni_wrapper.h"

void ExceptionClear(JNIEnv *env) {
  (*env)->ExceptionClear(env);
}

jthrowable ExceptionOccurred(JNIEnv* env) {
  return (*env)->ExceptionOccurred(env);
}

jclass FindClass(JNIEnv* env, const char* name) {
  return (*env)->FindClass(env, name);
}

jint GetEnv(JavaVM* jvm, JNIEnv** env, jint version) {
  return (*jvm)->GetEnv(jvm, (void**)env, version);
}

jfieldID GetFieldID(JNIEnv *env, jclass cls, const char *name, const char *sig) {
  return (*env)->GetFieldID(env, cls, name, sig);
}

jmethodID GetMethodID(JNIEnv* env, jclass cls, const char* name, const char* args) {
  return (*env)->GetMethodID(env, cls, name, args);
}

jsize GetStringLength(JNIEnv *env, jstring string) {
  return (*env)->GetStringLength(env, string);
}

jsize GetStringUTFLength(JNIEnv *env, jstring string) {
  return (*env)->GetStringLength(env, string);
}

void GetStringUTFRegion(JNIEnv *env, jstring str, jsize start, jsize len, char *buf) {
  (*env)->GetStringUTFRegion(env, str, start, len, buf);
}

jobject NewGlobalRef(JNIEnv* env, jobject obj) {
  return (*env)->NewGlobalRef(env, obj);
}

jobject NewObjectA(JNIEnv *env, jclass cls, jmethodID methodID, jvalue *args) {
  return (*env)->NewObjectA(env, cls, methodID, args);
}

void SetLongField(JNIEnv *env, jobject obj, jfieldID fieldID, jlong value) {
  (*env)->SetLongField(env, obj, fieldID, value);
}

jstring NewStringUTF(JNIEnv *env, const char *bytes) {
  return (*env)->NewStringUTF(env, bytes);
}

void SetObjectField(JNIEnv *env, jobject obj, jfieldID fieldID, jobject value) {
  (*env)->SetObjectField(env, obj, fieldID, value);
}

jint Throw(JNIEnv *env, jthrowable obj) {
  return (*env)->Throw(env, obj);
}

jint ThrowNew(JNIEnv *env, jclass cls, const char *message) {
  return (*env)->ThrowNew(env, cls, message);
}