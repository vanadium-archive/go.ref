// +build android

package jni

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/verror"
)

// #cgo LDFLAGS: -llog -ljniwrapper
// #include <stdlib.h>
// #include <android/log.h>
// #include "jni_wrapper.h"
//
// // CGO doesn't support variadic functions so we have to hard-code these
// // functions to match the invoking code. Ugh!
// static jobject CallInvokeMethod(JNIEnv* env, jobject obj, jmethodID id, jstring method, jobject call, jobjectArray inArgs) {
//   return (*env)->CallObjectMethod(env, obj, id, method, call, inArgs);
// }
// static jobject CallNewInvokerObject(JNIEnv* env, jclass class, jmethodID id, jobject obj) {
//   return (*env)->NewObject(env, class, id, obj);
// }
// static jobject CallGetInterfacePath(JNIEnv* env, jobject obj, jmethodID id) {
//   return (*env)->CallObjectMethod(env, obj, id);
// }
// static jobject CallNewServerCallObject(JNIEnv* env, jclass class, jmethodID id, jlong ref) {
//   return (*env)->NewObject(env, class, id, ref);
// }
import "C"

func newJNIInvoker(env *C.JNIEnv, jVM *C.JavaVM, jObj C.jobject) (ipc.Invoker, error) {
	// Create a new Java VDLInvoker object.
	cid := jMethodID(env, jVDLInvokerClass, "<init>", fmt.Sprintf("(%s)%s", objectSign, voidSign))
	jInvoker := C.CallNewInvokerObject(env, jVDLInvokerClass, cid, jObj)
	if err := jExceptionMsg(env); err != nil {
		return nil, fmt.Errorf("error creating Java VDLInvoker object: %v", err)
	}
	// Fetch the argGetter for the object.
	pid := jMethodID(env, jVDLInvokerClass, "getInterfacePath", fmt.Sprintf("()%s", stringSign))
	jPath := C.jstring(C.CallGetInterfacePath(env, jInvoker, pid))
	getter := newArgGetter(strings.Join(strings.Split(goString(env, jPath), ".")[1:], "/"))
	if getter == nil {
		return nil, fmt.Errorf("couldn't find VDL interface corresponding to path %q", goString(env, jPath))
	}
	// Reference Java invoker; it will be de-referenced when the go invoker
	// created below is garbage-collected (through the finalizer callback we
	// setup just below).
	jInvoker = C.NewGlobalRef(env, jInvoker)
	i := &jniInvoker{
		jVM:       jVM,
		jInvoker:  jInvoker,
		argGetter: getter,
	}
	runtime.SetFinalizer(i, func(i *jniInvoker) {
		var env *C.JNIEnv
		C.AttachCurrentThread(i.jVM, &env, nil)
		defer C.DetachCurrentThread(i.jVM)
		C.DeleteGlobalRef(env, i.jInvoker)
	})
	return i, nil
}

type jniInvoker struct {
	jVM       *C.JavaVM
	jInvoker  C.jobject
	argGetter *argGetter
}

func (i *jniInvoker) Prepare(method string, numArgs int) (argptrs []interface{}, label security.Label, err error) {
	// NOTE(spetrovic): In the long-term, this method will return an array of
	// []vom.Value.  This will in turn result in VOM decoding all input
	// arguments into vom.Value objects, which we shall then de-serialize into
	// Java objects (see Invoke comments below).  This approach is blocked on
	// pending VOM encoder/decoder changes as well as Java (de)serializer.
	mArgs := i.argGetter.FindMethod(method, numArgs)
	if mArgs == nil {
		err = fmt.Errorf("couldn't find VDL method %q with %d args", method, numArgs)
		return
	}
	argptrs = mArgs.InPtrs()
	// TODO(spetrovic): ask the Java object to give us the label.
	label = security.AdminLabel
	return
}

func (i *jniInvoker) Invoke(method string, call ipc.ServerCall, argptrs []interface{}) (results []interface{}, err error) {
	// NOTE(spetrovic): In the long-term, all input arguments will be of
	// vom.Value type (see comments for Prepare() method above).  Through JNI,
	// we will call Java functions that transform a serialized vom.Value into
	// Java objects. We will then pass those Java objects to Java's Invoke
	// method.  The returned Java objects will be converted into serialized
	// vom.Values, which will then be returned.  This approach is blocked on VOM
	// encoder/decoder changes as well as Java's (de)serializer.
	var env *C.JNIEnv
	C.AttachCurrentThread(i.jVM, &env, nil)
	defer C.DetachCurrentThread(i.jVM)

	// Create a new Java server call instance.
	mArgs := i.argGetter.FindMethod(method, len(argptrs))
	if mArgs == nil {
		err = fmt.Errorf("couldn't find VDL method %q with %d args", method, len(argptrs))
	}
	sCall := newServerCall(call, mArgs)
	cid := jMethodID(env, jServerCallClass, "<init>", fmt.Sprintf("(%s)%s", longSign, voidSign))
	jServerCall := C.CallNewServerCallObject(env, jServerCallClass, cid, ptrValue(sCall))
	goRef(sCall) // unref-ed when jServerCall is garbage-collected

	// Translate input args to JSON.
	jArgs, err := i.encodeArgs(env, argptrs)
	if err != nil {
		return
	}
	// Invoke the method.
	const callSign = "Lcom/veyron2/ipc/ServerCall;"
	const replySign = "Lcom/veyron/runtimes/google/VDLInvoker$InvokeReply;"
	mid := jMethodID(env, C.GetObjectClass(env, i.jInvoker), "invoke", fmt.Sprintf("(%s%s[%s)%s", stringSign, callSign, stringSign, replySign))
	jReply := C.CallInvokeMethod(env, i.jInvoker, mid, jString(env, camelCase(method)), jServerCall, jArgs)
	if err := jExceptionMsg(env); err != nil {
		return nil, fmt.Errorf("error invoking Java method %q: %v", method, err)
	}
	// Decode and return results.
	return i.decodeResults(env, method, len(argptrs), jReply)
}

// encodeArgs JSON-encodes the provided argument pointers, converts them into
// Java strings, and returns a Java string array response.
func (*jniInvoker) encodeArgs(env *C.JNIEnv, argptrs []interface{}) (C.jobjectArray, error) {
	// JSON encode.
	jsonArgs := make([][]byte, len(argptrs))
	for i, argptr := range argptrs {
		// Remove the pointer from the argument.  Simply *argptr doesn't work
		// as argptr is of type interface{}.
		arg := derefOrDie(argptr)
		var err error
		jsonArgs[i], err = json.Marshal(arg)
		if err != nil {
			return nil, fmt.Errorf("error marshalling %q into JSON", arg)
		}
	}

	// Convert to Java array of C.jstring.
	ret := C.NewObjectArray(env, C.jsize(len(argptrs)), jStringClass, nil)
	for i, arg := range jsonArgs {
		C.SetObjectArrayElement(env, ret, C.jsize(i), C.jobject(jString(env, string(arg))))
	}
	return ret, nil
}

// decodeResults JSON-decodes replies stored in the Java reply object and
// returns an array of Go reply objects.
func (i *jniInvoker) decodeResults(env *C.JNIEnv, method string, numArgs int, jReply C.jobject) ([]interface{}, error) {
	// Unpack the replies.
	results := jStringArrayField(env, jReply, "results")
	hasAppErr := jBoolField(env, jReply, "hasApplicationError")
	errorID := jStringField(env, jReply, "errorID")
	errorMsg := jStringField(env, jReply, "errorMsg")

	// Get result instances.
	mArgs := i.argGetter.FindMethod(method, numArgs)
	if mArgs == nil {
		return nil, fmt.Errorf("couldn't find method %q with %d input args: %v", method, numArgs)
	}
	argptrs := mArgs.OutPtrs()

	// Check for app error.
	if hasAppErr {
		return resultsWithError(argptrs, verror.Make(verror.ID(errorID), errorMsg)), nil
	}
	// JSON-decode.
	if len(results) != len(argptrs) {
		return nil, fmt.Errorf("mismatch in number of output arguments, have: %d want: %d", len(results), len(argptrs))
	}
	for i, result := range results {
		if err := json.Unmarshal([]byte(result), argptrs[i]); err != nil {
			return nil, err
		}
	}
	return resultsWithError(argptrs, nil), nil
}

// resultsWithError dereferences the provided result pointers and appends the
// given error to the returned array.
func resultsWithError(resultptrs []interface{}, err error) []interface{} {
	ret := make([]interface{}, len(resultptrs)+1)
	for i, resultptr := range resultptrs {
		ret[i] = derefOrDie(resultptr)
	}
	ret[len(resultptrs)] = err
	return ret
}
