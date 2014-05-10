// +build android

#include "jni_wrapper.h"

jclass GetObjectClass(JNIEnv* env, jobject obj) {
  return (*env)->GetObjectClass(env, obj);
}

jclass FindClass(JNIEnv* env, const char* name) {
  return (*env)->FindClass(env, name);
}

jmethodID GetMethodID(JNIEnv* env, jclass class, const char* name, const char* args) {
  return (*env)->GetMethodID(env, class, name, args);
}

jfieldID GetFieldID(JNIEnv* env, jclass class, const char* name, const char* sig) {
  return (*env)->GetFieldID(env, class, name, sig);
}

jboolean GetBooleanField(JNIEnv* env, jobject obj, jfieldID fieldID) {
  return (*env)->GetBooleanField(env, obj, fieldID);
}

jobject GetObjectField(JNIEnv* env, jobject obj, jfieldID fieldID) {
  return (*env)->GetObjectField(env, obj, fieldID);
}

jobjectArray NewObjectArray(JNIEnv* env, jsize len, jclass class, jobject obj) {
  return (*env)->NewObjectArray(env, len, class, obj);
}

jsize GetArrayLength(JNIEnv* env, jarray array) {
  return (*env)->GetArrayLength(env, array);
}

jobject GetObjectArrayElement(JNIEnv* env, jobjectArray array, jsize index) {
  return (*env)->GetObjectArrayElement(env, array, index);
}

void SetObjectArrayElement(JNIEnv* env, jobjectArray array, jsize index, jobject obj) {
  (*env)->SetObjectArrayElement(env, array, index, obj);
}

const char* GetStringUTFChars(JNIEnv* env, jstring str, jboolean* isCopy) {
  return (*env)->GetStringUTFChars(env, str, isCopy);
}

void ReleaseStringUTFChars(JNIEnv* env, jstring str, const char* utf) {
  (*env)->ReleaseStringUTFChars(env, str, utf);
}

jstring NewStringUTF(JNIEnv* env, const char* str) {
  return (*env)->NewStringUTF(env, str);
}

jint Throw(JNIEnv* env, jthrowable obj) {
  return (*env)->Throw(env, obj);
}

jint ThrowNew(JNIEnv* env, jclass class, const char* msg) {
  return (*env)->ThrowNew(env, class, msg);
}

jobject NewGlobalRef(JNIEnv* env, jobject obj) {
  return (*env)->NewGlobalRef(env, obj);
}

void DeleteGlobalRef(JNIEnv* env, jobject globalRef) {
  return (*env)->DeleteGlobalRef(env, globalRef);
}

jint GetJavaVM(JNIEnv* env, JavaVM** vm) {
  return (*env)->GetJavaVM(env, vm);
}

jthrowable ExceptionOccurred(JNIEnv* env) {
  return (*env)->ExceptionOccurred(env);
}

void ExceptionClear(JNIEnv* env) {
  return (*env)->ExceptionClear(env);
}

jint AttachCurrentThread(JavaVM* jvm, JNIEnv** env, void* args) {
  return (*jvm)->AttachCurrentThread(jvm, env, args);
}

jint DetachCurrentThread(JavaVM* jvm) {
  return (*jvm)->DetachCurrentThread(jvm);
}