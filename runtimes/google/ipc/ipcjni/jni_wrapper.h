// +build android

#ifndef VEYRON_RUNTIMES_GOOGLE_IPC_JNI_JNI_WRAPPERS_H
#define VEYRON_RUNTIMES_GOOGLE_IPC_JNI_JNI_WRAPPERS_H

#include <jni.h>

// Go cannot invoke function pointers (which is what JNI requires for all
// construct of type (*env)->GetMethodID(...)), so we have to create C stubs
// below to do the job.

// Returns the class of an object.
jclass GetObjectClass(JNIEnv* env, jobject obj);

// Searches the directories and zip files specified by the CLASSPATH environment
// variable for the class with the specified name.
jclass FindClass(JNIEnv* env, const char* name);

// Returns the method ID for an instance (nonstatic) method of a class or
// interface.
jmethodID GetMethodID(JNIEnv* env, jclass class, const char* name, const char* args);

// Returns the field ID for an instance (nonstatic) field of a class.
jfieldID GetFieldID(JNIEnv* env, jclass class, const char* name, const char* sig);

// Returns the value of an instance (nonstatic) boolean field of the provided
// object.
jboolean GetBooleanField(JNIEnv* env, jobject obj, jfieldID fieldID);

// Returns the value of an instance (nonstatic) Object field of the provided
// object.
jobject GetObjectField(JNIEnv* env, jobject obj, jfieldID fieldID);

// Constructs a new array holding objects in class jclass.
jobjectArray NewObjectArray(JNIEnv* env, jsize len, jclass class, jobject obj);

// Returns the number of elements in the array.
jsize GetArrayLength(JNIEnv* env, jarray array);

// Returns an element of an Object array.
jobject GetObjectArrayElement(JNIEnv* env, jobjectArray array, jsize index);

// Sets an element of an Object array.
void SetObjectArrayElement(JNIEnv* env, jobjectArray array, jsize index, jobject obj);

// Returns a pointer to an array of bytes representing the string in modified
// UTF-8 encoding.
const char* GetStringUTFChars(JNIEnv* env, jstring str, jboolean* isCopy);

// Informs the VM that the native code no longer needs access to the byte array.
void ReleaseStringUTFChars(JNIEnv* env, jstring str, const char* utf);

// Constructs a new java.lang.String object from an array of characters in
// modified UTF-8 encoding.
jstring NewStringUTF(JNIEnv* env, const char* str);

// Causes a java.lang.Throwable object to be thrown.
jint Throw(JNIEnv* env, jthrowable obj);

// Constructs an exception object from the specified class with the given
// message and causes that exception to be thrown.
jint ThrowNew(JNIEnv* env, jclass class, const char* msg);

// Creates a new global reference to the object referred to by the obj argument.
jobject NewGlobalRef(JNIEnv* env, jobject obj);

// Deletes the global reference pointed to by globalRef.
void DeleteGlobalRef(JNIEnv* env, jobject globalRef);

// Returns the Java VM interface (used in the Invocation API) associated with
// the current thread.
jint GetJavaVM(JNIEnv* env, JavaVM** vm);

// Determines if an exception is being thrown.
jthrowable ExceptionOccurred(JNIEnv* env);

// Clears any exception that is currently being thrown.
void ExceptionClear(JNIEnv* env);

// Attaches the current thread to a Java VM.
jint AttachCurrentThread(JavaVM* jvm, JNIEnv** env, void* args);

// Detaches the current thread from a Java VM.
jint DetachCurrentThread(JavaVM* jvm);

#endif /* VEYRON_RUNTIMES_GOOGLE_IPC_JNI_JNI_WRAPPERS_H */